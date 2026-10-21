# Issue #42 project inspection integration evidence

This development evidence is for CLI issue [#42](https://github.com/ayeshLK/komposyn-lg/issues/42), coordinated with parent issue [#41](https://github.com/ayeshLK/komposyn-lg/issues/41). The inspection API is now available in published core v0.9.0 and the CLI dependency is pinned to that release. This is not standalone release evidence: ordinary merged-master CI, native six-target validation, and signed lifecycle gates remain separate.

## Candidate and dependency status

- CLI source base: `1af4aa905368548f2df8b25a93a45e5979083471` (`master` before this branch).
- Core candidate: `e8a4288f8a2c0e6262bf7c19936d61e956e78081`.
- Core [v0.9.0](https://github.com/ayeshLK/komposyn/releases/tag/v0.9.0) is published at tag commit `60d999cfdb21ad3e886f110ff9fb0719c513ece9`. It descends from candidate `e8a4288f8a2c0e6262bf7c19936d61e956e78081`; the diff between those commits contains only `.release-please-manifest.json` and `CHANGELOG.md` updates. The inspection runtime/API matches the measured and tested candidate.
- The CLI `go.mod` and `go.sum` pin core `v0.9.0`, and the documented minimum supported core floor is also `v0.9.0`. No permanent `replace` or workspace file is used. `GOWORK=off go list -m -f '{{.Version}} {{.Dir}}' github.com/ayeshLK/komposyn` resolved `v0.9.0` from `/home/ayesh/go/pkg/mod/github.com/ayesh!l!k/komposyn@v0.9.0`.
- Core v0.9.0 adds schema migration 17 for recorded project-name origin. Upgrading the shipped CLI v0.8.1/schema 15 database applies migrations 16 and 17 and retains a verified schema-15 recovery backup. Legacy name origin remains unknown; the migration does not infer it.

## Reproducing the candidate override

Use a disposable core checkout and an external workspace. Run these commands from the CLI checkout, substituting its absolute path for `CLI_CHECKOUT`:

```sh
git clone --no-checkout https://github.com/ayeshLK/komposyn.git /tmp/komposyn-core-issue42
git -C /tmp/komposyn-core-issue42 fetch origin e8a4288f8a2c0e6262bf7c19936d61e956e78081
git -C /tmp/komposyn-core-issue42 checkout --detach e8a4288f8a2c0e6262bf7c19936d61e956e78081
git -C /tmp/komposyn-core-issue42 rev-parse HEAD

cat > /tmp/komposyn-lg-issue42.work <<'EOF'
go 1.26.0

use CLI_CHECKOUT

replace github.com/ayeshLK/komposyn => /tmp/komposyn-core-issue42
EOF
GOWORK=/tmp/komposyn-lg-issue42.work go list -m -f '{{.Path}} {{.Dir}} {{.Version}}' github.com/ayeshLK/komposyn
```

The verified candidate SHA was `e8a4288f8a2c0e6262bf7c19936d61e956e78081`, resolved from `/tmp/komposyn-core-issue42`. The external workspace checksum used during this run was `82f462ece8ca9065ca664723b5126ff829e61a79507bfc3c5db983b5474f13a1`. Do not commit the workspace or convert it into a permanent module replacement.

## Behavior and compatibility

`/projects` and `projects list` continue to enumerate all known projects in the core's existing order. Human table views now explain that usage totals, usage-derived session counts, and last-active values are range-scoped; `WARNINGS` is the reconciliation-warning count; and zero usage in the selected range does not establish lifetime inactivity or token loss. JSON/CSV values and formats remain unchanged. No active-only option was added.

`/projects inspect <row-number-or-project-id>` inspects the visible row snapshot or a full project ID without reloading the project inventory. Row selection requires the current view to be the displayed project list. Inspection uses the range snapshot that produced that list and leaves navigation, rolling bounds, and the report snapshot unchanged. The noninteractive command is:

```sh
komposyn-lg projects inspect <project-id> [--from <time>] [--to <time>] \
  [--data-dir <path>] [--limit <1-50>] [--offset <0-100000>] [--format json]
```

Human output explains selected versus stored historical activity, persisted sessions versus usage-bearing sessions, recorded or unavailable name provenance, classification evidence, location/alias/warning/source projections, and advisory recovery classification. Each evidence collection reports its own pagination state. One `--limit` and `--offset` pair is passed to all four evidence collections; to continue one collection, repeat with its `next_offset`, noting that other sections are also returned at that offset. JSON is the core's versioned `ProjectInspection` response (format version 2), with no CLI-defined wrapper. Each call is a separate database snapshot.

The inspection projection omits raw names, paths, aliases, provider IDs, pricing provenance, and source contents. Stable digests and exact metrics may still identify data, so inspection JSON is not automatically safe to publish. The query itself does not scan provider files, start collection, alter ranges, repair identities, or use the network. `Store.Open` retains its existing behavior and can create a database, apply migrations with backup, or synchronize the embedded pricing catalog. Unknown historical label origin and source-classification decisions remain unavailable when they were not persisted. Upgrading does not promise that all old labels will be repaired. No rename, merge, delete, cleanup, or identity repair was added. Protocol v1, error codes, stdout purity, consent, data paths, `/status`, `/refresh`, `plugin_version`, and `cli_version` handling are unchanged.

## Regression and sanity coverage

The CLI tests cover existing list JSON/CSV and navigation, visible-row inspection and full-ID inspection, invalid/unavailable row references, missing projects, invalid arguments and bounds, page continuation, historical-only usage, metadata-only sessions, all-zero inventory, conflicting alias evidence, privacy-marker exclusion, normal Codex and Claude imports, auxiliary-only Codex input, and late Claude cwd. Existing report and protocol-v1 tests remain in the same package suite. The candidate core suite separately covers bounded inspection, unknown evidence, privacy projection, provider imports, auxiliary-only classification, metadata-only sessions, late cwd precedence, and alias/location conflicts.

The local Linux end-to-end run used the repository's synthetic protocol-v1 demo fixture in `/tmp/komposyn-lg-v090-smoke.C9lgqa/data`. It displayed the two-project inventory, inspected `project-ledger` by full ID in human and JSON forms, confirmed format version 2, selected and historical activity, and explicit offset pagination. `watch status` remained stopped with zero sources. The database SHA-256 before and after inspection was identical: `31f50bec3ba7ceca4a1447f43b27a71663c1ca65c526a1a658f2a766e369252e`. The PTY harness passed at 48 columns, exercising project listing, displayed-row inspection, and sessions navigation. No production database or real provider history was used.

## Validation results

With `GOWORK=off` and the published module cache resolving core v0.9.0:

| Command | Result |
| --- | --- |
| `go mod verify` | Passed: all modules verified. |
| `go vet ./...` | Passed. |
| `go test ./...` | Passed for the CLI module. |
| `go test -race ./...` | Passed for the CLI module. |
| `make check` | Passed, including formatting, vet, tests, build, installer trust, release provenance, and executable-copy retry checks. The provenance fixture's expected rejection cases still reject local replacements and workspaces. |
| `python3 scripts/test-protocol-smoke.py` | Passed (2 tests). |
| `bash scripts/verify-published-upgrades.sh /tmp/komposyn-lg-pr44-schema17-dist /tmp/komposyn-lg-pr44-schema17-upgrades-rerun linux_amd64` | Passed on native Linux amd64 against signed historical CLI releases v0.6.0, v0.7.0, and v0.8.1. Verified the schema-15-to-17 upgrade, usable schema-15 backup restoration, v0.8.1 refusal without mutation, checkpoint continuation, install/upgrade, and data-preserving uninstall. |
| `bash scripts/test-copy-release-executable.sh` | Passed as part of `make check`. |
| `GOWORK=off bash scripts/test-release-provenance.sh` | Passed as part of `make check`; the release builder requires published core v0.9.0. |
| Core `go test ./plugins/library/token-logger -count=1` at the candidate checkout | Passed. |
| `git diff --check` | Passed. |

Reproduce the end-to-end and PTY checks with a fresh disposable data directory and the exact v0.9.0 module:

```sh
work="$(mktemp -d /tmp/komposyn-lg-issue42.XXXXXX)"
mkdir -m 700 "$work/data"
GOWORK=off go build -trimpath -ldflags "-X github.com/ayeshLK/komposyn/plugins/library/token-logger.Version=0.9.0 -X main.cliVersion=0.9.0" -o "$work/komposyn-lg" ./plugins/cli/token-logger
python3 docs/demo/generate-fixture.py "$work/data" | "$work/komposyn-lg" harness > "$work/harness.jsonl"
cat > "$work/data/settings.json" <<EOF
{"version":1,"onboarding_complete":true,"collector_enabled":false,"auto_start":false,"data_dir":"$work/data","poll_interval_seconds":5,"poll_interval_millis":5000,"failure_mode":"non-blocking"}
EOF
chmod 600 "$work/data/settings.json"
"$work/komposyn-lg" projects list --data-dir "$work/data" --from 2000-01-01 --to 2100-01-01 --format json
"$work/komposyn-lg" projects inspect project-ledger --data-dir "$work/data" --from 2000-01-01 --to 2100-01-01 --limit 1 --format json
DEMO_INSPECT_ROW=1 DEMO_EXPECT_VERSION='komposyn-lg 0.9.0 (core 0.9.0)' CLI_BIN="$work/komposyn-lg" TOKEN_LOGGER_DATA_DIR="$work/data" DEMO_COLS=48 DEMO_ROWS=24 python3 docs/demo/record-repl-demo.py
```

The normal release-demo defaults continue to expect the signed CLI v0.9.0/core v0.8.0 recording. Inspection JSON is core format version 2, so clients must not assume it is anonymous or safe to publish.

Native validation ran on Linux only, including the signed historical-upgrade verifier above. macOS and Windows were not available locally; any cross-compilation is not runtime evidence. Ordinary local validation against the published dependency passes, but CI on the PR, native macOS/Windows runtime evidence, and the signed six-target/historical lifecycle release gates remain outstanding. No release version was changed or published.
