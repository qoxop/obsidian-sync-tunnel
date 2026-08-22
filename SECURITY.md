# Security

Sync Tunnel is a self-hosted release candidate. Keep independent encrypted backups while validating it, and never publish credentials or Vault data in issues, logs, screenshots, or diagnostics.

## Deployment baseline

- Keep both host-published ports bound to `127.0.0.1`. The container listens on non-loopback interfaces only inside Docker so NAT can reach it; Compose must never publish either port on `0.0.0.0`.
- Route only the public sync port `8787` through Cloudflare Tunnel. Never route the local management port `8788` through Cloudflare or bind it to a LAN address.
- Add devices through short-lived, single-use pairing codes. Each paired device receives a scoped credential stored through Obsidian SecretStorage; Sync Tunnel's own `data.json` remains device-local.
- Prefer a Cloudflare Access Service Token as an additional control on the public sync hostname.
- Admin authentication is optional for the loopback-only management page. On a shared Windows account, enable token mode and keep the Admin Token in the ACL-restricted host secret file, never in an environment variable, image layer, browser storage, or Obsidian.
- Protect the Windows disk and backups with encryption. Vault contents in SQLite are plaintext.
- Review other plugins before enabling full `.obsidian` sync. Some plugins keep their own API keys in plaintext `data.json` files, which this project will faithfully copy into SQLite unless excluded.
- Test upgrades and sync behavior in disposable Vaults before using real notes.
- Do not run Obsidian Sync, Syncthing, iCloud, or another bidirectional real-time synchronizer over the same Vault at the same time.

## Reporting

Use GitHub Private Vulnerability Reporting when it is available. Otherwise open a public issue containing only a sanitized request for private contact; never attach tokens, pairing codes, Vault content, database files, private hostnames, or Cloudflare account details. Revoke any credential that may have been exposed before sharing a sanitized reproduction.
