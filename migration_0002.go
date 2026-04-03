package main

import (
	"log"

	"fiatjaf.com/nostr"
)

func init() {
	registerMigration(2, "0002CleanupRootNotes", migration0002CleanupRootNotes)
}

func migration0002CleanupRootNotes() error {
	ownerPK, err := nostr.PubKeyFromHex(config.OwnerPubkey)
	if err != nil {
		return err
	}

	var toRemove []string

	for id := range rootNotesList.notes {
		refID, err := nostr.IDFromHex(id)
		if err != nil {
			toRemove = append(toRemove, id)
			continue
		}

		ownerIsAuthor := false
		for rootEvent := range store.QueryEvents(nostr.Filter{IDs: []nostr.ID{refID}}, 1) {
			if rootEvent.PubKey.Hex() == config.OwnerPubkey {
				ownerIsAuthor = true
			}
		}
		if ownerIsAuthor {
			continue
		}

		ownerParticipated := false
		for range store.QueryEvents(nostr.Filter{
			Authors: []nostr.PubKey{ownerPK},
			Tags:    nostr.TagMap{"e": []string{id}},
			Limit:   1,
		}, 1) {
			ownerParticipated = true
		}

		if !ownerParticipated {
			toRemove = append(toRemove, id)
		}
	}

	for _, id := range toRemove {
		delete(rootNotesList.notes, id)
	}

	log.Printf("🧹 Removed %d root notes that don't match relay conditions", len(toRemove))
	return rootNotesList.SaveToFile()
}
