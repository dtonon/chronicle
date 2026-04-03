//go:build lmdb

package main

import (
	"fiatjaf.com/nostr/eventstore/lmdb"
)

func getDB() lmdb.LMDBBackend {
	return lmdb.LMDBBackend{
		Path: getEnv("DB_PATH"),
	}
}
