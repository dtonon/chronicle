package main

import (
	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip10"
	"fiatjaf.com/nostr/nip13"
)

// acceptedEvent returns true if the event should be stored in the relay
func acceptedEvent(event nostr.Event) bool {
	if event.PubKey.Hex() == config.OwnerPubkey {
		return true

	} else if event.Kind == nostr.KindGiftWrap {
		for tag := range event.Tags.FindAll("p") {
			if tag[1] == config.OwnerPubkey {
				return (belongsToWotNetwork(event) || nip13.Difficulty(event.ID) >= config.PowDMWhitelist)
			}
		}
		return false

	} else if belongsToValidThread(event) {
		if belongsToWotNetwork(event) || nip13.Difficulty(event.ID) >= config.PowWhitelist {
			return true
		}
		rootRef := nip10.GetThreadRoot(event.Tags)
		if rootRef != nil && isWhitelistedForThread(event.PubKey.Hex(), rootRef.AsTagReference()) {
			return true
		}

	}

	return false
}

// belongsToValidThread returns true if the event is part of a tracked thread
func belongsToValidThread(event nostr.Event) bool {
	eReference := nip10.GetThreadRoot(event.Tags)
	if eReference == nil {
		return false
	}

	if event.Kind == nostr.KindTextNote ||
		event.Kind == nostr.KindArticle {

		return rootNotesList.Include(eReference.AsTagReference())
	}

	if event.Kind == nostr.KindDeletion ||
		event.Kind == nostr.KindReaction ||
		event.Kind == nostr.KindZapRequest ||
		event.Kind == nostr.KindZap {

		refID, err := nostr.IDFromHex(eReference.AsTagReference())
		if err != nil {
			return false
		}
		for range store.QueryEvents(nostr.Filter{IDs: []nostr.ID{refID}}, 1) {
			return true
		}
	}

	return false
}

// isWhitelistedForThread checks if pubkey is whitelisted for a thread
func isWhitelistedForThread(pubkey string, rootEventID string) bool {
	ownerPK, err := nostr.PubKeyFromHex(config.OwnerPubkey)
	if err != nil {
		return false
	}
	filter := nostr.Filter{
		Authors: []nostr.PubKey{ownerPK},
		Tags: nostr.TagMap{
			"e": []string{rootEventID},
			"p": []string{pubkey},
		},
		Limit: 1,
	}
	for range store.QueryEvents(filter, 1) {
		return true
	}
	return false
}
