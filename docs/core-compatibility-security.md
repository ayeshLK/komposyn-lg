# Core compatibility and coordinated security releases

The CLI is independently versioned and pins a specific core module in `go.mod`.
At the v0.8.0 standalone release:

| Compatibility value | Core version |
| --- | --- |
| Minimum supported core | `v0.7.0` |
| Currently pinned core | `v0.7.0` |

Because the supported floor and pin are the same, the normal CLI CI suite on
`master` tests that version once; a duplicate matrix job adds no coverage. The
core repository's candidate CI separately runs the public CLI suite against a
candidate core through an isolated temporary replacement. It has read-only
access, no cross-repository credentials, and does not modify either repository.

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
6. Passing CLI `go mod verify`, vet, tests, race tests, `make check`, CodeQL,
   and native lifecycle checks appropriate to the change. Core CI's candidate
   compatibility job must pass before the core change merges.

The CLI CI tests the pinned core version. At present this is also the minimum
supported version (`v0.7.0`). If the minimum and pin diverge, add explicit CLI
CI coverage for both before documenting the expanded support range. Never
commit a `replace` directive; the candidate override exists only in disposable
CI storage.

## Coordinated private security releases

The repository that owns vulnerable code owns the GitHub Security Advisory and
CVE. Core owns defects in the reusable API, storage/migrations, adapters,
collector, pricing, diagnostics, and report behavior. The standalone CLI owns
command execution, settings/consent, data-directory policy, process lifecycle,
harness transport, installers, and binary release code. If impact crosses the
boundary, the receiving repository's maintainers coordinate privately with
the owner; do not open a public issue or PR before disclosure is cleared.

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
