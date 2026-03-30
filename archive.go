package main

import (
	"context"
	"log"
	"time"

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

// fetchThread is the shared core for fetching thread events. It resolves the
// OP's NIP-65 relays, then fetches and saves all events that pass WoT or the whitelist
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
		Kinds: []int{
			nostr.KindArticle,
			nostr.KindDeletion,
			nostr.KindReaction,
			nostr.KindZapRequest,
			nostr.KindZap,
			nostr.KindTextNote,
		},
		Tags:  nostr.TagMap{"e": []string{rootEventID}},
		Since: since,
	}

	for ev := range pool.SubManyEose(timeout, relaysToQuery, []nostr.Filter{filter}) {
		if belongsToWotNetwork(*ev.Event) || whitelisted[ev.Event.PubKey] {
			saveEvent(ctx, *ev.Event)
			go fetchQuotedEvents(*ev.Event)
			go processBlossomBackup(*ev.Event)
		}
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
		eventChan, _ := wdb.QueryEvents(lookupCtx, nostr.Filter{IDs: []string{rootEventID}})
		for rootEvent := range eventChan {
			opPubkey = rootEvent.PubKey
		}
	}

	// Build whitelist from owner's p tags — avoids per-event DB queries in the fetch loop
	whitelisted := make(map[string]bool)
	for _, tag := range event.Tags.GetAll([]string{"p"}) {
		whitelisted[tag[1]] = true
	}

	relayHint := ""
	if rootReference != nil {
		relayHint = rootReference.Relay()
	}

	fetchThread(rootEventID, opPubkey, whitelisted, relayHint, nil)
}

// fetchExternalThreadUpdates fetches new events for all external threads in the
// given state, using a since window matching the fetch frequency for that state
func fetchExternalThreadUpdates(state string, since time.Duration) {
	ctx := context.Background()
	sinceTs := nostr.Now() - nostr.Timestamp(since/time.Second)

	for id, s := range rootNotesList.notes {
		if s != state {
			continue
		}

		rootChan, _ := wdb.QueryEvents(ctx, nostr.Filter{IDs: []string{id}})
		var opPubkey string
		for rootEvent := range rootChan {
			opPubkey = rootEvent.PubKey
		}

		// Build whitelist from all p tags across owner's events in this thread
		whitelisted := make(map[string]bool)
		ownerChan, _ := wdb.QueryEvents(ctx, nostr.Filter{
			Authors: []string{config.OwnerPubkey},
			Tags:    nostr.TagMap{"e": []string{id}},
		})
		for ownerEvent := range ownerChan {
			for _, tag := range ownerEvent.Tags.GetAll([]string{"p"}) {
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
// based on the age of their most recent stored event.
func updateExternalThreadStates() {
	ctx := context.Background()
	now := nostr.Now()
	thirtyDays := nostr.Timestamp(30 * 24 * time.Hour / time.Second)
	sixMonths := nostr.Timestamp(180 * 24 * time.Hour / time.Second)

	for id, state := range rootNotesList.notes {
		if state == "" {
			continue // Internal thread, skip
		}

		activityChan, _ := wdb.QueryEvents(ctx, nostr.Filter{
			Tags:  nostr.TagMap{"e": []string{id}},
			Limit: 1,
		})
		lastActivity := nostr.Timestamp(0)
		for ev := range activityChan {
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
