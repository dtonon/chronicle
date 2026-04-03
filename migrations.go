package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

type migration struct {
	number int
	name   string
	run    func() error
}

var migrations []migration

func registerMigration(number int, name string, run func() error) {
	migrations = append(migrations, migration{number, name, run})
}

func migrationsFile() string {
	return config.DBPath + "migrations.txt"
}

func runMigrations() error {
	last, err := lastMigration()
	if err != nil {
		return fmt.Errorf("reading migrations file: %w", err)
	}

	for _, m := range migrations {
		if m.number <= last {
			continue
		}
		log.Printf("🔄 Running migration %04d: %s", m.number, m.name)
		if err := m.run(); err != nil {
			return fmt.Errorf("migration %04d failed: %w", m.number, err)
		}
		if err := setLastMigration(m.number); err != nil {
			return fmt.Errorf("recording migration %04d: %w", m.number, err)
		}
		log.Printf("✅ Migration %04d done", m.number)
	}
	return nil
}

func lastMigration() (int, error) {
	data, err := os.ReadFile(migrationsFile())
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid migrations file content: %w", err)
	}
	return n, nil
}

func setLastMigration(n int) error {
	return os.WriteFile(migrationsFile(), []byte(strconv.Itoa(n)+"\n"), 0644)
}
