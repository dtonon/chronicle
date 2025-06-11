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
		for _, url := range matches {
			hashMatches := blossomURLRegex.FindStringSubmatch(url)
			if len(hashMatches) < 2 {
				continue
			}
			hash := hashMatches[1]

			if isFileAlreadyDownloaded(hash) {
				continue
			}

			if err := downloadBlossomFile(url, hash); err != nil {
				log.Printf("⚠️  Failed to download Blossom file from %s: %v", url, err)
			}
		}
	}()
}