# Upgrade from Any 0.x Version to 1.0

This is intentionally not an in-place migration. 0.x protocols, global tokens and plugin sync state are unsupported. Upgrade the server and plugin together, use fresh 1.0 server data, pair each device again, and approve a new first-sync preview.

## Safe procedure

1. Let every 0.x client sync twice; confirm the second result is zero, then disable automatic sync.
2. Make a full independent backup of every local Vault, including `.obsidian`.
3. Preserve the old `.env`, server data and token as read-only rollback material.
4. Stop the old container.
5. Run `docker-init.ps1 -ForceConfig` with new 1.0 data, backup and Admin Token paths.
6. Start 1.0, create logical Vaults, and generate a different one-time pairing code for every device.
7. Start with a backed-up authoritative test copy and choose safe merge after reviewing the preview.
8. Join remaining devices one at a time, preferably from empty Vaults, and require a second zero-change sync before enabling automatic sync.

Never run 0.x and 1.0 behind the same hostname or against the same SQLite/Chunk directory. The Admin Token never goes into Obsidian; Cloudflare Access secrets remain in SecretStorage.

Rollback means stopping every 1.0 client, preserving the failed 1.0 test set, restoring the complete old server data plus complete client backup, and restoring the old `.env`/image. Do not mix revisions or Chunks between versions. Export any 1.0-only notes as ordinary files before rollback and import them manually afterward.
