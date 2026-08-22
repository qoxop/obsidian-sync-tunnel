# Changelog

All notable changes to Sync Tunnel are documented here. Semantic Versioning compatibility starts with the stable `1.0.0` release.

## Unreleased

### Security

- Removed client-supplied filesystem paths from the online backup API. The server now creates backups only under its managed backup root and verifies them by opaque backup ID.
- Parameterized SQLite `VACUUM INTO`, rejected out-of-root Chunk paths during doctor checks, and made query-limit parsing safe on every Go architecture.
- Required TLS 1.2 or newer for public connectivity probes and added regression coverage for redirect and DNS-rebinding rejection.

## 1.0.0-rc.2 - 2026-08-22

### Administration

- Added a loopback-only React/Ant Design management console for Vaults, devices, pairing, statistics, audit, runtime logs, doctor, backups, verification, and two-phase GC.
- Made Admin Token authentication optional and disabled by default; tokenless mode still enforces loopback Host and same-origin requests, while Docker publishes the admin port only on `127.0.0.1`.
- Replaced routine administration scripts with one `scripts/setup.ps1` entry point and the local Web console. Offline disaster recovery remains a dedicated script because a running service cannot safely replace its own SQLite data set.
- Rewrote the README as a short installation and usage guide, with technical details kept in the architecture and protocol documents.
- Added a local connectivity diagnostics page for the service, public DNS, Cloudflare Tunnel edge DNS, connector readiness, Access-protected health checks, and common Origin errors.

### Reliability and security

- Restricted connectivity probes to public HTTPS hostnames on port 443, rejected private, loopback, documentation, benchmark, and Clash Fake-IP ranges, disabled redirects and proxy inheritance, and revalidated every dial target to prevent SSRF and DNS rebinding.
- Moved SQLite integrity checks to an independent read-only connection so a full doctor run no longer blocks normal statistics or sync requests.
- Added a five-minute successful doctor-result cache with concurrent request coalescing, plus bounded streaming Chunk verification to reduce repeated disk scans and peak memory use.
- Documented a persistent Clash Verge TUN/Fake-IP configuration for Cloudflare Error 1033 without requiring users to disable TUN.

### Delivery

- Limited public distribution to the unofficial Obsidian plugin through GitHub Releases and BRAT.
- Removed GHCR, multi-architecture image publication, Cosign signing, and the scheduled Nightly workflow. The server remains source-distributed and is built locally from the Dockerfile/Compose files at the same immutable Tag as the plugin.
- Grouped Dependabot updates by ecosystem, limited each ecosystem to one open PR, disabled automatic rebases, pinned the plugin to the compatible TypeScript major, and added monthly management-Web dependency updates.
- Removed the superseded native Windows service deployment path and pre-1.0 process documents; Docker Desktop is now the single supported server deployment model.
- Replaced planning and point-in-time test records with a code-aligned architecture document, durable test guide, and task-oriented README.

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
- Added Windows CI, Ubuntu race detection, CodeQL, Dependabot, plugin checksums and a CycloneDX plugin SBOM. The initial server-image publication job was cancelled and is not part of the supported distribution model.
- Reworked Docker Desktop deployment to use separate public/admin loopback ports, Windows bind-mounted data/backups, and a local Admin Token secret.

## 0.2.0 - 2026-08-16

Historical pre-1.0 MVP with the Protocol v2 draft, initial snapshot reconciliation, first-sync preview, profiles, schema migration foundation, unit tests, restart notices, and cross-platform path-collision detection. It is not protocol-compatible with 1.0.

## 0.1.0

Initial Go/SQLite and Obsidian plugin MVP. It is not protocol-compatible with 1.0.
