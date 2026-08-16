# Changelog

All notable changes to Sync Tunnel are documented here. The project follows Semantic Versioning once a change is published.

## Unreleased

### Added

- Persistent, deduplicated Vault file-event queue with a two-second automatic-sync debounce.
- Client scan cache keyed by path, size, and modification time, plus hourly metadata scans and daily integrity rehashing.
- Transactional operation-ID records for retrying whole-file uploads and deletions without allocating duplicate revisions.
- Persistent client outbox with post-restart operation-result lookup and safe replay of uncommitted mutations.
- Persistent download inbox with size and SHA-256 verification, same-directory temporary files, backup-based replacement, and restart recovery.
- Fixed-size content-addressed Chunk storage, missing-Chunk queries, Manifest commits, and capability-negotiated Chunk upload/download.
- Transactional high-confidence rename operation with source tombstone, destination change, and post-restart result recovery.

### Changed

- Ordinary unchanged synchronization reuses cached hashes instead of reading every file body.
- Incremental deletion propagation is limited to paths observed by the event queue; periodic full scans remain the safety net.
- Whole-file clients attach a UUID operation ID; servers remain compatible with clients that omit it.
- A client with pending outbox entries refuses to write through a downgraded server that cannot prove operation results.
- Sync Tunnel temporary download and backup files are always excluded from Vault synchronization.
- Chunked Manifest commits keep the whole-file download path compatible during the 0.3 migration window.
- Rename events fall back to upload plus delete unless the old baseline and new content hash prove identity.

## 0.2.0 - 2026-08-16

### Added

- Product roadmap, Protocol v2 draft, threat model, and release test strategy.
- Versioned SQLite schema migration foundation.
- Authenticated `/api/v2/server-info` capability discovery endpoint.
- Stable, revision-bound Vault snapshot API with path pagination.
- Client snapshot reconciliation when synchronization filters change.
- Explicit first-sync preview with safe merge, remote-authoritative, and local-authoritative modes.
- Notes-only, recommended-safe, full-Vault, and custom synchronization profiles.
- Plugin unit tests, in-memory Vault tests, and first synchronization-engine state tests.
- Connection test for Tunnel, Access, API token, Vault ID, and server protocol compatibility.
- Immediate settings refresh after first-sync completion, removing the stale preview action.
- Restart notice when synchronized plugins, themes, snippets, or community-plugin state changes.
- Cross-platform case and Unicode path-collision detection.

### Changed

- Sync Tunnel's current and legacy plugin directories are now fully device-local and never synchronized.
- Existing 0.1 installations keep their initialized state; new installations require explicit first-sync confirmation.
- New installations default to the recommended-safe profile; existing 0.1 installations retain full-Vault behavior.

### Security

- Prevent one device from replacing another device's running Sync Tunnel bundle through Vault synchronization.
- Preserve an untracked local file as a conflict copy before a snapshot applies different remote content.
