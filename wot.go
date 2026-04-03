package main

import (
	"context"
	"log"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru"
)

func belongsToWotNetwork(event nostr.Event) bool {
	for _, pk := range trustNetwork {
		if pk == event.PubKey.Hex() {
			return true
		}
	}
	return false
}

func updateTrustNetworkFilter() {
	trustNetworkMap = make(map[string]bool)

	log.Println("🌐 WoT: updating trust network map...")
	for pubkey, count := range pubkeyFollowerCount {
		if count >= config.MinFollowers {
			trustNetworkMap[pubkey] = true
			appendPubkey(pubkey)
		}
	}

	log.Println("🌐 WoT: trust network map updated with", len(trustNetwork), "keys")
}

func refreshProfiles(ctx context.Context, relay *khatru.Relay) {
	for i := 0; i < len(trustNetwork); i += 200 {
		timeout, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()

		end := i + 200
		if end > len(trustNetwork) {
			end = len(trustNetwork)
		}

		filter := nostr.Filter{
			Authors: pubKeysFromHexes(trustNetwork[i:end]),
			Kinds:   []nostr.Kind{nostr.KindProfileMetadata},
		}

		for ev := range pool.FetchMany(timeout, seedRelays, filter, nostr.SubscriptionOptions{}) {
			relay.AddEvent(ctx, ev.Event)
		}
	}
	log.Println("👤 WoT: profiles refreshed: ", len(trustNetwork))
}

func refreshTrustNetwork(ctx context.Context, relay *khatru.Relay) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	filter := nostr.Filter{
		Authors: []nostr.PubKey{nostr.MustPubKeyFromHex(config.OwnerPubkey)},
		Kinds:   []nostr.Kind{nostr.KindFollowList},
	}

	log.Println("🔍 WoT: fetching owner's follows")
	for ev := range pool.FetchMany(timeoutCtx, seedRelays, filter, nostr.SubscriptionOptions{}) {
		for contact := range ev.Tags.FindAll("p") {
			pubkeyFollowerCount[contact[1]]++
			appendOneHopNetwork(contact[1])
		}
	}

	log.Println("🌐 WoT: building web of trust graph...")
	for i := 0; i < len(oneHopNetwork); i += 100 {
		timeout, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()

		end := i + 100
		if end > len(oneHopNetwork) {
			end = len(oneHopNetwork)
		}

		filter = nostr.Filter{
			Authors: pubKeysFromHexes(oneHopNetwork[i:end]),
			Kinds:   []nostr.Kind{nostr.KindFollowList, nostr.KindRelayListMetadata, nostr.KindProfileMetadata},
		}

		for ev := range pool.FetchMany(timeout, seedRelays, filter, nostr.SubscriptionOptions{}) {
			for contact := range ev.Tags.FindAll("p") {
				if len(contact) > 1 {
					pubkeyFollowerCount[contact[1]]++
				}
			}

			for r := range ev.Tags.FindAll("r") {
				appendRelay(r[1])
			}

			if ev.Kind == nostr.KindProfileMetadata {
				relay.AddEvent(ctx, ev.Event)
			}
		}
	}
	log.Println("🫂  WoT: total network size:", len(pubkeyFollowerCount))
	log.Println("🔗 WoT: relays discovered:", len(relays))

	updateTrustNetworkFilter()
}

func appendRelay(relay string) {
	for _, r := range relays {
		if r == relay {
			return
		}
	}
	relays = append(relays, relay)
}

func appendPubkey(pubkey string) {
	for _, pk := range trustNetwork {
		if pk == pubkey {
			return
		}
	}

	if len(pubkey) != 64 {
		return
	}

	trustNetwork = append(trustNetwork, pubkey)
}

func appendOneHopNetwork(pubkey string) {
	for _, pk := range oneHopNetwork {
		if pk == pubkey {
			return
		}
	}

	if len(pubkey) != 64 {
		return
	}

	oneHopNetwork = append(oneHopNetwork, pubkey)
}
