package main

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/khatru/policies"
	"github.com/joho/godotenv"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip10"
)

var (
	version string
)

type Config struct {
	OwnerPubkey      string
	RelayName        string
	RelayPubkey      string
	RelayDescription string
	DBPath           string
	RelayURL         string
	IndexPath        string
	StaticPath       string
	RefreshInterval  int
	MinimumFollowers int
	ArchivalSync     bool
	RelayContact     string
	RelayIcon        string
}

var pool *nostr.SimplePool
var wdb nostr.RelayStore
var rootNotesList *RootNotes
var relays []string
var config Config
var trustNetwork []string
var oneHopNetwork []string
var trustNetworkMap map[string]bool
var pubkeyFollowerCount = make(map[string]int)
var trustedNotes uint64
var untrustedNotes uint64

func main() {
	nostr.InfoLogger = log.New(io.Discard, "", 0)
	magenta := "\033[91m"
	gray := "\033[90m"
	reset := "\033[0m"

	art := magenta + `
  __  __                           _           
 |  \/  | ___ _ __ ___   ___  _ __(_) ___  ___ 
 | |\/| |/ _ \ '_ ' _ \ / _ \| '__| |/ _ \/ __|
 | |  | |  __/ | | | | | (_) | |  | |  __/\__ \
 |_|  |_|\___|_| |_| |_|\___/|_|  |_|\___||___/` + reset + `
     a Nostr relay to store your conversations` + gray + `
                        powered by Khatru
` + reset

	fmt.Println(art)
	log.Println("🚀 Booting up Memories relay")
	relay := khatru.NewRelay()
	ctx := context.Background()
	pool = nostr.NewSimplePool(ctx)
	config = LoadConfig()

	relay.Info.Name = config.RelayName
	relay.Info.PubKey = config.RelayPubkey
	relay.Info.Icon = config.RelayIcon
	relay.Info.Contact = config.RelayContact
	relay.Info.Description = config.RelayDescription
	relay.Info.Software = "Memories Relay"
	relay.Info.Version = version

	appendPubkey(config.RelayPubkey)
	appendPubkey(config.OwnerPubkey)

	db := getDB()
	if err := db.Init(); err != nil {
		panic(err)
	}
	wdb = eventstore.RelayWrapper{Store: &db}

	rootNotesList = NewRootNotes("db/root_notes")
	if err := rootNotesList.LoadFromFile(); err != nil {
		fmt.Println("Error loading strings:", err)
		return
	} else {
		log.Println("🗣️  Monitoring", rootNotesList.Size(), "threads")
	}

	relay.RejectEvent = append(relay.RejectEvent,
		policies.RejectEventsWithBase64Media,
		policies.EventIPRateLimiter(5, time.Minute*1, 30),
	)

	relay.RejectFilter = append(relay.RejectFilter,
		policies.NoEmptyFilters,
		policies.NoComplexFilters,
	)

	relay.RejectConnection = append(relay.RejectConnection,
		policies.ConnectionRateLimiter(10, time.Minute*2, 30),
	)

	relay.StoreEvent = append(relay.StoreEvent, db.SaveEvent)
	relay.QueryEvents = append(relay.QueryEvents, db.QueryEvents)
	relay.DeleteEvent = append(relay.DeleteEvent, db.DeleteEvent)
	relay.RejectEvent = append(relay.RejectEvent, func(ctx context.Context, event *nostr.Event) (bool, string) {
		if acceptedEvent(*event) {
			addEventToRootList(*event)
			return false, ""
		}
		return true, "event not allowed"
	})

	// WoT and archiving procedures
	interval := time.Duration(config.RefreshInterval) * time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	go func() {
		for {
			refreshProfiles(ctx)
			refreshTrustNetwork(ctx, relay)
			if config.ArchivalSync {
				archiveTrustedNotes(ctx, relay)
			}
			<-ticker.C // Wait for the ticker to tick
		}
	}()

	mux := relay.Router()
	static := http.FileServer(http.Dir(config.StaticPath))

	mux.Handle("GET /web/", http.StripPrefix("/web/", static))
	mux.Handle("GET /favicon.ico", http.StripPrefix("/", static))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		tmpl := template.Must(template.ParseFiles(os.Getenv("INDEX_PATH")))
		data := struct {
			RelayName        string
			RelayPubkey      string
			RelayDescription string
			RelayURL         string
		}{
			RelayName:        config.RelayName,
			RelayPubkey:      config.RelayPubkey,
			RelayDescription: config.RelayDescription,
			RelayURL:         config.RelayURL,
		}
		err := tmpl.Execute(w, data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	log.Println("🎉 Relay running on port :3334")
	err := http.ListenAndServe(":3334", relay)
	if err != nil {
		log.Fatal(err)
	}
}

func LoadConfig() Config {
	godotenv.Load(".env")

	if os.Getenv("REFRESH_INTERVAL_HOURS") == "" {
		os.Setenv("REFRESH_INTERVAL_HOURS", "3")
	}

	refreshInterval, _ := strconv.Atoi(os.Getenv("REFRESH_INTERVAL_HOURS"))
	log.Println("📝 Set refresh interval to", refreshInterval, "hours")

	if os.Getenv("MINIMUM_FOLLOWERS") == "" {
		os.Setenv("MINIMUM_FOLLOWERS", "1")
	}

	if os.Getenv("ARCHIVAL_SYNC") == "" {
		os.Setenv("ARCHIVAL_SYNC", "TRUE")
	}

	minimumFollowers, _ := strconv.Atoi(os.Getenv("MINIMUM_FOLLOWERS"))

	config := Config{
		OwnerPubkey:      getEnv("OWNER_PUBKEY"),
		RelayName:        getEnv("RELAY_NAME"),
		RelayPubkey:      getEnv("RELAY_PUBKEY"),
		RelayDescription: getEnv("RELAY_DESCRIPTION"),
		RelayContact:     getEnv("RELAY_CONTACT"),
		RelayIcon:        getEnv("RELAY_ICON"),
		DBPath:           getEnv("DB_PATH"),
		RelayURL:         getEnv("RELAY_URL"),
		IndexPath:        getEnv("INDEX_PATH"),
		StaticPath:       getEnv("STATIC_PATH"),
		RefreshInterval:  refreshInterval,
		MinimumFollowers: minimumFollowers,
		ArchivalSync:     getEnv("ARCHIVAL_SYNC") == "TRUE",
	}

	return config
}

func getEnv(key string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		log.Fatalf("Environment variable %s not set", key)
	}
	return value
}

func acceptedEvent(event nostr.Event) bool {
	if event.PubKey == config.OwnerPubkey {
		return true
	} else if belongsToValidThread(event) && belongsToWotNetwork(event) {
		return true
	} else {
		return false
	}
}

func belongsToValidThread(event nostr.Event) bool {
	rootReference := nip10.GetThreadRoot(event.Tags)
	rootCheck := false
	if rootReference != nil {
		rootCheck = rootNotesList.Include(rootReference.Value())
	}
	return rootCheck || rootNotesList.Include(event.ID)
}

func addEventToRootList(event nostr.Event) {
	rootReference := nip10.GetThreadRoot(event.Tags)
	var rootReferenceValue string
	if rootReference == nil { // Is a root post
		rootReferenceValue = event.ID
	} else { // Is a reply
		rootReferenceValue = rootReference.Value()
	}
	if rootNotesList.Add(rootReferenceValue) != nil {
		log.Println("🗣️  Added new thread: ", rootReferenceValue)
	}
}
