//go:build !lmdb

package main

import "fiatjaf.com/nostr/eventstore/boltdb"

func getDB() boltdb.BoltBackend {
	return boltdb.BoltBackend{
		Path: getEnv("DB_PATH") + "boltdb",
	}
}
