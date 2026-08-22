# Obsidian Sync Tunnel

English | [简体中文](README.md)

A personal, self-hosted full-Vault synchronization system for Obsidian. The Go/SQLite server runs in Windows Docker Desktop with bind-mounted persistence, Cloudflare Tunnel provides HTTPS ingress, and an unofficial Obsidian plugin supports desktop and mobile clients.

Current version: `1.0.0-rc.1`. Keep an independent backup and do not use this release candidate as the only copy of a real Vault.

## Features

- One administrator, multiple logical Vaults and multiple devices.
- Notes, Canvas, images, attachments, common Obsidian settings, themes, CSS and other plugin files.
- Notes, Recommended, Full and Custom sync profiles; Recommended is the safe default.
- First-sync preview with safe merge, remote-primary and local-primary choices.
- SHA-256 content addressing, 4 MiB Chunks, missing-Chunk resume and integrity checks.
- Persistent outbox/inbox recovery after network, Obsidian or server interruption.
- Optimistic revisions, idempotent operations, conflict copies, atomic rename and batch delete.
- History/restore, per-device credentials, revocation, quotas, audit, doctor, ACK-aware GC and verified backups.

Sync Tunnel's own plugin directory and `data.json` always remain device-local. Full mode may copy secrets stored by other plugins in their `data.json`; use Recommended unless you explicitly accept that risk.

## Architecture

```mermaid
flowchart LR
    O[Obsidian plugin] -->|HTTPS + device credential| C[Cloudflare Tunnel / Access]
    C --> F[Windows cloudflared]
    F -->|127.0.0.1:8787| P[Public sync API]
    A[Windows admin scripts] -->|127.0.0.1:8788 + Admin Token| M[Admin API]
    P --> G[Go server]
    M --> G
    G --> S[(SQLite WAL + Chunk storage)]
```

Only the plugin is distributed through GitHub Releases/BRAT. Server binaries and public container images are not published; build the server locally from the same immutable Git Tag as the plugin.

The [complete architecture document](docs/ARCHITECTURE.md) and most operating documentation are currently in Simplified Chinese.

## Quick start

On the Windows server:

```powershell
git clone --branch 1.0.0-rc.1 --depth 1 https://github.com/qoxop/obsidian-sync-tunnel.git
Set-Location .\obsidian-sync-tunnel
.\scripts\docker-init.ps1
.\scripts\docker-up.ps1

Invoke-RestMethod http://127.0.0.1:8787/healthz
Invoke-RestMethod http://127.0.0.1:8788/healthz

.\scripts\admin.ps1 -CreateVault -VaultId personal-notes -DisplayName 'Personal notes'
.\scripts\admin.ps1 -CreatePairing -VaultId personal-notes
```

Configure a stable Cloudflare Public Hostname with origin `http://127.0.0.1:8787`. Never expose the Admin API on port `8788`.

On each Obsidian device:

1. Install BRAT and add `https://github.com/qoxop/obsidian-sync-tunnel` as a beta plugin.
2. Enable Sync Tunnel and open its setup wizard.
3. Enter the HTTPS Server URL, logical Vault ID, device name and a newly generated one-time pairing code.
4. Keep the Recommended profile, test the connection, review First sync preview and choose Safe merge.
5. Run Sync now twice; the second run should report zero changes.
6. Generate a different pairing code for every additional device.

The Admin Token is only for local Windows scripts. Never enter it into Obsidian.

## Operations

```powershell
.\scripts\docker-logs.ps1 -Follow
.\scripts\admin.ps1 -Doctor
.\scripts\admin.ps1 -Stats
.\scripts\docker-backup.ps1 -KeepLast 7
```

SQLite, paths, content, history, Chunks and backups are plaintext in 1.0. Use disk encryption, strict ACLs and an encrypted off-machine backup. Read the [Chinese deployment guide](docs/DOCKER_DEPLOYMENT.zh-CN.md) before restore or GC operations.

## Development

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

See [testing](docs/TESTING.md), the [1.0 protocol](docs/PROTOCOL_1.0.en.md), [manual acceptance](docs/MANUAL_ACCEPTANCE_1.0.en.md), and [security policy](SECURITY.md).

## License

[MIT](LICENSE)
