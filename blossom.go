package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"fiatjaf.com/nostr"
)

var blossomURLRegex = regexp.MustCompile(`https?://[^/\s]+/([a-fA-F0-9]{64})(?:\.[a-zA-Z0-9]+)?`)
var ownerBlossomTrackingFile string
var ownerBlossomMutex sync.Mutex

func initOwnerBlossomTracking() {
	ownerBlossomTrackingFile = filepath.Join(config.DBPath, "owner_blossom")

	if _, err := os.Stat(ownerBlossomTrackingFile); os.IsNotExist(err) {
		log.Println("🗂️  Bootstrapping owner Blossom file tracking")
		bootstrapOwnerBlossomFiles()
	} else {
		log.Println("🗂️  Owner Blossom tracking file found")
	}
}

func bootstrapOwnerBlossomFiles() {
	hashSet := make(map[string]bool)

	filesFound := 0
	if entries, err := os.ReadDir(config.BlossomAssetsPath); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				filename := entry.Name()
				if !strings.HasSuffix(filename, ".tmp") && len(filename) == 64 {
					if _, err := filepath.Match("[a-fA-F0-9]*", filename); err == nil {
						hashSet[filename] = true
						filesFound++
					}
				}
			}
		}
		log.Printf("🗂️  Found %d existing owner Blossom files in assets directory", filesFound)
	} else {
		log.Printf("Warning: Could not read assets directory: %v", err)
	}

	ownerPK, err := nostr.PubKeyFromHex(config.OwnerPubkey)
	if err != nil {
		log.Printf("Error parsing owner pubkey: %v", err)
		return
	}

	missingFiles := make(map[string][]string)
	eventHashes := 0

	for event := range store.QueryEvents(nostr.Filter{Authors: []nostr.PubKey{ownerPK}}, 0) {
		hashes := extractBlossomHashes(event.Content)
		for _, hash := range hashes {
			eventHashes++
			if !hashSet[hash] {
				matches := blossomURLRegex.FindAllString(event.Content, -1)
				for _, url := range matches {
					if strings.Contains(url, hash) {
						missingFiles[hash] = append(missingFiles[hash], url)
						break
					}
				}
			}
			hashSet[hash] = true
		}
	}

	log.Printf("🔍 Found %d Blossom hashes in %d owner events", eventHashes, len(hashSet)-filesFound)

	downloaded := 0
	failed := 0

	for hash, urls := range missingFiles {
		for _, url := range urls {
			if err := downloadBlossomFile(url, hash, false); err == nil {
				downloaded++
				log.Printf("✅ Downloaded missing owner file: %s", hash[:16]+"...")
				break
			} else {
				failed++
				log.Printf("❌ Failed to download owner file: %s - %s", urls, err)
			}
		}
	}

	if len(missingFiles) > 0 {
		log.Printf("📥 Download results: %d succeeded, %d failed", downloaded, failed)
	}

	file, err := os.Create(ownerBlossomTrackingFile)
	if err != nil {
		log.Printf("Error creating owner Blossom tracking file: %v", err)
		return
	}
	defer file.Close()

	for hash := range hashSet {
		file.WriteString(hash + "\n")
	}

	log.Printf("📝 Bootstrapped %d total owner Blossom files (%d existing + %d from events)", len(hashSet), filesFound, len(hashSet)-filesFound)
}

func extractBlossomHashes(content string) []string {
	matches := blossomURLRegex.FindAllStringSubmatch(content, -1)
	var hashes []string
	for _, match := range matches {
		if len(match) > 1 {
			hashes = append(hashes, match[1])
		}
	}
	return hashes
}

func trackOwnerBlossomFile(hash string) {
	ownerBlossomMutex.Lock()
	defer ownerBlossomMutex.Unlock()

	file, err := os.OpenFile(ownerBlossomTrackingFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening owner Blossom tracking file: %v", err)
		return
	}
	defer file.Close()

	if _, err := file.WriteString(hash + "\n"); err != nil {
		log.Printf("Error writing to owner Blossom tracking file: %v", err)
	}
}

func isFileAlreadyDownloaded(hash string) bool {
	filePath := filepath.Join(config.BlossomAssetsPath, hash)
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}

func downloadBlossomFile(url, hash string, enforceMaxSize bool) error {
	if isFileAlreadyDownloaded(hash) {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	if enforceMaxSize && resp.ContentLength > 0 {
		maxSize := int64(config.MaxFileSizeMB * 1024 * 1024)
		if resp.ContentLength > maxSize {
			return fmt.Errorf("file too large: %d bytes (max %d MB)", resp.ContentLength, config.MaxFileSizeMB)
		}
	}

	tempFile := filepath.Join(config.BlossomAssetsPath, hash+".tmp")
	file, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	defer func() {
		file.Close()
		os.Remove(tempFile)
	}()

	hasher := sha256.New()
	var written int64

	if enforceMaxSize {
		maxSize := int64(config.MaxFileSizeMB * 1024 * 1024)
		limitedReader := io.LimitReader(resp.Body, maxSize+1)
		teeReader := io.TeeReader(limitedReader, hasher)

		written, err = io.Copy(file, teeReader)
		if err != nil {
			return err
		}

		if written > maxSize {
			return fmt.Errorf("file too large: %d bytes (max %d MB)", written, config.MaxFileSizeMB)
		}
	} else {
		teeReader := io.TeeReader(resp.Body, hasher)
		written, err = io.Copy(file, teeReader)
		if err != nil {
			return err
		}
	}

	calculatedHash := fmt.Sprintf("%x", hasher.Sum(nil))
	if calculatedHash != hash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", hash, calculatedHash)
	}

	file.Close()

	finalPath := filepath.Join(config.BlossomAssetsPath, hash)
	if err := os.Rename(tempFile, finalPath); err != nil {
		return err
	}

	log.Printf("📥 Downloaded Blossom file: %s (%d bytes)", hash[:16]+"...", written)
	return nil
}

