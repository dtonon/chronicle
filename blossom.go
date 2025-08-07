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

	"github.com/nbd-wtf/go-nostr"
)

// Blossom backup functionality
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

	// Step 1: Scan existing files in BLOSSOM_ASSETS_PATH
	filesFound := 0
	if entries, err := os.ReadDir(config.BlossomAssetsPath); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				filename := entry.Name()
				// Skip temporary files and validate hash format (64 hex chars)
				if !strings.HasSuffix(filename, ".tmp") && len(filename) == 64 {
					// Validate it's a valid hex hash
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

	// Step 2: Scan owner events for additional Blossom URLs
	ctx := context.Background()
	filter := nostr.Filter{
		Authors: []string{config.OwnerPubkey},
	}

	eventChan, err := wdb.QueryEvents(ctx, filter)
	if err != nil {
		log.Printf("Error querying owner events for Blossom bootstrap: %v", err)
		return
	}

	missingFiles := make(map[string][]string) // hash -> []urls
	eventHashes := 0

	for event := range eventChan {
		hashes := extractBlossomHashes(event.Content)
		for _, hash := range hashes {
			eventHashes++
			if !hashSet[hash] {
				// Extract original URL for download attempt
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

	// Step 3: Attempt to download missing files
	downloaded := 0
	failed := 0

	for hash, urls := range missingFiles {
		success := false
		for _, url := range urls {
			if err := downloadBlossomFile(url, hash, false); err == nil {
				downloaded++
				log.Printf("✅ Downloaded missing owner file: %s", hash[:16]+"...")
				success = true
				break
			}
		}
		if !success {
			failed++
			log.Printf("❌ Failed to download owner file: %s", hash[:16]+"...")
		}
	}

	if len(missingFiles) > 0 {
		log.Printf("📥 Download results: %d succeeded, %d failed", downloaded, failed)
	}

	// Step 4: Write tracking file with all discovered hashes
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
		// Use LimitReader to enforce max file size
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
		// No size limit for owner files during bootstrap
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

	filter := nostr.Filter{
		Authors: []string{authorPubkey},
		Kinds:   []int{10002},
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
		if len(servers) > 0 {
			log.Printf("🔍 Trying servers from local DB for %s", authorPubkey[:8]+"...")
			if tryDownloadFromServers(servers, hash, enforceMaxSize) {
				return true
			}
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
		if len(servers) > 0 {
			log.Printf("🔍 Trying servers from seedRelays for %s", authorPubkey[:8]+"...")
			if tryDownloadFromServers(servers, hash, enforceMaxSize) {
				return true
			}
		}
	}

	// Step 3: Try user's write relays (NIP-65)
	writeRelays := getUserWriteRelays(authorPubkey)
	if len(writeRelays) > 0 {
		log.Printf("🔍 Searching user's write relays for %s server list", authorPubkey[:8]+"...")

		serverListEvent = nil
		for ev := range pool.SubManyEose(timeout, writeRelays, []nostr.Filter{serverFilter}) {
			if serverListEvent == nil || ev.Event.CreatedAt > serverListEvent.CreatedAt {
				serverListEvent = ev.Event
			}
		}

		if serverListEvent != nil {
			saveEvent(ctx, *serverListEvent)
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
		// Check if this is owner media (no size limit for owner)
		isOwnerEvent := event.PubKey == config.OwnerPubkey
		
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
			// No size limit for owner's files, enforce for others
			if err := downloadBlossomFile(originalURL, hash, !isOwnerEvent); err == nil {
				continue // Success, move to next file
			}

			log.Printf("🔄 Original URL %s failed, trying author's fallback servers", originalURL)

			// Try the comprehensive tier-by-tier approach
			if !downloadFromAltBlossomServers(event.PubKey, hash, !isOwnerEvent) {
				log.Printf("⚠️  Failed to download Blossom file %s from all available sources", hash[:16]+"...")
			}
		}
	}()
}
