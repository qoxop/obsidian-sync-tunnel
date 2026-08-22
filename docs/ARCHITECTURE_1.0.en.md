# Sync Tunnel 1.0 Architecture

## Product model

1.0 serves one trusted administrator with multiple logical Vaults and devices. A logical Vault ID names a server-side sync space; it is independent from a local Obsidian folder name. This is not a multi-tenant collaboration service.

Pre-1.0 is outside the compatibility surface. The server and plugin maintain one protocol, authentication model, and client-state schema.

## Runtime boundaries

- Obsidian clients reach the public API through HTTPS, Cloudflare Tunnel, and Windows `cloudflared`.
- Cloudflare's origin is `http://127.0.0.1:8787` only.
- Windows administration reaches a separate `http://127.0.0.1:8788` Admin API with a local Admin Token.
- Both host ports are loopback-only. The Admin API must never have a Cloudflare Public Hostname.
- SQLite/WAL, content-addressed Chunks, and JSONL logs live in a Windows bind mount. Online backups use a separate bind mount.

## Code boundaries

| Component | Responsibility |
|---|---|
| `cmd/obsidian-sync-server` | CLI, configuration, two HTTP servers, logging, shutdown and Windows service |
| `internal/httpapi` | Public/Admin routing, auth, rate/body limits, structured errors and sanitized logs |
| `internal/store` | Vaults, devices, credentials, revisions, Chunks, history, ACK, GC, backup and doctor |
| `plugin/src/api-client.ts` | One final `/api/v1` client, device Bearer and optional Cloudflare Access headers |
| `plugin/src/sync-engine.ts` | Snapshot reconciliation, scanning, outbox/inbox, Chunks, conflicts, ACK and progress |
| scanner/file I/O | Profiles, exclusions, self-protection, desktop streams and mobile memory limits |
| plugin UI | Setup, Activity, conflicts, history, diagnostics and restart paths |

The HTTP layer does not construct SQL or own conflict semantics; Store has no HTTP dependency; the sync engine depends on testable API/DataAdapter boundaries.

## Consistency

Every path has a current revision and an append-only change history. Writes carry a base revision and operation UUID. Identical retries do not allocate a second revision; stale writes return current state so the plugin can preserve both byte sequences. Large content is split into SHA-256 Chunks and committed through a Manifest. Downloads use same-directory temporary files, size/hash verification, and atomic replacement. Persistent outbox/inbox records recover across client restarts.

After persisting local state, a client ACKs its cursor. GC uses the minimum ACK of active devices plus retention time/version rules, and always requires an immutable plan plus matching plan hash. History restore creates a new revision. Online backup uses SQLite `VACUUM INTO`, copies the Chunk tree, and writes a per-file SHA-256 manifest.

## Profiles and non-goals

Notes, Recommended, Full and Custom profiles are supported. Recommended includes common settings and plugin bundles but not other plugins' `data.json`; Full can copy those files and their secrets, so it requires explicit selection. Sync Tunnel's own directory and device-local artifacts are always excluded.

1.0 does not provide E2EE, user sharing, Web editing, character-level merges, compatibility with 0.x protocols, or safe coexistence with another real-time bidirectional sync system on the same Vault.