func getUserWriteRelays(authorPubkey string) []string {
	ctx := context.Background()
	timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pk, err := nostr.PubKeyFromHex(authorPubkey)
	if err != nil {
		return nil
	}

	filter := nostr.Filter{
		Authors: []nostr.PubKey{pk},
		Kinds:   []nostr.Kind{nostr.KindRelayListMetadata},
		Limit:   1,
	}

	var relayListEvent nostr.Event
	var found bool
	for event := range store.QueryEvents(filter, 1) {
		if !found || event.CreatedAt > relayListEvent.CreatedAt {
			relayListEvent = event
			found = true
		}
	}

	if !found {
		for ev := range pool.FetchMany(timeout, seedRelays, filter, nostr.SubscriptionOptions{}) {
			if !found || ev.CreatedAt > relayListEvent.CreatedAt {
				relayListEvent = ev.Event
				found = true
				saveEvent(ev.Event)
			}
		}
	}

	if !found {
		return nil
	}

	var writeRelays []string
	for tag := range relayListEvent.Tags.FindAll("r") {
		if len(tag) >= 2 && tag[1] != "" {
			if len(tag) < 3 || tag[2] == "" || tag[2] == "write" {
				writeRelays = append(writeRelays, tag[1])
			}
		}
	}

	return writeRelays
}

func tryDownloadFromServers(servers []string, hash string, enforceMaxSize bool) bool {
	for _, server := range servers {
		url := fmt.Sprintf("%s/%s", server, hash)
		if err := downloadBlossomFile(url, hash, enforceMaxSize); err == nil {
			log.Printf("✅ Downloaded from server: %s", server)
			return true
		}
	}
	return false
}

func downloadFromAltBlossomServers(authorPubkey string, hash string, enforceMaxSize bool) bool {
	ctx := context.Background()
	timeout, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	pk, err := nostr.PubKeyFromHex(authorPubkey)
	if err != nil {
		return false
	}

	serverFilter := nostr.Filter{
		Authors: []nostr.PubKey{pk},
		Kinds:   []nostr.Kind{10063},
		Limit:   1,
	}

	var serverListEvent nostr.Event
	var found bool
	for event := range store.QueryEvents(serverFilter, 1) {
		if !found || event.CreatedAt > serverListEvent.CreatedAt {
			serverListEvent = event
			found = true
		}
	}

	if found {
		servers := extractServersFromEvent(serverListEvent)
		if len(servers) > 0 {
			log.Printf("🔍 Trying servers from local DB for %s", authorPubkey[:8]+"...")
			if tryDownloadFromServers(servers, hash, enforceMaxSize) {
				return true
			}
		}
	}

	found = false
	for ev := range pool.FetchMany(timeout, seedRelays, serverFilter, nostr.SubscriptionOptions{}) {
		if !found || ev.CreatedAt > serverListEvent.CreatedAt {
			serverListEvent = ev.Event
			found = true
		}
	}

	if found {
		saveEvent(serverListEvent)
		servers := extractServersFromEvent(serverListEvent)
		if len(servers) > 0 {
			log.Printf("🔍 Trying servers from seedRelays for %s", authorPubkey[:8]+"...")
			if tryDownloadFromServers(servers, hash, enforceMaxSize) {
				return true
			}
		}
	}

	writeRelays := getUserWriteRelays(authorPubkey)
	if len(writeRelays) > 0 {
		log.Printf("🔍 Searching user's write relays for %s server list", authorPubkey[:8]+"...")

		found = false
		for ev := range pool.FetchMany(timeout, writeRelays, serverFilter, nostr.SubscriptionOptions{}) {
			if !found || ev.CreatedAt > serverListEvent.CreatedAt {
				serverListEvent = ev.Event
				found = true
			}
		}

		if found {
			saveEvent(serverListEvent)
			servers := extractServersFromEvent(serverListEvent)
			if len(servers) > 0 {
				log.Printf("🔍 Trying servers from user's write relays for %s", authorPubkey[:8]+"...")
				if tryDownloadFromServers(servers, hash, enforceMaxSize) {
					return true
				}
			}
		}
	}

	return false
}

func extractServersFromEvent(event nostr.Event) []string {
	var servers []string
	for tag := range event.Tags.FindAll("server") {
		if len(tag) >= 2 && tag[1] != "" {
			servers = append(servers, tag[1])
		}
	}
	return servers
}

func processBlossomBackup(event nostr.Event) {
	if !config.BackupBlossomMedia {
		return
	}

	matches := blossomURLRegex.FindAllString(event.Content, -1)
	if len(matches) == 0 {
		return
	}

	go func() {
		isOwnerEvent := event.PubKey.Hex() == config.OwnerPubkey

		for _, originalURL := range matches {
			hashMatches := blossomURLRegex.FindStringSubmatch(originalURL)
			if len(hashMatches) < 2 {
				continue
			}
			hash := hashMatches[1]

			if isFileAlreadyDownloaded(hash) {
				continue
			}

			if err := downloadBlossomFile(originalURL, hash, !isOwnerEvent); err == nil {
				continue
			}

			log.Printf("🔄 Original URL %s failed, trying author's fallback servers", originalURL)

			if !downloadFromAltBlossomServers(event.PubKey.Hex(), hash, !isOwnerEvent) {
				log.Printf("⚠️  Failed to download Blossom file %s from all available sources", hash[:16]+"...")
			}
		}
	}()
}
