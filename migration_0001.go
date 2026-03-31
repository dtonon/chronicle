package main

import (
	"log"
	"time"

	"fiatjaf.com/nostr"
)

func init() {
	registerMigration(1, "0001ClassifyRootNotes", migration0001ClassifyRootNotes)
}

func migration0001ClassifyRootNotes() error {
	now := nostr.Now()
	thirtyDays := nostr.Timestamp(30 * 24 * time.Hour / time.Second)
	sixMonths := nostr.Timestamp(180 * 24 * time.Hour / time.Second)

	for id, state := range rootNotesList.notes {
		if state != "" {
			continue
		}

		refID, err := nostr.IDFromHex(id)
		if err != nil {
			continue
		}

		isInternal := false
		for rootEvent := range store.QueryEvents(nostr.Filter{IDs: []nostr.ID{refID}}, 1) {
			if rootEvent.PubKey.Hex() == config.OwnerPubkey {
				isInternal = true
			}
		}
		if isInternal {
			continue
		}

		lastActivity := nostr.Timestamp(0)
		for ev := range store.QueryEvents(nostr.Filter{
			Tags:  nostr.TagMap{"e": []string{id}},
			Limit: 1,
		}, 1) {
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
