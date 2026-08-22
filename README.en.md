# Obsidian Sync Tunnel

English | [简体中文](README.md)

A personal, self-hosted full-Vault sync system for Obsidian. The Go/SQLite server runs in Windows Docker Desktop with Windows bind-mounted persistence, Cloudflare Tunnel provides the HTTPS ingress, and the Obsidian plugin supports desktop and mobile clients.

The current code is `1.0.0-rc.1`: implementation and automated verification are complete, but manual multi-platform acceptance is still required. Do not use it as the only copy of a real Vault or as a replacement for independent backups.

## 1.0 scope

- Single administrator, multiple logical Vaults, multiple devices.
- Notes, attachments, Canvas, themes, CSS, and other plugin bundles; Full Vault can also synchronize other plugins' `data.json`.
- Sync Tunnel's own state, credentials, workspaces, caches, diagnostics, and temporary files remain device-local.
- One-time pairing, server-assigned device IDs, scoped per-device credentials, rotation and revocation.
- Stable snapshots, optimistic revisions, persistent outbox/inbox, Chunk resume, conflicts, history/restore, ACK watermarks, two-phase GC, online backup/verify/restore, quotas and audit.
- Plaintext server storage in 1.0; E2EE is deferred to 2.0.

There is exactly one final `/api/v1` protocol. All 0.x clients, global API tokens, old capabilities, and old client sync state are intentionally incompatible. Upgrade the server and plugin together, use fresh 1.0 server data, and pair every device again.

## Documentation

- [Architecture](docs/ARCHITECTURE_1.0.en.md)
- [Final protocol](docs/PROTOCOL_1.0.en.md)
- [Upgrade from 0.x](docs/UPGRADE_TO_1.0.en.md)
- [Manual acceptance](docs/MANUAL_ACCEPTANCE_1.0.en.md)
- [Release checklist](docs/RELEASE_CHECKLIST_1.0.en.md)
- [Chinese Docker operations](docs/DOCKER_DEPLOYMENT.zh-CN.md)
- [Chinese automated test evidence](docs/AUTOMATED_TESTS_1.0.zh-CN.md)
- [Threat model](docs/THREAT_MODEL.zh-CN.md)

## Developer gate

```powershell
go test ./...
go vet ./...
.\scripts\smoke-selftest.ps1

Set-Location .\plugin
npm ci
npm run typecheck
npm test
npm run build
```

Opt-in 10,000-file test:

```powershell
$env:OBSIDIAN_SYNC_SCALE_TEST='1'
go test .\internal\store -run '^TestScaleTenThousandFiles$' -count=1 -v
```

## Safe deployment defaults

- Public sync API: `127.0.0.1:8787`; this is the only Cloudflare origin.
- Local Admin API: `127.0.0.1:8788`; never expose it through Cloudflare.
- Data: `runtime-data/`; backups: `runtime-backups/`; local admin secret: `secrets/admin-token.txt`.
- Device credentials stay in Obsidian SecretStorage; the server stores hashes only.
- SQLite, Chunks, history and backups contain plaintext Vault data. Use disk encryption, strict ACLs, and an encrypted off-machine backup.
