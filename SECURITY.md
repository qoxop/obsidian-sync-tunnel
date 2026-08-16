# Security

This project is an early self-hosted MVP. Do not expose the origin listener, commit credentials, or treat synchronization as backup.

## Deployment baseline

- Keep the host-published port bound to `127.0.0.1`. The container explicitly opts into an internal non-loopback listener so Docker NAT can reach it; Compose must never publish it on `0.0.0.0`.
- Keep the API token in the read-only Compose secret file, not in an environment variable or image layer.
- Publish it only through Cloudflare Tunnel.
- Use the server Bearer Token and preferably a Cloudflare Access Service Token as a second layer.
- Store credentials in the server's ACL-restricted token file and Obsidian SecretStorage.
- Protect the Windows disk and backups with encryption. Vault contents in SQLite are plaintext.
- Review other plugins before enabling full `.obsidian` sync. Some plugins keep their own API keys in plaintext `data.json` files, which this project will faithfully copy into SQLite unless excluded.
- Test upgrades and sync behavior in disposable Vaults before using real notes.

## Reporting

Until a public security contact is configured, do not open a public issue containing tokens, Vault content, database files, hostnames, or Cloudflare account details. Revoke any credential that may have been exposed before sharing a sanitized reproduction.
