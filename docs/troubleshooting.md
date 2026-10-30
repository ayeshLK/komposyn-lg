# Troubleshooting

Start with the data directory and command output. Komposyn keeps diagnostics
local and sanitizes them before displaying or using them in an issue draft.
Do not paste provider transcripts, prompts, responses, credentials, or raw
diagnostics into an issue.

For persisted detailed evidence, use `komposyn-lg diagnostics list` and
`komposyn-lg diagnostics show <problem-id>`. Review is separate:
`komposyn-lg diagnostics review <problem-id> --state reviewed|skipped --expected-revision <revision>` records
only review state and does not resolve a condition. List pagination uses the
returned `next_token` as `--continuation`. Detailed problem IDs are distinct
from legacy diagnostic fingerprints; older evidence may remain unavailable.
Opening these commands' store can perform existing migrations and catalog
synchronization. See [diagnostic inspection](diagnostic-inspection-contract.md)
and [backup recovery](backups.md) before inspecting or restoring older data.

The CLI also keeps a separate bounded local event log. See
[local troubleshooting logs](local-logs.md) for its location, privacy limits,
and inspection commands. Log initialization or write failure does not change
command results; a detached collector records a safe pending diagnostic if its
log sink becomes unavailable.

## The CLI cannot be found

Confirm the installer directory is on `PATH`, or invoke the binary by its full
path. A successful installation places `komposyn-lg` in `~/.local/bin` by
default on Unix-like systems. On Windows, use the installed `.exe` path.

```sh
komposyn-lg version
```

## The application opened an unexpected data directory

Run `komposyn-lg settings` to see the absolute directory used by the CLI. Stored
data defaults to `$HOME/.komposyn/token-logger/data`; an existing legacy
directory (`$XDG_DATA_HOME/token-logger`, or `$HOME/.local/share/token-logger`)
is reused only when it contains Komposyn state and the new location does not.
If both locations contain state, the new location wins and a warning explains
how to select the legacy location. No automatic merge or copy occurs. Set
`TOKEN_LOGGER_DATA_DIR` to select a directory explicitly. If the home directory
cannot be resolved, set this variable; Komposyn does not use the current
working directory as a fallback.

## No events are imported

Check that the source path exists, has readable JSON/JSONL files, and belongs
to a supported adapter. Use an explicit path to remove default-root ambiguity:

```sh
komposyn-lg import /path/to/source --data-dir /path/to/data
komposyn-lg usage --data-dir /path/to/data --format json
```

The collector skips missing built-in default roots. A missing explicit `watch`
root or environment-configured root is required and therefore reports
`health=degraded`, `discovery_failures`, and `failed_source`; collection keeps
polling so creation or permission recovery can be observed. `sources=0` with
healthy status means all configured roots were optional/missing or existing
directories were empty. It never edits source files. Check configured roots
and file permissions before changing settings.

## A repeated import reports duplicates

This is expected when records from a changed source have already been
committed. An exactly unchanged, safely reusable snapshot reports
`unchanged_snapshot` without resubmitting its historical events. Stable
ingestion keys remain the accounting backstop. A changed or replaced source
may receive a new source generation so reused offsets do not collide with older
events.

## Interactive totals look stale

Read the compact `Collector:` notice at the next command prompt, then run
`/status` for details. Inspect `state`, `last_completed`, `sources`,
`discovery_failures`, `pending`, and the typed failure counts. Counters accumulate
for the collector run, so recorded failures may remain after recovery.
`/status` also shows the last successful report query's exact bounds and
`queried_at`; this is the SQLite query time, not proof that provider sources
were just scanned. A running collector without a completed scan is still
waiting for its first result.
`/refresh` redraws committed database data only. Use foreground `watch` when an
explicit source scan is needed, and ensure it uses the same data directory.

## A project shows zero usage or an `Unattributed` name

`/projects` and `projects list` show all known projects. Their usage totals,
usage-derived session counts, and last-active values use the selected report
range. A zero can mean activity is outside that range; it does not by itself
show lifetime inactivity, missing tokens, or an import failure. `WARNINGS`
counts reconciliation warnings, not all collector/parser outcomes. Stopped
collection also means no new source evidence is being collected until a later
scan; it does not erase persisted activity. Keep legitimate metadata-only
sessions even when no accepted usage event represents them.

From the REPL, inspect a row in the currently displayed project list with
`/projects inspect 1`, or use its full stable ID with
`/projects inspect <project-id>`. The row form never reloads the inventory and
uses the range snapshot that produced the visible list. Non-interactively,
inspect using:

```sh
komposyn-lg projects inspect <project-id> --from 2020-01-01 --to 2027-01-01
komposyn-lg projects inspect <project-id> --limit 10 --offset 20 --format json
```

