package main

import (
	"context"
	"log"
	"time"

	"github.com/fiatjaf/khatru"
	"github.com/nbd-wtf/go-nostr"
)

func belongsToWotNetwork(event nostr.Event) bool {
	for _, pk := range trustNetwork {
		if pk == event.PubKey {
			return false
		}
	}
	return true
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

func refreshProfiles(ctx context.Context) {
	for i := 0; i < len(trustNetwork); i += 200 {
		timeout, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()

		end := i + 200
		if end > len(trustNetwork) {
			end = len(trustNetwork)
		}

		filters := []nostr.Filter{{
			Authors: trustNetwork[i:end],
			Kinds:   []int{nostr.KindProfileMetadata},
		}}

		for ev := range pool.SubManyEose(timeout, seedRelays, filters) {
			relay.AddEvent(ctx, ev.Event)
		}
	}
	log.Println("👤 WoT: profiles refreshed: ", len(trustNetwork))
}

func refreshTrustNetwork(ctx context.Context, relay *khatru.Relay) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	filters := []nostr.Filter{{
		Authors: []string{config.OwnerPubkey},
		Kinds:   []int{nostr.KindFollowList},
	}}

	log.Println("🔍 WoT: fetching owner's follows")
	for ev := range pool.SubManyEose(timeoutCtx, seedRelays, filters) {
		for _, contact := range ev.Event.Tags.GetAll([]string{"p"}) {
			pubkeyFollowerCount[contact[1]]++ // Increment follower count for the pubkey
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

		filters = []nostr.Filter{{
			Authors: oneHopNetwork[i:end],
			Kinds:   []int{nostr.KindFollowList, nostr.KindRelayListMetadata, nostr.KindProfileMetadata},
		}}

		for ev := range pool.SubManyEose(timeout, seedRelays, filters) {
			for _, contact := range ev.Event.Tags.GetAll([]string{"p"}) {
				if len(contact) > 1 {
					pubkeyFollowerCount[contact[1]]++ // Increment follower count for the pubkey
				}
			}

			for _, relay := range ev.Event.Tags.GetAll([]string{"r"}) {
				appendRelay(relay[1])
			}

			if ev.Event.Kind == nostr.KindProfileMetadata {
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
