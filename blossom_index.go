package main

import (
	"context"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru/blossom"
)

// blobIndex wraps the khatru index so Delete closes the lookup's read
// transaction before writing. The upstream version keeps the iterator
// open while deleting, which deadlocks bolt when the write must grow the mmap
type blobIndex struct {
	blossom.EventStoreBlobIndexWrapper
}

func (bi blobIndex) Delete(ctx context.Context, sha256 string, pubkey nostr.PubKey) error {
	var id nostr.ID
	found := false
	for evt := range bi.Store.QueryEvents(nostr.Filter{
		Authors: []nostr.PubKey{pubkey},
		Tags:    nostr.TagMap{"x": []string{sha256}},
		Kinds:   []nostr.Kind{24242},
		Limit:   1,
	}, 1) {
		id = evt.ID
		found = true
		break
	}
	if !found {
		return nil
	}
	return bi.Store.DeleteEvent(id)
}
