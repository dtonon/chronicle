package main

import (
	"bufio"
	"os"
	"strings"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/nip10"
)

const (
	ThreadExternal = "EX"
	ThreadOld      = "OL"
	ThreadArchived = "AR"
)

// RootNotes tracks thread root IDs with their state
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

func (r *RootNotes) Add(id, state string) {
	r.notes[id] = state
}

func (r *RootNotes) Include(id string) bool {
	_, exists := r.notes[id]
	return exists
}

func (r *RootNotes) State(id string) string {
	return r.notes[id]
}

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
		rootNotesList.Add(event.ID.Hex(), "")
		return
	}

	rootID := rootReference.AsTagReference()

	if event.PubKey.Hex() != config.OwnerPubkey {
		if !rootNotesList.Include(rootID) {
			rootNotesList.Add(rootID, "")
		}
		return
	}

	existing := rootNotesList.State(rootID)
	if existing == ThreadOld || existing == ThreadArchived {
		rootNotesList.Add(rootID, ThreadExternal)
		return
	}
	if existing == ThreadExternal || existing == "" && rootNotesList.Include(rootID) {
		return
	}

	refID, err := nostr.IDFromHex(rootID)
	if err != nil {
		return
	}
	state := ThreadExternal
	for rootEvent := range store.QueryEvents(nostr.Filter{IDs: []nostr.ID{refID}}, 1) {
		if rootEvent.PubKey.Hex() == config.OwnerPubkey {
			state = ""
		}
	}
	rootNotesList.Add(rootID, state)
}

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
