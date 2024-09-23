![image](logo.png)

Chronicle is a personal relay [Nostr](https://njump.me), built on the [Khatru](https://khatru.nostr.technology) framework, that stores complete conversations in which the owner has taken part and nothing else: pure signal.  
This is possible since writing is limited to the threads in which the owner has partecipated (either as an original poster or with a reply/zap/reaction), and only to his trusted network (WoT), to protect against spam.

Chronicle fits well in the Outbox model, so you can use it as your read/write relay, and it also automatically becomes a space-efficient backup relay.

## How it works

Every incoming event is verified against some simple rules.  
If it's signed by the relay owner, it is automatically accepted.  
If it is posted by someone else it is checked if it is part of in a conversation in which the owner participated *and* if the author is in the owner's social graph (to the 2nd degree), then it is accepted, otherwise it is rejected.

If an event published by the owner refers to a conversation that is not yet known by the relay, it tries to fetch it.

## How to run

After cloning the repo create an `.env` file based on the example provided in the repository and personalize it:


```bash
# Your pubkey, in hex format
OWNER_PUBKEY="xxxxxxxxxxxxxxxxxxxxxxxxxxx...xxx"

# Relay info, for NIP-11
RELAY_NAME="YourRelayName"
RELAY_DESCRIPTION="Your relay description"
RELAY_URL="wss://chronicle.xxxxxxxx.com"
RELAY_ICON="https://chronicle.xxxxxxxx.com/web/icon.png"
RELAY_CONTACT="your_email_or_website"

# The relay pubkey, in hex format
RELAY_PUBKEY="RelayPublicKey"

# The path you would like the database to be saved
# The path can be relative to the binary, or absolute
DB_PATH="db/"

# Interval in hours to refresh the web of trust
REFRESH_INTERVAL_HOURS=24

# How many followers before they're allowed in the WoT
MINIMUM_FOLLOWERS=3

# Periodically try fetch notes from other relays
FETCH_SYNC="FALSE"
```

Build it with `go install` or `go build`, then run it.

## Credits

Chronicle uses some code from the great [wot-relay](https://github.com/bitvora/wot-relay).

## License

This project is licensed under the MIT License.
