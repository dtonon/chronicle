package main

import (
	"bufio"
	"os"
)

// RootNotes is a structure to hold the strings in memory.
type RootNotes struct {
	strings  map[string]struct{}
	filename string
}

// NewRootNotes creates a new RootNotes with a specified filename for persistence.
func NewRootNotes(filename string) *RootNotes {
	return &RootNotes{
		strings:  make(map[string]struct{}),
		filename: filename,
	}
}

func (r *RootNotes) Size() int {
	return len(r.strings)
}

// Add adds a string to the set if it is not already present and saves it to the file.
func (r *RootNotes) Add(str string) *string {
	if r.Include(str) {
		// Return nil if the string is already present
		return nil
	}
	r.strings[str] = struct{}{}
	if err := r.AppendToFile(str); err != nil {
		return nil
	}
	return &str
}

// Include checks if a string is present in the set.
func (r *RootNotes) Include(str string) bool {
	_, exists := r.strings[str]
	return exists
}

// AppendToFile appends the current string to the text file.
func (r *RootNotes) AppendToFile(str string) error {
	file, err := os.OpenFile(r.filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) // Open in append mode
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	_, err = writer.WriteString(str + "\n")
	if err != nil {
		return err
	}
	return writer.Flush()
}

// LoadFromFile loads strings from a text file into the set.
func (r *RootNotes) LoadFromFile() error {
	// Check if the file exists
	if _, err := os.Stat(r.filename); os.IsNotExist(err) {
		// If the file does not exist, create an empty file
		_, err := os.Create(r.filename)
		if err != nil {
			return err
		}
		// Initialize the strings map as empty
		r.strings = make(map[string]struct{})
		return nil
	}

	// If the file exists, read the data
	file, err := os.Open(r.filename)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		r.strings[scanner.Text()] = struct{}{}
	}
	return scanner.Err()
}
