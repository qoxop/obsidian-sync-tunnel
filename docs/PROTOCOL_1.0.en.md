# Sync Tunnel 1.0 Final Protocol

The only base path is `/api/v1`, with `protocol.version = 1`. Draft `/api/v2` routes, legacy capability names, global sync tokens, and `X-Device-ID` are removed. A client must stop before writing if the version differs or a required capability is absent.

## Authentication

- Admin: loopback `8788`, tokenless by default with Host/Origin checks; optional local bearer token mode; never tunneled.
- Pairing: `POST /api/v1/pair` with a short-lived one-time code; no device Bearer.
- Device: the pair response returns a server-assigned Device ID and high-entropy credential. Subsequent calls use that credential as Bearer; identity is derived from it.
- Optional Cloudflare Access uses `CF-Access-Client-Id` and `CF-Access-Client-Secret` in addition to the device credential.
- Default scopes: `sync:read,sync:write,history:read,restore:write`.

Required plugin capabilities are `snapshot`, `idempotent-operations`, `chunk-transfer`, `rename`, `batch-delete`, and `device-ack`. Additional final capabilities include `whole-file`, `history`, `restore`, and `scoped-credentials`.

## Public API

| Endpoint | Purpose |
|---|---|
| `POST /api/v1/pair` | Pair one device |
| `GET /api/v1/server-info` | Version, capabilities, DB schema and limits |
| `GET /api/v1/vaults/{vault}` / `status` | Vault metadata/current revision |
| `GET .../snapshot` / `changes` | Stable path pages/revision stream |
| `GET .../operations/{uuid}` | Recover an idempotent result |
| `PUT/DELETE .../files/content` | Whole-file mutation |
| `POST .../chunks/missing`, `PUT/GET .../chunks/{hash}` | Chunk resume |
| `POST .../files/commit`, `GET .../manifests/{hash}` | Manifest commit/download |
| `GET .../blobs/{hash}` | Whole-content download |
| `POST .../rename`, `POST .../batch/delete` | Atomic multi-path operations |
| `POST .../ack` | Persist device watermark |
| `GET .../history`, `POST .../restore` | Version browsing/restore-as-new-revision |
| `POST .../credential/rotate` | Rotate and revoke the old credential |

Mutations carry `X-Operation-ID` (UUID), `X-Base-Revision`, `X-Modified-At`, and for content `X-Content-SHA256`. A reused UUID with different metadata is rejected.

## Admin API

`/admin/v1` powers the local Web console for Vaults, pairing codes, device status, audit, runtime logs, statistics, doctor, restricted public connectivity diagnostics, online backups, and two-phase GC. Connectivity probes accept only public HTTPS hostnames and reject redirects, private/Fake-IP targets, and DNS rebinding. Disaster recovery remains the only offline script operation.

## Errors and retries

Errors use `{ "error": { "code": "...", "message": "..." }, "current": {} }`. Do not retry 400/401/403/409/413 without changing state; 409 enters conflict handling. Honor `Retry-After` for 429. Retry network/5xx failures with bounded exponential backoff and persistent outbox/inbox state. Logs must not contain credentials, bodies, or full paths.
