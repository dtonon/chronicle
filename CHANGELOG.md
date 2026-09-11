# Changelog

All notable changes to this project are documented here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Dockerfile and entrypoint that build the `.env` file from the container environment, with a Docker README
- The relay info reports the release number and the build commit

### Fixed

- Close the Blossom index lookup before deleting the event
- Rotate the relay pool hourly to release leaked goroutines
- Guard the root notes map with a mutex
- Never query the event store with a zero limit

## [0.6.1] - 2026-04-30

### Added

- `NEGENTROPY` disables negentropy sync
- `NEGENTROPY_AUTH` restricts negentropy to the relay owner via NIP-42 auth, enabled by default

## [0.6.0] - 2026-04-10

### Added

- Negentropy sync

## [0.5.0] - 2026-04-03

### Added

- NIP-42 auth for gift wrap events and personal notes (see [Manent](https://github.com/dtonon/manent))
- `FETCH_ALL_INTERACTIONS` stores reactions and zaps from other users, discarded by default
- `SKIP_DELETIONS` discards delete requests for other users' notes
- Migration tool from badger to bbolt (`cmd/migrate`) and a migration structure for the root notes list
- Users outside the WoT the owner interacts with are whitelisted for the thread
- The OP's NIP-65 relays are queried when fetching a full thread

### Changed

- New optimized fetch logic to store full threads, with reactions and zaps tracking the full thread
- Migrated from badger to bbolt (boltdb)
- Updated to the latest Khatru engine
- The root notes list records the state of each thread, with external threads marked separately

### Removed

- The sync option

### Fixed

- Fetching the root note on reactions and zaps

A migration is needed when upgrading from v0.4.x, see the [README](https://github.com/dtonon/chronicle?tab=readme-ov-file#migration-from-v04x-to-v050).

## [0.4.2] - 2026-03-27

### Changed

- The WoT is enforced when fetching the full thread
- Better log for the Blossom owner files bootstrap

### Fixed

- Wrong WoT membership check in `belongsToWotNetwork()`

## [0.4.1] - 2025-08-17

### Fixed

- Duplicate processing of the event when fetching the full conversation

## [0.4.0] - 2025-08-12

### Added

- Backup of the Blossom media of all notes, not only the owner's, with a size limit that does not apply to the owner
- Fallback download mechanism for the Blossom backup, verifying the full file
- Bootstrap of the Blossom backup downloads the missing media
- Blossom media are backed up also for synced events

### Changed

- Renamed some Blossom backup flags
- The full conversation is fetched also for root notes

### Removed

- Fallback download of media from a hardcoded list of servers

## [0.3.2] - 2025-01-01

### Fixed

- A bug in retrieving the full conversation

## [0.3.1] - 2024-12-23

### Changed

- Updated to Khatru v0.14.0
- Plain deploy task in the justfile

### Fixed

- Error about variable context

## [0.3] - 2024-11-28

### Added

- Blossom media server
- `POW_WHITELIST` and `POW_DM_WHITELIST` options to bypass the WoT
- Gift wrapped events are accepted
- Quoted events are fetched
- Justfile and Dockerfile for cross compiling

### Fixed

- Duplicate events are no longer saved
- Excessive number of concurrent requests
- Quoted events request

## [0.2.1] - 2024-10-08

### Changed

- Upgraded badger, eventstore and go-nostr
- Removed the relay pubkey from the relay info

## [0.2.0] - 2024-09-25

### Added

- Reactions, zaps and deletions of replies are accepted

### Changed

- Badger is the default storage
- Updated relays seed list

## [0.1.0] - 2024-09-24

### Added

- First release: personal relay with WoT filtering, storing the complete conversations where the owner participated
