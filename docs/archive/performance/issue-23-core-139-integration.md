# Issue #23: published checkpoint-core integration

> **Historical record.** Historical integration evidence for CLI v0.8.1 and core v0.8.0, including schema 15 to 16. Its validation claims cover the listed runs and platforms only and do not establish current release readiness. See [migrations](../../migrations.md), [backups](../../backups.md), and the maintained [release verification requirements](../../release-verification.md).


This candidate integrates the published checkpoint implementation into the
standalone CLI for [issue #23](https://github.com/ayeshLK/komposyn-lg/issues/23),
coordinated under [issue #20](https://github.com/ayeshLK/komposyn-lg/issues/20).
It pins core v0.8.0, raises the minimum supported core to v0.8.0, and validates
the schema-15-to-16 migration while preserving the standalone protocol-v1 and
CLI/core version boundary.

## Revisions and compatibility review

The verified core release is published [v0.8.0](https://github.com/ayeshLK/komposyn/releases/tag/v0.8.0),
tag commit `258dc3e3e13eee176786aa68a97ed5ee09063717`. It includes the reviewed
checkpoint implementation from core PR #145 at
`e5d396a61a6222610d3f8e43772d5861692a01a9`. Comparing those revisions shows
only `.release-please-manifest.json` and `CHANGELOG.md` changed; the runtime Go
source is identical. The release commit is the source used by the pinned Go
module checksum `h1:oWaALEa+tBCVM2YGex24FDt+tQ3LfAMHUAgO8XTL8ps=`.

The CLI source base is current `origin/master` commit
`4fe002af9b49cf47618c19d4494e4ada2bed9c63`, after merged PR #29. The latest
published standalone executable remains CLI v0.8.1 at
`6fcc75ab0543667d24fd97f21a91af61812138bc`, pinned to core v0.7.1. This branch
keeps the CLI release version untouched; the release builder continues to set
`cli_version` from its requested CLI tag and `plugin_version` from the pinned
core module.

Core v0.8.0 changes `CurrentSchemaVersion` from 15 to 16 and adds the private
`parser_checkpoints` table through migration 16. Checkpoint records hold
bounded normalized parser state, revisions, offsets, fingerprints and a state
digest; they do not hold provider source text. The implementation adds no
exported API entrypoint, CLI parser or protocol field. Core `go.mod` and its
toolchain requirements are unchanged (Go 1.26.0, toolchain Go 1.26.9), matching
the CLI module. The CLI module graph is unchanged apart from the core version
and its two checksum entries. The candidate has no permanent `replace`.

Compatibility policy sets the minimum supported core and current pin together
at v0.8.0, so CI tests that version once. CLI v0.8.0/v0.7.0 and CLI
v0.8.1/v0.7.1 remain historical mappings. Protocol v1, event
`schema_versions`, stable error behavior, stdout purity, exact JSON/CSV, data
paths, consent and provider-source privacy remain unchanged. Initialization
continues to identify the core in `plugin_version` and this executable in
`cli_version`.

## Schema migration, recovery and lifecycle evidence

The candidate release verifier now requires fresh databases to open at schema
16. Its native upgrade suite verifies the actual signed CLI v0.8.1 binary
creates schema 15, then the candidate upgrades exactly 15 → 16. The verifier
checks the retained settings/consent file, source registry and cursors, project
and session identities, event totals, diagnostics review state, collector run
history and a verified backup whose metadata is source 15/target 16. It copies
the backup to a separate data directory and checks that the old v0.8.1 binary
can read the preserved schema-15 usage.

The suite also injects a conflicting `parser_checkpoints` table into a
disposable schema-15 copy to make migration fail. It verifies the original
schema/data and integrity remain intact, the verified 15 → 16 backup remains
usable by v0.8.1, and no production data is involved. The core v0.8.0 tests
separately cover failure to create a requested backup without changing its
caller-owned destination or migrating the schema, transactional migration-16
failure with a usable backup, and refusal of newer schemas. Candidate release
checks exercise fresh-schema initialization and refusal of synthetic schema
17. The shipped v0.8.1 executable is also required to refuse the migrated
schema-16 database with the unsupported-schema error and without modifying the
database or WAL/SHM sidecars.

For checkpoint use after migration, the signed-reference native verifier
appends a synthetic Codex event to the existing schema-15 source after the
candidate migrates the database. The candidate collector bootstraps a durable
checkpoint and reaches exactly 148 tokens/3 requests. A second cumulative
usage record is appended to the same session; the collector reaches exactly
185 tokens/4 requests, and the checkpoint boundary advances to source EOF.
The [merged #139 measurement report](./issue-23-core-139-checkpoint-comparison.md)
provides the matched standalone CLI comparison and deterministic core
work-counter evidence for continuation. Published v0.8.0 has identical runtime
Go source to its measured candidate, so this phase does not repeat the full
measurement matrix. The report's stable-session synthetic workloads,
full-history hashing, fallback costs and shared-host noise remain its stated
limits; it does not establish a universal CPU/RSS improvement or fixed leak.

The Go lifecycle suite continues to cover embedded collection,
positional and registry-backed foreground modes, detached collection,
restart, dynamic registry reload, lease heartbeat and cooperative stop. The
release verifier preserves the prior signed v0.6.0 and v0.7.0 historical
upgrade coverage and adds signed CLI v0.8.1 as the schema-15 migration source.
The signed release workflow now exercises active-collector upgrades from both
v0.6.0 and v0.8.1 on all six native targets. Unsigned candidate CI is not
release-signing or publication evidence.

Consumer guidance in [migrations](../../migrations.md), [backups and recovery](../../backups.md)
and [upgrades](../../upgrades.md) explains the 15 → 16 backup, older-binary refusal,
and restoration into a separate data directory with a schema-compatible CLI.

## Validation

All local commands below ran in the dedicated integration worktree with Go
1.26.9, Linux amd64 and the published v0.8.0 module resolved from
`/home/ayesh/go/pkg/mod/github.com/ayesh!l!k/komposyn@v0.8.0`:

| Command | Result |
| --- | --- |
| `go mod verify` | Pass: all modules verified. |
| `go vet ./...` | Pass. |
| `go test ./...` | Pass: CLI package, 8.047s. |
| `go test -race ./...` | Pass: CLI package, 72.882s. |
| `make check` | Pass: format, vet, tests, build, installer trust, release provenance and executable-copy checks. |
| Core v0.8.0 migration-focused tests | Pass: `TestMigrationBackupFailureLeavesSchemaUnmigratedAndPreservesDestination`, `TestMigrationFailureLeavesOriginalAndVerifiedBackupUsable`, `TestMigrationSixteenFailurePreservesSchemaFifteenAndBackup`, and `TestOpenRefusesNewerSchemaWithoutChangingDatabaseOrWAL`. |
| `bash scripts/verify-published-upgrades.sh /tmp/komposyn-lg-v080-integration-release /tmp/komposyn-lg-v080-native-upgrades-final3 linux_amd64` | Pass on native Linux amd64 for signed historical v0.6.0/v0.7.0 and signed CLI v0.8.1 → candidate upgrades, including schema 15 → 16, backup/recovery, migration failure preservation, old-binary refusal, append bootstrap/continuation and data-preserving uninstall. |
| `bash scripts/verify-release.sh /tmp/komposyn-lg-v080-integration-release` | Pass: fresh schema 16, protocol smoke, schema-17 refusal and disposable install/uninstall. |
| `bash scripts/test-release-assets.sh` | Pass. |
| `python3 scripts/test-protocol-smoke.py` | Pass. |
| `python3 scripts/test-measure-collector.py` | Pass: 10 tests, 2 platform-specific skips. |
| `bash -n` release and upgrade scripts; `git diff --check` | Pass. |

The PR CI supplies native runtime coverage on Linux, macOS and Windows,
including both Windows architectures, and historical-upgrade artifacts. Its
results must pass before merge. Cross-compilation is not native runtime
evidence. Final delivery still requires the normal Release Please preparation,
signed six-archive build and verification, six-target signed active-upgrade
checks, publication, and post-publication asset/version verification. This PR
does not publish, sign, tag, or merge a CLI release.
