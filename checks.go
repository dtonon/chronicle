package main

import (
	"context"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip10"
	"github.com/nbd-wtf/go-nostr/nip13"
)

// acceptedEvent returns true if the event should be stored in the relay
// Owner events are always accepted. Other events must belong to a valid tracked
// thread and pass WoT, PoW, or the implicit per-thread whitelist check
func acceptedEvent(event nostr.Event) bool {
	if event.PubKey == config.OwnerPubkey {
		return true

	} else if event.Kind == nostr.KindGiftWrap {
		for _, tag := range event.Tags.GetAll([]string{"p"}) {
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
		if rootRef != nil && isWhitelistedForThread(event.PubKey, rootRef.Value()) {
			return true
		}

	}

	return false
}

// belongsToValidThread returns true if the event is part of a thread the owner
// participated in. For text notes and articles it checks the root notes list;
// for reactions, zaps and deletions it checks that the referenced event exists locally
func belongsToValidThread(event nostr.Event) bool {
	eReference := nip10.GetThreadRoot(event.Tags)
	if eReference == nil {
		// We already accept root notes by owner
		return false
	}

	if event.Kind == nostr.KindTextNote ||
		event.Kind == nostr.KindArticle {

		rootCheck := rootNotesList.Include(eReference.Value())
		return rootCheck
	}

	// The event refers to a note in the thread
	if event.Kind == nostr.KindDeletion ||
		event.Kind == nostr.KindReaction ||
		event.Kind == nostr.KindZapRequest ||
		event.Kind == nostr.KindZap {

		ctx := context.Background()
		_, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		filter := nostr.Filter{
			IDs: []string{eReference.Value()},
		}
		eventChan, _ := wdb.QueryEvents(ctx, filter)
		for range eventChan {
			return true
		}
	}

	return false
}

// isWhitelistedForThread checks if pubkey is whitelisted for a thread by verifying
// that the owner has an event in that thread that tags the pubkey
func isWhitelistedForThread(pubkey string, rootEventID string) bool {
	ctx := context.Background()
	filter := nostr.Filter{
		Authors: []string{config.OwnerPubkey},
		Tags: nostr.TagMap{
			"e": []string{rootEventID},
			"p": []string{pubkey},
		},
		Limit: 1,
	}
	eventChan, err := wdb.QueryEvents(ctx, filter)
	if err != nil {
		return false
	}
	for range eventChan {
		return true
	}
	return false
}
