#!/bin/bash

# Define the output file
OUTPUT_FILE=".env"

# Define the required environment variables
REQUIRED_VARS=("OWNER_PUBKEY")

# Define the all environment variables
ALL_VARS=("OWNER_PUBKEY" "RELAY_NAME" "RELAY_DESCRIPTION" "RELAY_URL" "RELAY_ICON" "RELAY_CONTACT" "DB_PATH" "REFRESH_INTERVAL" "MIN_FOLLOWERS" "FETCH_SYNC" "POW_WHITELIST" "POW_DM_WHITELIST")

# Check if all required environment variables are set
for var in "${REQUIRED_VARS[@]}"; do
  if [ -z "${!var}" ]; then
    echo "Error: Environment variable $var is not set."
    exit 1
  fi
done

#Recreate conf file
truncate -s 0 $OUTPUT_FILE

# Write environment variables to the output file
for var in "${ALL_VARS[@]}"; do
  echo "$var=`printenv ${var}`" >> "$OUTPUT_FILE"
done

./chronicle