Selected-range activity is shown separately from all currently persisted
historical activity. Stored sessions are distinct from sessions represented by
usage. Evidence pages include location/alias/warning/source digests, safe
provenance labels, page totals, and `next_offset`; repeat the same project,
range, and data directory with that offset to fetch another page. Pages are
separate snapshots and concurrent imports may change later offsets. The
recovery classification is advisory. Unknown historical name origin remains
unavailable: a display label, adapter, or project location does not prove how
an old name was chosen. A provisional/fallback label may remain after upgrade;
upgrading does not promise that every historical label is repaired.

Inspection does not scan provider files, start a collector, change the range,
repair identity, or use the network. Opening the store can still apply its
existing migrations and synchronize its embedded pricing catalog. Digests and
exact activity values may identify local data; inspection JSON is not
automatically safe to publish. The current development source requires
KompoSyn core v0.10.0 or newer; this is the dependency pin and minimum core
floor for the next CLI release. The latest published CLI v0.10.0 uses core
v0.9.0. A local candidate override is not release validation.

## Background collector is stopped or stale

Run `komposyn-lg watch status --format json` with the same data directory used
at start. `stale` means an active lease stopped heartbeating; no process is
signalled because the stored PID may have been reused. Review the sanitized
error and registered sources, then run `watch start` to recover the lease
transactionally. `watch stop` waits for the current atomic import. If it times
out, retry later; do not kill the recorded PID solely from status output.

If the REPL says a background collector is active, it is intentionally reading
persisted status instead of starting a duplicate embedded collector. REPL exit
does not stop the independently started process.

## Import reports pending or incomplete accounting

`deferred_incomplete_tail=1` means the source ended with a partial JSON record,
usually because the provider was still writing. This is healthy and retryable:
rerun the import after the source grows, or let `watch` retry it. Do not edit the
provider-owned file.

`unsupported_usage`, `malformed_record`, or `failed_source` means accounting
may be incomplete and causes explicit import to exit with status 3. Unsupported
usage indicates a usage-bearing provider shape that this version deliberately
does not normalize; upgrade before retrying. Malformed records have invalid
JSON or required attribution such as model or timestamp. Failed sources indicate
an access, detection, or persistence failure. Use `--format json` to inspect
typed counts without exposing source paths, and review the locally sanitized
diagnostics before reporting an issue.

Claude Code may include optional `fallback_credit` billing metadata alongside
its top-level token counters. Current versions recognize that metadata without
storing it, changing token totals, or inferring a discount. If an older
KompoSyn version reported those otherwise valid records as `unsupported_usage`,
upgrade normally and allow the same source to complete another scan. The
non-reusable cursor recorded for the incomplete import causes the unchanged
transcript to be parsed again; already imported events remain duplicates and
the skipped events are recovered automatically. Do not edit provider files,
delete the database, reset cursors, or touch file timestamps. A different
unknown usage field can still keep the source incomplete and should be reduced
to a sanitized record shape before it is reported.

If a Codex import reports `unsupported_usage` around a `token_usage_record`,
inspect the sanitized outcome rather than manually adding the cumulative
fields. KompoSyn imports only the per-response `usage`; turn- and
thread-cumulative values are validation metadata. A source that contains both
new response records and legacy `token_count` snapshots will show those
mirrored snapshots as ignored non-usage records. This is expected and prevents
double-counting. Unsupported results can also indicate a cumulative rollback,
conflicting cache-write spellings, unknown usage fields, or ambiguous
accounting; upgrade the importer after the provider format is understood.

The importer intentionally does not require a Codex client-version marker.
Support is selected from the record shape and validated against the pinned
protocol provenance documented in the [usage guide](usage.md). This keeps
forward-compatible records visible as typed unsupported or malformed outcomes
instead of silently assigning behavior based on an unreliable version guess.

## Reports show unexpected hourly buckets

Hourly reports use the selected reporting timezone and default to UTC. Pass
`--timezone <IANA zone>` for a different location; fractional offsets and
repeated daylight-saving hours are then represented in that selected zone.
Confirm the effective timezone in the report before treating a bucket
difference as data loss.

## Upgrade or migration fails

Do not delete or edit the database. Restore a backup if necessary and confirm
the installed binary is compatible with the database schema. Komposyn applies
numbered migrations on open and explicitly rejects a database newer than the
binary supports. See [database migrations](migrations.md) and
[upgrades](upgrades.md).

## Installer verification fails

Ensure `cosign` is installed and that the selected release directory contains
the archive, `SHA256SUMS`, and matching `.sig` and `.pem` files. Do not bypass
checksum or certificate verification. Retry with a fresh release directory and
confirm the release version and platform architecture match.

## Reporting an issue

Include the Komposyn version, operating system, command shape, sanitized error,
and reproducible steps. Redact paths or project names when they reveal private
information. Review any generated issue draft before sending it; external
submission is never required for local operation.
