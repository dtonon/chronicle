package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"time"

	"github.com/fiatjaf/eventstore/badger"
	nostr "github.com/nbd-wtf/go-nostr"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: migrate <badger-db-path> <relay-ws-url>")
		fmt.Fprintln(os.Stderr, "example: migrate ./db ws://localhost:3334")
		os.Exit(1)
	}

	ctx := context.Background()

	db := &badger.BadgerBackend{Path: os.Args[1], MaxLimit: 10_000_000}
	if err := db.Init(); err != nil {
		log.Fatalf("open badger: %v", err)
	}
	defer db.Close()

	relay, err := nostr.RelayConnect(ctx, os.Args[2])
	if err != nil {
		log.Fatalf("connect to relay: %v", err)
	}
	defer relay.Close()

	ch, err := db.QueryEvents(ctx, nostr.Filter{Limit: 10_000_000})
	if err != nil {
		log.Fatalf("query events: %v", err)
	}

	var events []*nostr.Event
	for event := range ch {
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt < events[j].CreatedAt
	})
	log.Printf("loaded %d events, publishing oldest-first", len(events))

	var total, accepted, rejected int
	for _, event := range events {
		total++
		pubCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := relay.Publish(pubCtx, *event); err != nil {
			rejected++
			log.Printf("rejected kind:%d id:%s reason: %v", event.Kind, event.ID, err)
		} else {
			accepted++
		}
		cancel()
		if total%100 == 0 {
			log.Printf("progress: %d processed, %d accepted, %d rejected", total, accepted, rejected)
		}
	}

	log.Printf("done: %d total, %d accepted, %d rejected", total, accepted, rejected)
}
