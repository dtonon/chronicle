package main

import (
	"context"
	"log"
	"time"

	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

var seedRelays = []string{
	"wss://nos.lol",
	"wss://nostr.mom",
	"wss://purplepag.es",
	"wss://purplerelay.com",
	"wss://relay.damus.io",
	"wss://relay.nostr.band",
	"wss://relay.snort.social",
	"wss://relayable.org",
	"wss://relay.primal.net",
	"wss://relay.nostr.bg",
	"wss://no.str.cr",
	"wss://nostr21.com",
	"wss://nostrue.com",
	"wss://relay.siamstr.com",
	"wss://bitcoiner.social",
}

func archiveTrustedNotes(ctx context.Context, relay *khatru.Relay) {
	timeout, cancel := context.WithTimeout(ctx, time.Duration(config.RefreshInterval)*time.Hour)
	defer cancel()

	go func() {
		filters := []nostr.Filter{
			{
				Kinds: []int{
					nostr.KindArticle,
					nostr.KindDeletion,
					nostr.KindEncryptedDirectMessage,
					nostr.KindReaction,
					nostr.KindRepost,
					nostr.KindZapRequest,
					nostr.KindZap,
					nostr.KindTextNote,
				},
				Authors: []string{config.OwnerPubkey},
			},
			{
				Kinds: []int{
					nostr.KindArticle,
					nostr.KindTextNote,
					nostr.KindDeletion,
					nostr.KindEncryptedDirectMessage,
					nostr.KindReaction,
					nostr.KindRepost,
					nostr.KindZapRequest,
					nostr.KindZap,
					nostr.KindGiftWrap,
				},
				Tags: nostr.TagMap{"p": []string{config.OwnerPubkey}},
			},
		}

		log.Println("📦 Archiving trusted notes...")

		for ev := range pool.SubMany(timeout, seedRelays, filters) {
			go archiveEvent(ctx, relay, *ev.Event)
		}
	}()

	<-timeout.Done()
	log.Println("📦 Archived", trustedNotes, "trusted notes, discarded", untrustedNotes, "notes")
}

func archiveEvent(ctx context.Context, relay *khatru.Relay, event nostr.Event) {
	if acceptedEvent(event) {
		addEventToRootList(event)
		fetchQuotedEvents(event)
		wdb.Publish(ctx, event)
		relay.BroadcastEvent(&event)
		trustedNotes++
	} else {
		untrustedNotes++
	}
}
