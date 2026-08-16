# Changelog

All notable changes to Sync Tunnel are documented here. The project follows Semantic Versioning once a change is published.

## Unreleased

### Added

- Product roadmap, Protocol v2 draft, threat model, and release test strategy.
- Versioned SQLite schema migration foundation.
- Authenticated `/api/v2/server-info` capability discovery endpoint.
- Stable, revision-bound Vault snapshot API with path pagination.
- Client snapshot reconciliation when synchronization filters change.
- Explicit first-sync preview with safe merge, remote-authoritative, and local-authoritative modes.
- Notes-only, recommended-safe, full-Vault, and custom synchronization profiles.
- Plugin unit tests, in-memory Vault tests, and first synchronization-engine state tests.
- Connection test for Tunnel, Access, API token, Vault ID, and server protocol compatibility.
- Restart notice when synchronized plugins, themes, snippets, or community-plugin state changes.
- Cross-platform case and Unicode path-collision detection.

### Changed

- Sync Tunnel's current and legacy plugin directories are now fully device-local and never synchronized.
- Existing 0.1 installations keep their initialized state; new installations require explicit first-sync confirmation.
- New installations default to the recommended-safe profile; existing 0.1 installations retain full-Vault behavior.

### Security

- Prevent one device from replacing another device's running Sync Tunnel bundle through Vault synchronization.
- Preserve an untracked local file as a conflict copy before a snapshot applies different remote content.
