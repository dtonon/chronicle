package main

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/fiatjaf/eventstore"
	"github.com/fiatjaf/khatru"
	"github.com/fiatjaf/khatru/policies"
	"github.com/joho/godotenv"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip10"
)

//go:embed template/index.html
var indexHTML string

//go:embed template/assets
var assets embed.FS

var (
	version string
)

type Config struct {
	OwnerPubkey      string
	RelayName        string
	RelayDescription string
	DBPath           string
	RelayURL         string
	RelayPort        string
	RefreshInterval  int
	MinFollowers     int
	FetchSync        bool
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
var relay *khatru.Relay

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
	pool = nostr.NewSimplePool(ctx)
	config = LoadConfig()

	relay.Info.Name = config.RelayName
	relay.Info.PubKey = config.OwnerPubkey
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
			go fetchQuotedEvents(*event)
			go fetchConversation(*event)
			return false, ""
		}
		return true, "event not allowed"
	})

	// WoT and archiving procedures
	var wg sync.WaitGroup
	wg.Add(1) // We expect one goroutine to finish
	interval := time.Duration(config.RefreshInterval) * time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	go func() {
		refreshProfiles(ctx)
		refreshTrustNetwork(ctx, relay)
		wg.Done()
		for {
			if config.FetchSync {
				archiveTrustedNotes(ctx, relay)
			}
			<-ticker.C // Wait for the ticker to tick
			refreshProfiles(ctx)
			refreshTrustNetwork(ctx, relay)
		}
	}()

	// Wait for the first execution to complete
	wg.Wait()

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

	log.Println("🎉 Relay running on port", config.RelayPort)
	err := http.ListenAndServe(":"+config.RelayPort, relay)
	if err != nil {
		log.Fatal(err)
	}
}

func LoadConfig() Config {
	godotenv.Load(".env")

	if os.Getenv("REFRESH_INTERVAL") == "" {
		os.Setenv("REFRESH_INTERVAL", "3")
	}

	refreshInterval, _ := strconv.Atoi(os.Getenv("REFRESH_INTERVAL"))
	log.Println("📝 Set refresh interval to", refreshInterval, "hours")

	if os.Getenv("MIN_FOLLOWERS") == "" {
		os.Setenv("MIN_FOLLOWERS", "1")
	}

	if os.Getenv("FETCH_SYNC") == "" {
		os.Setenv("FETCH_SYNC", "TRUE")
	}

	minFollowers, _ := strconv.Atoi(os.Getenv("MIN_FOLLOWERS"))

	config := Config{
		OwnerPubkey:      getEnv("OWNER_PUBKEY"),
		RelayName:        getEnv("RELAY_NAME"),
		RelayDescription: getEnv("RELAY_DESCRIPTION"),
		RelayContact:     getEnv("RELAY_CONTACT"),
		RelayIcon:        getEnv("RELAY_ICON"),
		DBPath:           getEnv("DB_PATH"),
		RelayURL:         getEnv("RELAY_URL"),
		RelayPort:        getEnv("RELAY_PORT"),
		RefreshInterval:  refreshInterval,
		MinFollowers:     minFollowers,
		FetchSync:        getEnv("FETCH_SYNC") == "TRUE",
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

func fetchConversation(event nostr.Event) {
	rootReference := nip10.GetThreadRoot(event.Tags)
	if rootReference == nil || // It's not a reply
		rootNotesList.Include(rootReference.Value()) { // It's archived
		return // We don't need the full conversation
	}

	ctx := context.Background()
	timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	eventID := rootReference.Value()
	eventRelay := rootReference.Relay()

	go func() {
		filters := []nostr.Filter{
			{
				IDs: []string{eventID},
			},
			{
				Kinds: []int{
					nostr.KindArticle,
					nostr.KindDeletion,
					nostr.KindReaction,
					nostr.KindZapRequest,
					nostr.KindZap,
					nostr.KindTextNote,
				},
				Tags: nostr.TagMap{"e": []string{eventID}},
			},
		}

		for ev := range pool.SubMany(timeout, append([]string{eventRelay}, seedRelays...), filters) {
			wdb.Publish(ctx, *ev.Event)
			go fetchQuotedEvents(*ev.Event)
		}
	}()

	<-timeout.Done()
}

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

		for ev := range pool.SubMany(timeout, append(quoteRelays, seedRelays...), filters) {
			wdb.Publish(ctx, *ev.Event)
		}
	}()

	<-timeout.Done()
}

func belongsToValidThread(event nostr.Event) bool {
	eReference := nip10.GetThreadRoot(event.Tags)
	if eReference == nil {
		// We already accept root notes by owner
		return false
	}

	if event.Kind == nostr.KindTextNote ||
		event.Kind == nostr.KindArticle {

		rootCheck := rootNotesList.Include(eReference.Value())
		return rootCheck
	}

	// The event refers to a note in the thread
	if event.Kind == nostr.KindDeletion ||
		event.Kind == nostr.KindReaction ||
		event.Kind == nostr.KindZapRequest ||
		event.Kind == nostr.KindZap {

		ctx := context.Background()
		_, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		filter := nostr.Filter{
			IDs: []string{eReference.Value()},
		}
		eventChan, _ := wdb.QueryEvents(ctx, filter)
		for range eventChan {
			return true
		}
	}

	return false
}

func addEventToRootList(event nostr.Event) {
	// Add only notes and articles to the root list
	if event.Kind != nostr.KindTextNote &&
		event.Kind != nostr.KindArticle {
		return
	}

	rootReference := nip10.GetThreadRoot(event.Tags)
	var rootReferenceValue string
	if rootReference == nil { // Is a root post
		rootReferenceValue = event.ID
	} else { // Is a reply
		rootReferenceValue = rootReference.Value()
	}
	rootNotesList.Add(rootReferenceValue)
}
