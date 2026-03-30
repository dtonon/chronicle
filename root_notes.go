package main

import (
	"bufio"
	"context"
	"os"
	"strings"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip10"
)

const (
	ThreadExternal = "EX"
	ThreadOld      = "OL"
	ThreadArchived = "AR"
)

// RootNotes tracks thread root IDs with their state
// Internal threads (owner is OP) have an empty state
// External threads use EX, OL, or AR to drive periodic fetch frequency
type RootNotes struct {
	notes    map[string]string // id -> state ("", "EX", "OL", "AR")
	filename string
}

func NewRootNotes(filename string) *RootNotes {
	return &RootNotes{
		notes:    make(map[string]string),
		filename: filename,
	}
}

func (r *RootNotes) Size() int {
	return len(r.notes)
}

// Add sets the state for a root note ID, overwriting any existing state
func (r *RootNotes) Add(id, state string) {
	r.notes[id] = state
}

// Include checks if a root note ID is tracked
func (r *RootNotes) Include(id string) bool {
	_, exists := r.notes[id]
	return exists
}

// State returns the state of a tracked root note ID
func (r *RootNotes) State(id string) string {
	return r.notes[id]
}

// SaveToFile rewrites the full file from the in-memory map
func (r *RootNotes) SaveToFile() error {
	file, err := os.Create(r.filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for id, state := range r.notes {
		line := id
		if state != "" {
			line = id + " " + state
		}
		if _, err := writer.WriteString(line + "\n"); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func addEventToRootList(event nostr.Event) {
	if event.Kind != nostr.KindTextNote &&
		event.Kind != nostr.KindArticle {
		return
	}

	rootReference := nip10.GetThreadRoot(event.Tags)

	if rootReference == nil {
		// Event is a root post — always internal
		rootNotesList.Add(event.ID, "")
		return
	}

	rootID := rootReference.Value()

	if event.PubKey != config.OwnerPubkey {
		// Non-owner event: track only if not already known, don't change existing state
		if !rootNotesList.Include(rootID) {
			rootNotesList.Add(rootID, "")
		}
		return
	}

	// Owner replied to a thread — determine internal vs external
	existing := rootNotesList.State(rootID)
	if existing == ThreadOld || existing == ThreadArchived {
		// Reactivate: owner interacted again
		rootNotesList.Add(rootID, ThreadExternal)
		return
	}
	if existing == ThreadExternal || existing == "" && rootNotesList.Include(rootID) {
		// Already correctly tracked, no change
		return
	}

	// Not yet tracked — check root author to determine state
	ctx := context.Background()
	filter := nostr.Filter{IDs: []string{rootID}}
	eventChan, _ := wdb.QueryEvents(ctx, filter)
	state := ThreadExternal
	for rootEvent := range eventChan {
		if rootEvent.PubKey == config.OwnerPubkey {
			state = ""
		}
	}
	rootNotesList.Add(rootID, state)
}

// LoadFromFile loads root notes from the file into memory
// Each line is either "<id>" (internal) or "<id> <state>" (external)
func (r *RootNotes) LoadFromFile() error {
	if _, err := os.Stat(r.filename); os.IsNotExist(err) {
		_, err := os.Create(r.filename)
		return err
	}

	file, err := os.Open(r.filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			r.notes[parts[0]] = parts[1]
		} else {
			r.notes[parts[0]] = ""
		}
	}
	return scanner.Err()
}
