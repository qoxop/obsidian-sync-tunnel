# Sync Tunnel 1.0 Release Checklist

## Repository gate

- Clean, reviewed diff; no runtime `/api/v2`, `X-Device-ID`, legacy capabilities or global sync-token code.
- Go tests/vet, isolated smoke test, plugin install/typecheck/tests/build, 10k-file test, Docker build/Compose, release metadata and artifacts all pass.
- Windows CI, Ubuntu race, CodeQL, Dependabot and secret scanning have no blockers.
- README, architecture, protocol, deployment, testing, threat model and acceptance docs match the implementation.

## GitHub administrator gate

- Protect main, require CI/CodeQL, disallow force pushes.
- Enable Dependabot alerts, secret scanning/push protection and Private Vulnerability Reporting.
- Allow the repository Actions token to create Releases.
- Never store Cloudflare, Admin/device credentials or real Vault data in Actions.

## RC publication

After manual approval, commit the synchronized `1.0.0-rc.1` manifest/package/versions files, push main, create an annotated `1.0.0-rc.1` tag, and push the tag. The workflow publishes only the three unofficial plugin assets, checksums, CycloneDX SBOM and a prerelease. It does not publish server binaries or container images.

Verify the release flag, asset hashes and BRAT installation. Run manual acceptance using downloaded plugin assets and a server built locally from the Dockerfile at the same immutable Tag.

## Stable gate

Pass the full manual checklist and seven-day RC soak; resolve high-severity security/dependency findings; complete off-machine restore; test clean installation; accurately disclose platform coverage and known limits. Then publish a new immutable `1.0.0` tag. Never move an existing tag or assemble mismatched assets manually.
