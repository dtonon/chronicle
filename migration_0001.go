package main

import (
	"context"
	"log"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

func init() {
	registerMigration(1, "0001ClassifyRootNotes", migration0001ClassifyRootNotes)
}

// migration0001ClassifyRootNotes classifies existing root notes as internal or
// external, assigning EX/OL/AR state based on root author and last activity
func migration0001ClassifyRootNotes() error {
	ctx := context.Background()
	now := nostr.Now()
	thirtyDays := nostr.Timestamp(30 * 24 * time.Hour / time.Second)
	sixMonths := nostr.Timestamp(180 * 24 * time.Hour / time.Second)

	for id, state := range rootNotesList.notes {
		if state != "" {
			// Already classified by new code, skip
			continue
		}

		// Determine if root event is owned by the relay owner
		rootFilter := nostr.Filter{IDs: []string{id}}
		rootChan, _ := wdb.QueryEvents(ctx, rootFilter)
		isInternal := false
		for rootEvent := range rootChan {
			if rootEvent.PubKey == config.OwnerPubkey {
				isInternal = true
			}
		}
		if isInternal {
			continue // Keep state as ""
		}

		// External thread — determine state from last activity
		activityFilter := nostr.Filter{
			Tags:  nostr.TagMap{"e": []string{id}},
			Limit: 1,
		}
		activityChan, _ := wdb.QueryEvents(ctx, activityFilter)
		lastActivity := nostr.Timestamp(0)
		for ev := range activityChan {
			if ev.CreatedAt > lastActivity {
				lastActivity = ev.CreatedAt
			}
		}

		if lastActivity == 0 {
			// No events found in store, skip classification
			continue
		}

		age := now - lastActivity
		switch {
		case age > sixMonths:
			rootNotesList.notes[id] = ThreadArchived
		case age > thirtyDays:
			rootNotesList.notes[id] = ThreadOld
		default:
			rootNotesList.notes[id] = ThreadExternal
		}
	}

	log.Printf("🗂️  Classified %d root notes", len(rootNotesList.notes))
	return rootNotesList.SaveToFile()
}
