![image](logo.png)

Chronicle is a personal relay [Nostr](https://njump.me), built on the [Khatru](https://khatru.nostr.technology) framework, that stores complete conversations, media included, in which the owner has taken part and nothing else: pure signal.  
This is possible since writing is limited to the threads in which the owner has partecipated (either as an original poster or with a reply/zap/reaction), and only to his trusted network (WoT), to protect against spam.

Chronicle fits well in the Outbox model, so you can use it as your read/write relay, and it also automatically becomes a space-efficient backup relay.

It also include a Blossom media server, so you can use it to store all your images and attachments; the Blossom server can also optionally backup media for other authors, to create a fallback if the original blobs got lost (the BUD/NIP that manages the retrieval process is in progress).

## How it works

Every incoming event is verified against some simple rules.  
If it's signed by the relay owner, it is automatically accepted.  
If it is posted by someone else it is checked if it is part of in a conversation in which the owner participated *and* if the author is in the owner's social graph (to the 2nd degree), then it is accepted, otherwise it is rejected.  
A couple of options (_POW\_*_, see below) permit to whitelist an event and bypass the WoT, if an event has a sufficient PoW.

If an event published by the owner refers to a conversation that is not yet known by the relay, it tries to fetch it.

Conversations that were not started by the relay owner, but in which they are participating, are automatically monitored with a decaying frequence for updates.

Direct Blossom uploads are restricted to the relay owner.

## Direct messages and private notes

Chronicle supports NIP-42 auth for gift wrap messages and private notes (see [Manent](https://github.com/dtonon/manent)); the auth check is applied transparently only on the specific events, so you can use your plain host, without any additional path, as private relay.

## Features highlight

It works nicely as inbox/outbox/dm relay.  
It is a space-efficient backup relay.  
It offers spam protection by WoT.  
It includes a Blossom media server.
It backup other authors media.

## Configure

After cloning the repo create an `.env` file based on the example provided in the repository and personalize it:


```bash
# Your pubkey, in hex format
OWNER_PUBKEY="xxxxxxxxxxxxxxxxxxxxxxxxxxx...xxx"

# Relay info, for NIP-11
RELAY_NAME="YourRelayName"
RELAY_DESCRIPTION="Your relay description"
RELAY_URL="wss://chronicle.xxxxxxxx.com"
RELAY_ICON="https://chronicle.xxxxxxxx.com/assets/icon.png"
RELAY_CONTACT="your_email_or_website"

# The path you would like the database to be saved
# The path can be relative to the binary, or absolute
DB_PATH="db/"

# Interval in hours to refresh the web of trust
REFRESH_INTERVAL=24

# How many followers before they're allowed in the WoT
MIN_FOLLOWERS=3

# Whitelist events and/or DMs by PoW
# A positivie value enable the whitelist
# Empty disables the whitelist
# Zero (0) disables WoT - not recommended!
POW_WHITELIST=
POW_DM_WHITELIST=

# Define the external public url of the media server
# and the local path where assets are stored
BLOSSOM_PUBLIC_URL="http://localhost:3335"
BLOSSOM_ASSETS_PATH="assets/"

# Enable automatic backup of media for other authors
BLOSSOM_BACKUP_MEDIA="TRUE"

# Accept reactions and zaps for all notes in archived threads, not just owner notes
FETCH_ALL_INTERACTIONS="FALSE"

# Ignore deletion requests from other users, keeping their notes permanently
SKIP_DELETIONS="FALSE"

# Enable negentropy sync support
NEGENTROPY="FALSE"
```

## Build

Build it with `go install` or `go build`, then run it.

By default Chronicle use [bbolt (a boltdb fork)](https://github.com/etcd-io/bbolt) as event storage since it makes easier to cross-compile.  
You can also use [lmdb](https://www.symas.com/lmdb), compiling with:
```
go build -tags=lmdb .
```

## Migration from v0.4.x to v0.5.0 (and following)

Chronicle v0.5.0 use bbolt as a default database, if you are using badger you need to run a migration, since the latter is not supported anymore.

Before starting the migration, as usual, it is highly suggested to backup the db folder.

This is the migration procedure:

1. Download `migrate-linux-amd64`, or build it yourself from the sources, and move it in the same dir of the (old) chronicle binary  
2. Download `chronicle-linux-amd64`, or build it yourself from the sources, and replace the (old) chronicle binary  
3. Manaully run the server with the migration flag: `MIGRATION_MODE=true ./chronicle-linux-amd64`
4. While the server is running, start the migration: `./migrate ./<db> ws://localhost:<port>`, where `<db>`is the database location and `<port>` the relay's port configured in the `.env`
5. When the migration ends, restart the server in the stadard way, without the migration flag

Finish!

## License

This project is licensed under the MIT License.
