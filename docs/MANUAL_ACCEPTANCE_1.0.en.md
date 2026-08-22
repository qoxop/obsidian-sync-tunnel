# Sync Tunnel 1.0 Manual Acceptance

Use dedicated test Vaults and record date, OS, Obsidian/plugin/server versions and sanitized evidence. Stop on any failure. The [Chinese checklist](MANUAL_ACCEPTANCE_1.0.zh-CN.md) contains the exact Windows commands.

## Required matrix

- Preserve 0.x data and keep an encrypted off-machine backup of every real Vault.
- Start 1.0 RC with fresh Windows data/backup/Admin Token paths. Verify both loopback health endpoints, doctor, stats, connectivity diagnostics, Docker health and that Cloudflare routes only to 8787.
- Create one logical test Vault. Pair Windows A and B with separate one-time codes. The server must assign each Device ID and SecretStorage must hold the credentials.
- Use Recommended + safe merge, keep automatic sync disabled, and require two manual syncs with zero counts on the second.
- Exchange Markdown, Canvas, PNG, PDF, Unicode/space paths, plugin bundles, 5 MiB and 64 MiB binaries in both directions; compare SHA-256.
- Verify Recommended excludes other plugins' `data.json`, while Full requires explicit warning/choice. Sync Tunnel's own state and `.DS_Store` never propagate.
- Test concurrent offline edits and all four conflict resolutions; atomic rename; 20-item batch delete; history, old-version restore and deletion recovery.
- Interrupt network, Obsidian upload/download, server restart, Docker Desktop restart and Windows restart. Persistent queues must resume and converge.
- Rotate a device credential, revoke a device, suspend a Vault, exercise quota/file/disk limits, inspect sanitized audit/diagnostics, and review two-phase GC.
- Create, verify and restore an online backup. Recheck doctor, revision, probe hashes and client convergence; replicate the backup to encrypted off-machine storage.
- Pair a Mac as a second device and verify bidirectional convergence, interrupted transfer recovery, rename and deletion. Test at least one Android or iOS physical device for foreground/background, network switch, attachment, conflict and memory-limit behavior.

## RC pass gate

All scenarios pass; Windows A/B, Mac and one mobile device have a zero second sync; Docker/host restart and backup restore pass; at least seven days of normal RC use have no silent loss, unrecoverable conflict or corruption; every failure has an issue and disposition; all CI, race, CodeQL and dependency checks pass.

Do not promote an untested RC directly to stable `1.0.0`.
