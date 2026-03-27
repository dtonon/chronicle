package main

import (
	"context"
	"log"
	"time"

	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip10"
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

func saveEvent(ctx context.Context, event nostr.Event) bool {
	filter := nostr.Filter{IDs: []string{event.ID}}
	eventChan, err := wdb.QueryEvents(ctx, filter)
	if err != nil {
		return false
	}
	for range eventChan {
		return true
	}
	wdb.Publish(ctx, event)
	return true
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
			archiveEvent(ctx, relay, *ev.Event)
		}
	}()

	<-timeout.Done()
	log.Println("📦 Archived", trustedNotes, "trusted notes, discarded", untrustedNotes, "notes")
}

func archiveEvent(ctx context.Context, relay *khatru.Relay, event nostr.Event) {
	if acceptedEvent(event) {
		addEventToRootList(event)
		fetchQuotedEvents(event)
		saveEvent(ctx, event)
		relay.BroadcastEvent(&event)
		trustedNotes++
	} else {
		untrustedNotes++
	}
}

// fetchConversation fetches the full thread for an accepted event, querying
// the OP's NIP-65 read and write relays. Non-WoT authors are accepted if they
// are tagged in the owner's triggering event (implicit whitelist via p tags)
func fetchConversation(event nostr.Event) {
	rootReference := nip10.GetThreadRoot(event.Tags)

	var rootEventID string
	var opPubkey string

	if rootReference == nil {
		rootEventID = event.ID
		opPubkey = event.PubKey
	} else {
		rootEventID = rootReference.Value()
		lookupCtx := context.Background()
		filter := nostr.Filter{IDs: []string{rootEventID}}
		eventChan, _ := wdb.QueryEvents(lookupCtx, filter)
		for rootEvent := range eventChan {
			opPubkey = rootEvent.PubKey
		}
	}

	readRelays, writeRelays := getNIP65Relays(opPubkey)
	relaysToQuery := append(readRelays, writeRelays...)
	if len(relaysToQuery) == 0 {
		relaysToQuery = seedRelays
	}
	if rootReference != nil && rootReference.Relay() != "" {
		relaysToQuery = append([]string{rootReference.Relay()}, relaysToQuery...)
	}

	ctx := context.Background()
	timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	go func() {
		filters := []nostr.Filter{
			{
				Kinds: []int{
					nostr.KindArticle,
					nostr.KindDeletion,
					nostr.KindReaction,
					nostr.KindZapRequest,
					nostr.KindZap,
					nostr.KindTextNote,
				},
				Tags: nostr.TagMap{"e": []string{rootEventID}},
			},
		}

		for ev := range pool.SubMany(timeout, relaysToQuery, filters) {
			if belongsToWotNetwork(*ev.Event) || isWhitelistedForThread(ev.Event.PubKey, rootEventID) {
				saveEvent(ctx, *ev.Event)
				go fetchQuotedEvents(*ev.Event)
				go processBlossomBackup(*ev.Event)
			}
		}
	}()

	<-timeout.Done()
}

// fetchQuotedEvents fetches events referenced via q tags and their reactions/zaps
func fetchQuotedEvents(event nostr.Event) {
	var quoteIDs []string
	var quoteRelays []string
	for _, tag := range event.Tags.GetAll([]string{"q", ""}) {
		quoteIDs = append(quoteIDs, tag[1])
		if len(tag) >= 3 && tag[2] != "" {
			quoteRelays = append(quoteRelays, tag[2])
		}
	}

	if len(quoteIDs) == 0 { // No quoted events found
		return
	}

	ctx := context.Background()
	timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	go func() {
		filters := []nostr.Filter{
			{
				IDs: quoteIDs,
			},
			{
				Kinds: []int{
					nostr.KindDeletion,
					nostr.KindReaction,
					nostr.KindZapRequest,
					nostr.KindZap,
				},
				Tags: nostr.TagMap{"e": quoteIDs},
			},
		}

		for ev := range pool.SubManyEose(timeout, append(quoteRelays, seedRelays...), filters) {
			saveEvent(ctx, *ev.Event)
			go processBlossomBackup(*ev.Event)
		}
	}()

	<-timeout.Done()
}

// getNIP65Relays fetches and parses the NIP-65 relay list (kind 10002) for a given pubkey
// Returns separate slices for read and write relays
func getNIP65Relays(pubkey string) (read []string, write []string) {
	if pubkey == "" {
		return nil, nil
	}

	ctx := context.Background()
	timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var bestEvent *nostr.Event
	for ev := range pool.SubManyEose(timeout, seedRelays, []nostr.Filter{{
		Authors: []string{pubkey},
		Kinds:   []int{nostr.KindRelayListMetadata},
	}}) {
		if bestEvent == nil || ev.Event.CreatedAt > bestEvent.CreatedAt {
			bestEvent = ev.Event
		}
	}

	if bestEvent == nil {
		return nil, nil
	}

	for _, tag := range bestEvent.Tags.GetAll([]string{"r"}) {
		if len(tag) < 2 {
			continue
		}
		if len(tag) == 2 {
			// No marker means both read and write
			read = append(read, tag[1])
			write = append(write, tag[1])
		} else if tag[2] == "read" {
			read = append(read, tag[1])
		} else if tag[2] == "write" {
			write = append(write, tag[1])
		}
	}
	return read, write
}
