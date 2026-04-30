package main

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	OwnerPubkey        string
	RelayName          string
	RelayDescription   string
	DBPath             string
	RelayURL           string
	RelayPort          string
	RefreshInterval    int
	MinFollowers       int
	RelayContact       string
	RelayIcon          string
	PowWhitelist       int
	PowDMWhitelist     int
	BlossomAssetsPath  string
	BlossomPublicURL   string
	BackupBlossomMedia  bool
	MaxFileSizeMB       int
	FetchAllInteractions bool
	SkipDeletions        bool
	Negentropy           bool
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

	if os.Getenv("POW_WHITELIST") == "" {
		os.Setenv("POW_WHITELIST", "999")
	}
	if os.Getenv("POW_DM_WHITELIST") == "" {
		os.Setenv("POW_DM_WHITELIST", "999")
	}

	if os.Getenv("BLOSSOM_BACKUP_MEDIA") == "" {
		os.Setenv("BLOSSOM_BACKUP_MEDIA", "FALSE")
	}

	if os.Getenv("FETCH_ALL_INTERACTIONS") == "" {
		os.Setenv("FETCH_ALL_INTERACTIONS", "FALSE")
	}

	if os.Getenv("SKIP_DELETIONS") == "" {
		os.Setenv("SKIP_DELETIONS", "FALSE")
	}

	if os.Getenv("NEGENTROPY") == "" {
		os.Setenv("NEGENTROPY", "FALSE")
	}

	if os.Getenv("BLOSSOM_MAX_FILE_MB") == "" {
		os.Setenv("BLOSSOM_MAX_FILE_MB", "10")
	}

	minFollowers, _ := strconv.Atoi(os.Getenv("MIN_FOLLOWERS"))
	PowWhitelist, _ := strconv.Atoi(os.Getenv("POW_WHITELIST"))
	PowDMWhitelist, _ := strconv.Atoi(os.Getenv("POW_DM_WHITELIST"))
	maxFileSizeMB, _ := strconv.Atoi(os.Getenv("BLOSSOM_MAX_FILE_MB"))

	config := Config{
		OwnerPubkey:        getEnv("OWNER_PUBKEY"),
		RelayName:          getEnv("RELAY_NAME"),
		RelayDescription:   getEnv("RELAY_DESCRIPTION"),
		RelayContact:       getEnv("RELAY_CONTACT"),
		RelayIcon:          getEnv("RELAY_ICON"),
		DBPath:             getEnv("DB_PATH"),
		RelayURL:           getEnv("RELAY_URL"),
		RelayPort:          getEnv("RELAY_PORT"),
		RefreshInterval:    refreshInterval,
		MinFollowers:       minFollowers,
		PowWhitelist:       PowWhitelist,
		PowDMWhitelist:     PowDMWhitelist,
		BlossomAssetsPath:  getEnv("BLOSSOM_ASSETS_PATH"),
		BlossomPublicURL:   getEnv("BLOSSOM_PUBLIC_URL"),
		BackupBlossomMedia:  getEnv("BLOSSOM_BACKUP_MEDIA") == "TRUE",
		MaxFileSizeMB:       maxFileSizeMB,
		FetchAllInteractions: getEnv("FETCH_ALL_INTERACTIONS") == "TRUE",
		SkipDeletions:        getEnv("SKIP_DELETIONS") == "TRUE",
		Negentropy:           getEnv("NEGENTROPY") == "TRUE",
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
