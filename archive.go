package main

import (
	"context"
	"log"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip10"
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

func saveEvent(event nostr.Event) bool {
	for range store.QueryEvents(nostr.Filter{IDs: []nostr.ID{event.ID}}, 1) {
		return true
	}
	store.SaveEvent(event)
	return true
}

// getNIP65Relays fetches and parses the NIP-65 relay list (kind 10002) for a given pubkey
func getNIP65Relays(pubkey string) (read []string, write []string) {
	if pubkey == "" {
		return nil, nil
	}

	ctx := context.Background()
	timeout, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pk, err := nostr.PubKeyFromHex(pubkey)
	if err != nil {
		return nil, nil
	}

	var bestEvent *nostr.Event
	for ev := range pool.FetchMany(timeout, seedRelays, nostr.Filter{
		Authors: []nostr.PubKey{pk},
		Kinds:   []nostr.Kind{nostr.KindRelayListMetadata},
	}, nostr.SubscriptionOptions{}) {
		e := ev.Event
		if bestEvent == nil || e.CreatedAt > bestEvent.CreatedAt {
			bestEvent = &e
		}
	}

	if bestEvent == nil {
		return nil, nil
	}

	for tag := range bestEvent.Tags.FindAll("r") {
		if len(tag) < 2 {
			continue
		}
		if len(tag) == 2 {
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

// fetchThread fetches thread events passing WoT or whitelist and saves them
func fetchThread(rootEventID, opPubkey string, whitelisted map[string]bool, relayHint string, since *nostr.Timestamp) {
	readRelays, writeRelays := getNIP65Relays(opPubkey)
	relaysToQuery := append(readRelays, writeRelays...)
	if len(relaysToQuery) == 0 {
		relaysToQuery = seedRelays
	}
	if relayHint != "" {
		relaysToQuery = append([]string{relayHint}, relaysToQuery...)
	}

	ctx := context.Background()
	timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	filter := nostr.Filter{
		Kinds: []nostr.Kind{
			nostr.KindArticle,
			nostr.KindDeletion,
			nostr.KindReaction,
			nostr.KindZapRequest,
			nostr.KindZap,
			nostr.KindTextNote,
		},
		Tags: nostr.TagMap{"e": []string{rootEventID}},
	}
	if since != nil {
		filter.Since = *since
	}

	for ev := range pool.FetchMany(timeout, relaysToQuery, filter, nostr.SubscriptionOptions{}) {
		if belongsToWotNetwork(ev.Event) || whitelisted[ev.PubKey.Hex()] {
			saveEvent(ev.Event)
			go fetchQuotedEvents(ev.Event)
			go processBlossomBackup(ev.Event)
		}
	}
}

// fetchConversation fetches the full thread for an accepted event
func fetchConversation(event nostr.Event) {
	rootReference := nip10.GetThreadRoot(event.Tags)

	var rootEventID string
	var opPubkey string

	if rootReference == nil {
		rootEventID = event.ID.Hex()
		opPubkey = event.PubKey.Hex()
	} else {
		rootEventID = rootReference.AsTagReference()
		for rootEvent := range store.QueryEvents(nostr.Filter{IDs: []nostr.ID{nostr.MustIDFromHex(rootEventID)}}, 1) {
			opPubkey = rootEvent.PubKey.Hex()
		}
	}

	whitelisted := make(map[string]bool)
	for tag := range event.Tags.FindAll("p") {
		whitelisted[tag[1]] = true
	}

	relayHint := ""
	if rootReference != nil {
		if ep, ok := rootReference.(nostr.EventPointer); ok && len(ep.Relays) > 0 {
			relayHint = ep.Relays[0]
		}
	}

	fetchThread(rootEventID, opPubkey, whitelisted, relayHint, nil)
}

// fetchExternalThreadUpdates fetches new events for all external threads in the given state
func fetchExternalThreadUpdates(state string, since time.Duration) {
	sinceTs := nostr.Now() - nostr.Timestamp(since/time.Second)

	for id, s := range rootNotesList.notes {
		if s != state {
			continue
		}

		var opPubkey string
		for rootEvent := range store.QueryEvents(nostr.Filter{IDs: []nostr.ID{nostr.MustIDFromHex(id)}}, 1) {
			opPubkey = rootEvent.PubKey.Hex()
		}

		whitelisted := make(map[string]bool)
		for ownerEvent := range store.QueryEvents(nostr.Filter{
			Authors: []nostr.PubKey{nostr.MustPubKeyFromHex(config.OwnerPubkey)},
			Tags:    nostr.TagMap{"e": []string{id}},
		}, 0) {
			for tag := range ownerEvent.Tags.FindAll("p") {
				whitelisted[tag[1]] = true
			}
		}

		fetchThread(id, opPubkey, whitelisted, "", &sinceTs)
	}

	updateExternalThreadStates()
	if err := rootNotesList.SaveToFile(); err != nil {
		log.Printf("Error saving root notes after fetch: %v", err)
	}
}

// updateExternalThreadStates transitions external threads between EX/OL/AR
func updateExternalThreadStates() {
	now := nostr.Now()
	thirtyDays := nostr.Timestamp(30 * 24 * time.Hour / time.Second)
	sixMonths := nostr.Timestamp(180 * 24 * time.Hour / time.Second)

	for id, state := range rootNotesList.notes {
		if state == "" {
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
}

// fetchQuotedEvents fetches events referenced via q tags and their reactions/zaps
func fetchQuotedEvents(event nostr.Event) {
	var quoteIDs []string
	var quoteRelays []string
	for tag := range event.Tags.FindAll("q") {
		if len(tag) >= 2 {
			quoteIDs = append(quoteIDs, tag[1])
			if len(tag) >= 3 && tag[2] != "" {
				quoteRelays = append(quoteRelays, tag[2])
			}
		}
	}

	if len(quoteIDs) == 0 {
		return
	}

	ctx := context.Background()
	timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	relaysToQuery := append(quoteRelays, seedRelays...)

	go func() {
		for ev := range pool.FetchMany(timeout, relaysToQuery, nostr.Filter{
			IDs: func() []nostr.ID {
				ids := make([]nostr.ID, 0, len(quoteIDs))
				for _, h := range quoteIDs {
					if id, err := nostr.IDFromHex(h); err == nil {
						ids = append(ids, id)
					}
				}
				return ids
			}(),
		}, nostr.SubscriptionOptions{}) {
			saveEvent(ev.Event)
			go processBlossomBackup(ev.Event)
		}

		for ev := range pool.FetchMany(timeout, relaysToQuery, nostr.Filter{
			Kinds: []nostr.Kind{
				nostr.KindDeletion,
				nostr.KindReaction,
				nostr.KindZapRequest,
				nostr.KindZap,
			},
			Tags: nostr.TagMap{"e": quoteIDs},
		}, nostr.SubscriptionOptions{}) {
			saveEvent(ev.Event)
			go processBlossomBackup(ev.Event)
		}
	}()

	<-timeout.Done()
}
