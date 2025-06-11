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
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// Blossom backup functionality
var blossomURLRegex = regexp.MustCompile(`https?://[^/\s]+/([a-fA-F0-9]{64})(?:\.[a-zA-Z0-9]+)?`)
var ownerBlossomTrackingFile string
var ownerBlossomMutex sync.Mutex

func initOwnerBlossomTracking() {
	ownerBlossomTrackingFile = filepath.Join(config.DBPath, "owner_blossom")

	if _, err := os.Stat(ownerBlossomTrackingFile); os.IsNotExist(err) {
		log.Println("🗂️  Bootstrapping owner Blossom file tracking...")
		bootstrapOwnerBlossomFiles()
	} else {
		log.Println("🗂️  Owner Blossom tracking file found")
	}
}

func bootstrapOwnerBlossomFiles() {
	ctx := context.Background()

	filter := nostr.Filter{
		Authors: []string{config.OwnerPubkey},
	}

	eventChan, err := wdb.QueryEvents(ctx, filter)
	if err != nil {
		log.Printf("Error querying owner events for Blossom bootstrap: %v", err)
		return
	}

	hashSet := make(map[string]bool)

	for event := range eventChan {
		hashes := extractBlossomHashes(event.Content)
		for _, hash := range hashes {
			hashSet[hash] = true
		}
	}

	file, err := os.Create(ownerBlossomTrackingFile)
	if err != nil {
		log.Printf("Error creating owner Blossom tracking file: %v", err)
		return
	}
	defer file.Close()

	count := 0
	for hash := range hashSet {
		file.WriteString(hash + "\n")
		count++
	}

	log.Printf("📝 Bootstrapped %d owner Blossom files", count)
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

func downloadBlossomFile(url, hash string) error {
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

	if resp.ContentLength > 0 {
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

	// Use LimitReader to enforce max file size
	maxSize := int64(config.MaxFileSizeMB * 1024 * 1024)
	limitedReader := io.LimitReader(resp.Body, maxSize+1)

	hasher := sha256.New()
	teeReader := io.TeeReader(limitedReader, hasher)

	written, err := io.Copy(file, teeReader)
	if err != nil {
		return err
	}

	if written > maxSize {
		return fmt.Errorf("file too large: %d bytes (max %d MB)", written, config.MaxFileSizeMB)
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

	filter := nostr.Filter{
		Authors: []string{authorPubkey},
		Kinds:   []int{10002}, // NIP-65 relay list metadata
		Limit:   1,
	}

	// First try local database
	var relayListEvent *nostr.Event
	if eventChan, err := wdb.QueryEvents(timeout, filter); err == nil {
		for event := range eventChan {
			if relayListEvent == nil || event.CreatedAt > relayListEvent.CreatedAt {
				relayListEvent = event
			}
		}
	}

	// If not found locally, fetch from seedRelays
	if relayListEvent == nil {
		for ev := range pool.SubManyEose(timeout, seedRelays, []nostr.Filter{filter}) {
			if relayListEvent == nil || ev.Event.CreatedAt > relayListEvent.CreatedAt {
				relayListEvent = ev.Event
				saveEvent(ctx, *ev.Event)
			}
		}
	}

	if relayListEvent == nil {
		return nil
	}

	// Extract write relays (relays without "read" marker or with "write" marker)
	var writeRelays []string
	for _, tag := range relayListEvent.Tags.GetAll([]string{"r"}) {
		if len(tag) >= 2 && tag[1] != "" {
			// If no third parameter, it's read+write
			// If third parameter is "write", it's write-only
			// If third parameter is "read", skip it
			if len(tag) < 3 || tag[2] == "" || tag[2] == "write" {
				writeRelays = append(writeRelays, tag[1])
			}
		}
	}

	return writeRelays
}

func testMediaExists(servers []string, hash string) []string {
	var workingServers []string

	for _, server := range servers {
		url := fmt.Sprintf("%s/%s", server, hash)

		// Send HEAD request to check if file exists without downloading
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
		if err != nil {
			cancel()
			continue
		}

		resp, err := http.DefaultClient.Do(req)
		cancel()

		if err != nil {
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			workingServers = append(workingServers, server)
		}
	}

	return workingServers
}

func getAuthorBlossomServers(authorPubkey string, hash string) []string {
	ctx := context.Background()
	timeout, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	serverFilter := nostr.Filter{
		Authors: []string{authorPubkey},
		Kinds:   []int{10063},
		Limit:   1,
	}

	// Step 1: Try local database
	var serverListEvent *nostr.Event
	if eventChan, err := wdb.QueryEvents(timeout, serverFilter); err == nil {
		for event := range eventChan {
			if serverListEvent == nil || event.CreatedAt > serverListEvent.CreatedAt {
				serverListEvent = event
			}
		}
	}

	if serverListEvent != nil {
		servers := extractServersFromEvent(serverListEvent)
		workingServers := testMediaExists(servers, hash)
		if len(workingServers) > 0 {
			log.Printf("🎯 Found working servers in local DB for %s", authorPubkey[:8]+"...")
			return workingServers
		}
	}

	// Step 2: Try seedRelays
	serverListEvent = nil
	for ev := range pool.SubManyEose(timeout, seedRelays, []nostr.Filter{serverFilter}) {
		if serverListEvent == nil || ev.Event.CreatedAt > serverListEvent.CreatedAt {
			serverListEvent = ev.Event
		}
	}

	if serverListEvent != nil {
		saveEvent(ctx, *serverListEvent)
		servers := extractServersFromEvent(serverListEvent)
		workingServers := testMediaExists(servers, hash)
		if len(workingServers) > 0 {
			log.Printf("🎯 Found working servers in seedRelays for %s", authorPubkey[:8]+"...")
			return workingServers
		}
	}

	// Step 3: Try user's write relays (NIP-65)
	writeRelays := getUserWriteRelays(authorPubkey)
	if len(writeRelays) > 0 {
		log.Printf("🔍 Searching user's write relays for %s server list...", authorPubkey[:8]+"...")

		serverListEvent = nil
		for ev := range pool.SubManyEose(timeout, writeRelays, []nostr.Filter{serverFilter}) {
			if serverListEvent == nil || ev.Event.CreatedAt > serverListEvent.CreatedAt {
				serverListEvent = ev.Event
			}
		}

		if serverListEvent != nil {
			saveEvent(ctx, *serverListEvent)
			servers := extractServersFromEvent(serverListEvent)
			workingServers := testMediaExists(servers, hash)
			if len(workingServers) > 0 {
				log.Printf("🎯 Found working servers in user's write relays for %s", authorPubkey[:8]+"...")
				return workingServers
			}
		}
	}

	return nil
}

func extractServersFromEvent(event *nostr.Event) []string {
	var servers []string
	for _, tag := range event.Tags.GetAll([]string{"server"}) {
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

	// Extract all Blossom URLs directly from the event content
	matches := blossomURLRegex.FindAllString(event.Content, -1)
	if len(matches) == 0 {
		return
	}

	// Process downloads asynchronously
	go func() {
		for _, originalURL := range matches {
			hashMatches := blossomURLRegex.FindStringSubmatch(originalURL)
			if len(hashMatches) < 2 {
				continue
			}
			hash := hashMatches[1]

			if isFileAlreadyDownloaded(hash) {
				continue
			}

			// Try downloading from the original URL first
			if err := downloadBlossomFile(originalURL, hash); err == nil {
				continue // Success, move to next file
			}

			log.Printf("🔄 Original URL failed for %s, searching for verified fallback servers...", hash[:16]+"...")

			// Use the comprehensive search with validation
			workingServers := getAuthorBlossomServers(event.PubKey, hash)
			if len(workingServers) == 0 {
				log.Printf("⚠️  No working fallback servers found for author %s", event.PubKey[:8]+"...")
				continue
			}

			// Try downloading from the first working server
			downloaded := false
			for _, server := range workingServers {
				fallbackURL := fmt.Sprintf("%s/%s", server, hash)
				if err := downloadBlossomFile(fallbackURL, hash); err == nil {
					log.Printf("✅ Downloaded from verified server: %s", server)
					downloaded = true
					break
				}
			}

			if !downloaded {
				log.Printf("⚠️  Failed to download Blossom file %s even from verified servers", hash[:16]+"...")
			}
		}
	}()
}
