# Obsidian Sync Tunnel

English | [简体中文](README.md)

A personal, self-hosted Obsidian sync service. The server runs on your own Windows computer, while Cloudflare Tunnel connects your other computers and mobile devices.

> The current stable release is 1.0. Sync is not backup; always keep an independent copy of every real Vault.

## What it does

- Syncs notes, images, attachments, Canvas files, themes, CSS, and plugins.
- Supports Windows, macOS, Android, and iOS.
- Handles multiple Vaults and devices, resumable transfers, conflicts, and history.
- Provides a local Web console for Vaults, devices, connectivity diagnostics, logs, backups, and maintenance.
- Persists all server data on the Windows host.

## Install the server

Install Docker Desktop, Git, and PowerShell on a Windows 10/11 computer. Download this repository, then run:

```powershell
Set-ExecutionPolicy -Scope Process Bypass -Force
.\scripts\setup.ps1
```

The script builds and starts the service, then opens <http://127.0.0.1:8788/admin/>. Run the same script after checking out a newer release. Normal administration does not require command-line tools.

## Configure Cloudflare Tunnel

Create a Named Tunnel in Cloudflare Zero Trust, install its Windows connector, and add a Public Hostname whose origin is `http://127.0.0.1:8787`.

Confirm that `https://sync.example.com/healthz` returns `status: ok`. Never expose port `8788`. A stable hostname requires a domain connected to Cloudflare; Quick Tunnels are only for temporary testing.

If the public URL returns `Error 1033`, especially while Clash Verge TUN is enabled, see [Troubleshooting](docs/TROUBLESHOOTING.en.md#cloudflare-error-1033).

## Create a Vault and pair a device

Open the local admin page and select **Vaults & devices**. Create a Vault, select **Pair**, and copy the one-time code. Generate a separate pairing code for every device.

The Vault ID identifies a server-side sync space, not a local folder.

## Install the Obsidian plugin

Install and enable BRAT, choose **Add Beta plugin**, enter `https://github.com/qoxop/obsidian-sync-tunnel`, and select **Latest version**. Confirm that BRAT shows `1.0.0`.

Enable **Sync Tunnel**, then use its setup wizard to enter the Cloudflare URL, Vault ID, device name, and pairing code. Keep the **Recommended** profile and **Safe merge** for the first sync. Run sync a second time and confirm every change counter is zero before enabling automatic sync.

## Daily administration

Use <http://127.0.0.1:8788/admin/> to manage Vaults and devices, check the local service and Cloudflare path, inspect logs, run data checks, create and verify backups, and execute two-phase garbage collection.

Only disaster recovery requires stopping the service. See [Docker deployment and recovery](docs/DOCKER_DEPLOYMENT.zh-CN.md).

## Important notes

- Server data and backups are not end-to-end encrypted. Use disk encryption and an off-device encrypted backup.
- The Full profile may copy secrets stored by other plugins; Recommended is the normal choice.
- Never route the local admin port through Cloudflare or bind it to a LAN address.
- Keep an independent copy of every real Vault.

Technical details are in [ARCHITECTURE.md](docs/ARCHITECTURE.md), [PROTOCOL_1.0.en.md](docs/PROTOCOL_1.0.en.md), [TESTING.md](docs/TESTING.md), and [Troubleshooting](docs/TROUBLESHOOTING.en.md).

## License

[MIT](LICENSE)
