package main

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"
	"text/template"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/eventstore"
	"fiatjaf.com/nostr/khatru"
	"fiatjaf.com/nostr/khatru/blossom"
	"fiatjaf.com/nostr/khatru/policies"
)

//go:embed template/index.html
var indexHTML string

//go:embed template/assets
var assets embed.FS

var (
	version string
)

var pool *nostr.Pool
var store eventstore.Store
var rootNotesList *RootNotes
var relays []string
var config Config
var trustNetwork []string
var oneHopNetwork []string
var trustNetworkMap map[string]bool
var pubkeyFollowerCount = make(map[string]int)

func pubKeysFromHexes(hexes []string) []nostr.PubKey {
	pks := make([]nostr.PubKey, 0, len(hexes))
	for _, h := range hexes {
		if pk, err := nostr.PubKeyFromHex(h); err == nil {
			pks = append(pks, pk)
		}
	}
	return pks
}

func main() {
	nostr.InfoLogger = log.New(io.Discard, "", 0)
	magenta := "\033[91m"
	gray := "\033[90m"
	reset := "\033[0m"

	art := magenta + `
   ____ _                     _      _
  / ___| |__  _ __ ___  _ __ (_) ___| | ___
 | |   | '_ \| '__/ _ \| '_ \| |/ __| |/ _ \
 | |___| | | | | | (_) | | | | | (__| |  __/
  \____|_| |_|_|  \___/|_| |_|_|\___|_|\___|` + gray + `
                        powered by Khatru
` + reset

	fmt.Println(art)

	log.Println("🚀 Booting up Chronicle relay")
	relay := khatru.NewRelay()
	ctx := context.Background()
	pool = nostr.NewPool(nostr.PoolOptions{})
	config = LoadConfig()
	os.MkdirAll(config.BlossomAssetsPath, 0755)

	relay.Info.Name = config.RelayName
	ownerPubkey := nostr.MustPubKeyFromHex(config.OwnerPubkey)
	relay.Info.PubKey = &ownerPubkey
	relay.Info.Icon = config.RelayIcon
	relay.Info.Contact = config.RelayContact
	relay.Info.Description = config.RelayDescription
	relay.Info.Software = "https://github.com/dtonon/chronicle"
	relay.Info.Version = version

	appendPubkey(config.OwnerPubkey)

	db := getDB()
	if err := db.Init(); err != nil {
		panic(err)
	}
	store = &db

	relay.UseEventstore(store, 500)

	rootNotesList = NewRootNotes(config.DBPath + "root_notes")
	if err := rootNotesList.LoadFromFile(); err != nil {
		fmt.Println("Error loading strings:", err)
		return
	} else {
		log.Println("🗣️  Monitoring", rootNotesList.Size(), "threads")
	}

	if err := runMigrations(); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	if config.BackupBlossomMedia {
		initOwnerBlossomTracking()
		log.Printf("📁 Blossom media backup enabled (max %d MB per file)", config.MaxFileSizeMB)
	}

	migrationMode := os.Getenv("MIGRATION_MODE") == "true"

	relay.OnEvent = policies.SeqEvent(
		policies.RejectEventsWithBase64Media,
		policies.EventIPRateLimiter(5, time.Minute*1, 30),
		func(ctx context.Context, event nostr.Event) (bool, string) {
			if acceptedEvent(event) {
				addEventToRootList(event)
				if !migrationMode {
					go fetchQuotedEvents(event)
					go fetchConversation(event)
					go processBlossomBackup(event)
				}
				return false, ""
			}
			return true, "event not allowed"
		},
	)

	relay.OnRequest = policies.SeqRequest(
		policies.NoEmptyFilters,
		policies.NoComplexFilters,
		func(ctx context.Context, filter nostr.Filter) (bool, string) {
			for _, kind := range filter.Kinds {
				if kind == nostr.KindGiftWrap {
					authed, isAuthed := khatru.GetAuthed(ctx)
					if !isAuthed {
						return true, "auth-required: gift wrap events require authentication"
					}
					if authed.Hex() != config.OwnerPubkey {
						return true, "restricted: only the relay owner can access gift wrap events"
					}
					return false, ""
				}
			}
			return false, ""
		},
	)

	relay.RejectConnection = policies.ConnectionRateLimiter(10, time.Minute*2, 30)

	mux := relay.Router()

	serverRoot, fsErr := fs.Sub(assets, "template/assets")
	if fsErr != nil {
		log.Fatal(fsErr)
	}
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(serverRoot))))
	mux.Handle("/favicon.ico", http.FileServer(http.FS(serverRoot)))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.New("index").Parse(indexHTML))
		data := struct {
			RelayName        string
			RelayDescription string
			RelayURL         string
			RelayPort        string
			OwnerPubkey      string
		}{
			RelayName:        config.RelayName,
			RelayDescription: config.RelayDescription,
			RelayURL:         config.RelayURL,
			RelayPort:        config.RelayPort,
			OwnerPubkey:      config.OwnerPubkey,
		}
		err := tmpl.Execute(w, data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Blossom config
	bl := blossom.New(relay, config.BlossomPublicURL)
	bl.Store = blossom.EventStoreBlobIndexWrapper{Store: &db, ServiceURL: bl.ServiceURL}
	bl.StoreBlob = func(ctx context.Context, sha256 string, ext string, body []byte) error {
		file, err := os.Create(config.BlossomAssetsPath + sha256)
		if err != nil {
			return err
		}
		if _, err := io.Copy(file, bytes.NewReader(body)); err != nil {
			return err
		}
		if config.BackupBlossomMedia {
			trackOwnerBlossomFile(sha256)
		}
		return nil
	}
	bl.LoadBlob = func(ctx context.Context, sha256 string, ext string) (io.ReadSeeker, *url.URL, error) {
		reader, err := os.Open(config.BlossomAssetsPath + sha256)
		return reader, nil, err
	}
	bl.DeleteBlob = func(ctx context.Context, sha256 string, ext string) error {
		return os.Remove(config.BlossomAssetsPath + sha256)
	}
	bl.RejectUpload = func(ctx context.Context, auth *nostr.Event, size int, ext string) (bool, string, int) {
		if auth != nil && auth.PubKey == ownerPubkey {
			return false, ext, size
		}
		return true, "Not allowed", 403
	}

	// WoT and archiving procedures
	var wg sync.WaitGroup
	wg.Add(1)
	interval := time.Duration(config.RefreshInterval) * time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	go func() {
		refreshProfiles(ctx, relay)
		refreshTrustNetwork(ctx, relay)
		wg.Done()
		for {
			<-ticker.C
			refreshProfiles(ctx, relay)
			refreshTrustNetwork(ctx, relay)
		}
	}()

	wg.Wait()

	go func() {
		tickerEX := time.NewTicker(24 * time.Hour)
		tickerOL := time.NewTicker(7 * 24 * time.Hour)
		tickerAR := time.NewTicker(30 * 24 * time.Hour)
		defer tickerEX.Stop()
		defer tickerOL.Stop()
		defer tickerAR.Stop()
		for {
			select {
			case <-tickerEX.C:
				fetchExternalThreadUpdates(ThreadExternal, 24*time.Hour)
			case <-tickerOL.C:
				fetchExternalThreadUpdates(ThreadOld, 7*24*time.Hour)
			case <-tickerAR.C:
				fetchExternalThreadUpdates(ThreadArchived, 30*24*time.Hour)
			}
		}
	}()

	log.Println("🎉 Relay running on port", config.RelayPort)
	err := http.ListenAndServe(":"+config.RelayPort, relay)
	if err != nil {
		log.Fatal(err)
	}
}
