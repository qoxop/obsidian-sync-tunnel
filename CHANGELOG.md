# Changelog

All notable changes to Sync Tunnel are documented here. Semantic Versioning compatibility starts with the stable `1.0.0` release.

## 1.0.0-rc.1 - 2026-08-19

### Breaking

- Replaced all pre-1.0 protocol drafts with one final `/api/v1` protocol.
- Removed global sync tokens, client-supplied device IDs, legacy capability names, and protocol downgrade paths.
- Pre-1.0 plugin state is intentionally reset; every device must pair again and explicitly approve its first synchronization.
- Renamed the file limit configuration to `max_file_bytes`, `--max-file-bytes`, and `OBSIDIAN_SYNC_MAX_FILE_BYTES`.

### Server

- Added logical Vault administration, one-time pairing, server-assigned device identities, scoped per-device credentials, rotation, revocation, retirement, and audit events.
- Added stable snapshots, persistent idempotent operations, whole-file and Chunk/Manifest transfers, atomic rename and batch delete, and per-device ACK watermarks.
- Added version history, deletion recovery, restore-as-new-revision, deterministic two-phase GC, quotas, file limits, rate limits, and disk reserve protection.
- Added independent loopback-only Admin API, online SQLite/Chunk backups, SHA-256 manifests, verification, safe restore, doctor, and statistics.

### Plugin

- Added setup wizard, safer default profile, SecretStorage device credentials, protocol enforcement, Activity history, progress, pause/cancel boundaries, and exact restart-path notices.
- Added persistent outbox/inbox restart recovery, desktop streamed large-file I/O, mobile file limits, conflict center with text comparison and four resolution choices, history browser, restore, credential rotation, and sanitized diagnostics.
- Added Chinese/English UI selection and explicit protection for Sync Tunnel's own state, temporary files, caches, logs, and workspace state.

### Delivery and quality

- Added deterministic multi-device state-machine tests, opt-in 10,000-file tests, final API client tests, corruption tests, and an isolated end-to-end smoke self-test.
- Added Windows CI, Ubuntu race detection, Nightly scale/image builds, CodeQL, Dependabot, multi-architecture GHCR releases, checksums, SBOM, provenance, and keyless Cosign image signing.
- Reworked Docker Desktop deployment to use separate public/admin loopback ports, Windows bind-mounted data/backups, and a local Admin Token secret.

## 0.2.0 - 2026-08-16

Historical pre-1.0 MVP with the Protocol v2 draft, initial snapshot reconciliation, first-sync preview, profiles, schema migration foundation, unit tests, restart notices, and cross-platform path-collision detection. It is not protocol-compatible with 1.0.

## 0.1.0

Initial Go/SQLite and Obsidian plugin MVP. It is not protocol-compatible with 1.0.
