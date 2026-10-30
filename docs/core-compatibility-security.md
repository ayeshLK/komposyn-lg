# Core compatibility and coordinated security releases

The standalone CLI and KompoSyn core are versioned independently. The CLI pins
one core module version in `go.mod`; `plugin_version` reports that core version
and `cli_version` reports the executable version. The latest published CLI is
v0.10.0, pinned to core v0.9.0 and schema 17. The current development source
pins published core v0.10.0 and sets that as the minimum core floor for the
next CLI release; it supports schema 18. This source pin does not mean a CLI
release using v0.10.0 has been published. Earlier published mappings are
retained here for upgrade compatibility:

| CLI release | Pinned core | Schema supported by that core |
| --- | --- | ---: |
| v0.8.0 | v0.7.0 | 15 |
| v0.8.1 | v0.7.1 | 15 |
| v0.8.2 and v0.9.0 | v0.8.0 | 16 |
| v0.10.0 | v0.9.0 | 17 |
| Next CLI release (not yet published) | v0.10.0 | 18 |

The standalone security support policy is latest-published-release only. As of
2026-10-22, v0.10.0 is supported; a newer published CLI release supersedes it
and older CLI releases receive no further security fixes. The `master` branch
is the development source for a future signed release, not an independently
supported binary line. See [the standalone security policy](../SECURITY.md).

Because the supported core floor and dependency pin are the same, the normal
CLI CI suite tests that version once. If they diverge, add explicit CLI CI
coverage for both before documenting the expanded support range. Core candidate
CI separately runs the public CLI suite against candidate core source through
an isolated temporary replacement. It has read-only access, no cross-repository
credentials, and does not modify either repository.

## Core dependency update review

Dependabot may propose core dependency updates. Review each update for:

1. Exported API additions, removals, and changed behavior, including clean
   external-consumer build compatibility.
2. Schema version and migration behavior: upgrade, backup, refusal, recovery,
   and data preservation. Numbered migrations are forward-only and immutable.
3. Harness protocol v1 behavior and exact JSON/CSV output, including
   `plugin_version` as core version and `cli_version` as executable version.
4. Provider source immutability, consent and data-path behavior, privacy,
   diagnostics sanitization, and security notes.
5. Dependency vulnerability results and release/version metadata.
6. Passing CLI `go mod verify`, vet, tests, race tests, `make check`, CodeQL, and
   native lifecycle checks appropriate to the change. Core CI's candidate
   compatibility job must pass before the core change merges.

Core v0.8.0 advanced SQLite schema 15 to 16 with private parser checkpoints.
Core v0.9.0 advances schema 16 to 17 to record project-name origin; existing
origins remain unknown. Core v0.10.0 advances schema 17 to 18 for bounded
persisted diagnostic details and their database-local namespace. These
forward-only migrations create a verified pre-migration backup; no automatic
down-migration is provided. Current recovery and version-compatibility instructions
are in [backups](backups.md), [migrations](migrations.md), and [upgrades](upgrades.md).
Never commit a `replace` directive; candidate overrides belong only in
disposable CI or measurement worktrees.

## Coordinated private security releases

The repository that owns vulnerable code owns the GitHub Security Advisory and
CVE. Core owns defects in the reusable API, storage/migrations, adapters,
collector, pricing, diagnostics, and report behavior. The standalone CLI owns
command execution, settings/consent, data-directory policy, process lifecycle,
harness transport, installers, and binary release code. If impact crosses the
boundary, the receiving repository's maintainers coordinate privately with the
owner; do not open a public issue or PR before disclosure is cleared.

For a core vulnerability affecting the CLI:

1. Privately prepare and validate the core fix and record affected/fixed core
   versions.
2. Privately update the CLI's pinned dependency and verify API behavior,
   migrations, protocol semantics, metadata, and security guidance.
3. Publish the fixed core source tag, then promptly publish a signed CLI
   release pinned to it. For an embargo, prepare both reviewed changes in
   private advisory branches/forks before either release is disclosed.
4. Publish coordinated advisories that map affected/fixed core and CLI
   versions, supported-version boundaries, impact, and prerequisites.
5. Include explicit upgrade steps: install the fixed standalone CLI release
   over the existing executable. Preserve the existing data directory,
   settings, consent, diagnostics, and backups; no self-updater is provided.

For a CLI-only vulnerability, the standalone repository owns the advisory and
signed release. If a core issue is reported to the CLI, forward it privately to
core maintainers and do not disclose it publicly. Neither repository should
share signing credentials or grant the other repository write access for normal
CI.
