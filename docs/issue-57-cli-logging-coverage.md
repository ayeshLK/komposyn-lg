# Issue #57 CLI logging coverage

This matrix records baseline increment A on current CLI master based at
`2365e07062ceb3d1b688fd15d6cf588f28da1227` (after merged PR #60), with the
published core v0.9.0 dependency. The CLI owns the bounded local
JSONL sink and operation context. Core v0.9.0 does not provide the optional
typed operational event interface proposed by core #163, so this increment
uses controlled generic classifications where the cause is unavailable.
The implementation and resource-harness revision is
`1d04d49` (`feat(logging): instrument CLI operational paths`). At measurement
time, published CLI v0.11.0 pointed to `304804f8c13d6f1d81fd193ec4f518ba44110d6b`;
the measured current-master tree includes the merged #56 logging work after
that tag. The PR does not change the CLI version.

| Operation | Existing or added evidence | Severity | Regression coverage |
| --- | --- | --- | --- |
| Data-directory resolution | `data_directory.resolved` with one of `explicit`, `environment`, `current_default`, `legacy_default`; `command.failed` / `data_directory` on failure. No path is logged. | Info / warning | `datadir_test.go`; `TestInteractiveDatabaseOpenFailureIsLoggedBeforeStoreExists` |
| Settings load, update, onboarding persistence | `command.failed` / `settings_load`, `settings_update`, `onboarding_save`; `settings.load_failed` records a non-fatal read fallback; `settings.updated` records successful persistence without preference values. | Info / warning | Startup and settings persistence logger regressions; existing settings concurrency and onboarding tests |
| Database opening | Every production `tokenlogger.Open` call routes through `openStoreWithLog`, including REPL, report/resource commands, watch commands, issue report, and harness initialization. `database_open` is emitted before a Store exists. | Warning | `TestInteractiveDatabaseOpenFailureIsLoggedBeforeStoreExists`; protocol initialization regressions |
| Report/resource queries and rendering | `command.failed` / `report_query` or `report_render`; includes noninteractive commands, REPL report navigation/refresh/pricing/inspection, and diagnostic report creation. | Warning | `TestReportQueryAndRenderFailuresUseDistinctCodes`; `TestInteractiveStatusTracksLastSuccessfulReportOnly`; `TestInteractiveReportWriterFailureIsLoggedSafely`; existing JSON/CSV/protocol output tests |
| Detached launch and readiness | `command.failed` / `process_launch`; `collector.failed` / `collector_readiness`. The parent records launch/readiness independently of the worker log. | Warning | `TestDetachedLaunchAndReadinessFailuresAreLogged`; detached-worker process tests |
| Embedded collector | `collector.started`, `collector.stopped`, and controlled `collector.failed` codes for acquire, readiness, heartbeat, poll, storage, and release operations. | Info / warning | Existing embedded lifecycle, lease, poll, stop, and shutdown tests |
| Foreground collector | Same lifecycle and failure classes as embedded, with expected active-lease contention excluded from collector failure logging. | Info / warning | Foreground signal/process lifecycle tests and lease contention tests |
| Detached collector | The child initializes its own logger before opening SQLite; failures are recorded without a terminal. Parent and worker records have separate process/run IDs. | Info / warning | Detached worker and sink-unavailable diagnostic tests; process launch/readiness regression |
| Lease acquisition, heartbeat, cooperative stop, release | `collector_acquire`, `collector_heartbeat`, `collector_stop`, and `collector_release`. Poll cancellation is expected when its actual parent context or the poll helper's cooperative-stop result explains it. A bare deadline remains a failure. | Info for normal stop; warning for failed operations | `TestPollWithLeaseHeartbeatHonorsStopRequest`; `TestUnexpectedDeadlineIsNotSuppressedAsCancellation`; existing cooperative-stop, heartbeat, release and signal tests |
| Poll failures and health changes | `collector.failed` / `collector_poll` once per continuous failure episode; `collector.health_changed` only on a controlled running/degraded/stale transition. Repeated unchanged warnings are summarized. | Warning on failure/degraded; info on running | Watch/embedded collector tests and `TestCollectorSummarySeparatesLatestAndCumulativeScopes` |
| Poll/run outcome summary | `collector.summary` reports fixed numeric counters with `latest_poll_*` and `run_cumulative_*` prefixes, plus health and measured duration when available. At most one summary per minute per logger and an initial summary after the first completed poll. | Info when running; warning when degraded/stale | `TestCollectorSummarySeparatesLatestAndCumulativeScopes`; `TestCollectorSummaryEncodingIsBoundedAndOmitsUnknownDuration` |

`malformed_record` and `failed_source` are accumulated observations. The
summary does not establish distinct affected records, active failures, lost
usage, or parser provenance. Full provider-specific classification, source
references, retry evidence, and reliable pre-`Store.Open` core causes require
the optional public core observer and typed errors in core #163.

## Event and code catalogue (format version 1)

The following event names and code strings are the complete CLI allowlists.
Existing names and meanings remain stable; the additions are backward
compatible. Logs keep `format_version: 1`.

The common envelope remains the existing format-version-1 set: `time`,
`level`, `msg`, `format_version`, `cli_version`, `core_version`, `pid`,
`run_id`, `command`, and `metadata_truncated`, with optional `code`,
`exit_code`, and `count`. The only new serialized keys in this increment are
the bounded collector-summary fields listed below; there are no arbitrary
attribute or error-string fields.

| Event | Meaning |
| --- | --- |
| `process.started`, `process.finished` | Process lifecycle; finished records include the executable exit code. |
| `command.failed` | Controlled operation or generic command failure; optional `code` identifies the operation class. |
| `settings.updated`, `settings.load_failed` | Settings persistence succeeded or a non-fatal settings read fell back to defaults; no preference values are emitted. |
| `data_directory.resolved` | Resolution succeeded; `code` is an allowlisted origin, never a path. |
| `collector.started`, `collector.stopped`, `collector.failed` | Collector lifecycle or controlled operational failure. |
| `collector.health_changed` | Controlled running, degraded, or stale state transition. |
| `collector.summary` | Bounded numeric poll/run counters; see scope rules below. |
| `logging.unavailable`, `logging.events_suppressed`, `logging.events_dropped` | Best-effort sink, repeat-suppression, or bounded-queue evidence. |

Additive event values for the #58 reader are:

| `msg` | `level` | `code` values |
| --- | --- | --- |
| `data_directory.resolved` | `INFO` | `explicit`, `environment`, `current_default`, `legacy_default` |
| `settings.updated` | `INFO` | omitted |
| `settings.load_failed` | `WARN` | `settings_load` |
| `collector.health_changed` | `INFO` when running, otherwise `WARN` | `collector_health_running`, `collector_health_degraded`, `collector_health_stale` |
| `collector.summary` | `INFO` when running, otherwise `WARN` | same health codes as above |

`collector.summary` adds `poll_duration_ms` when measured, plus the following
nonnegative, bounded integer keys under both `latest_poll_` and
`run_cumulative_`: `imported`, `duplicates`, `ignored_non_usage`,
`unchanged_snapshot`, `deferred_incomplete_tail`, `unsupported_usage`,
`malformed_record`, and `failed_source`. Unknown duration is omitted. The
largest counter is capped at 2^53−1 for exact JSON integer representation.
No severity values are added; current records use only `INFO` and `WARN`.
The embedded REPL collector does not measure the callback's poll duration, so
it omits `poll_duration_ms` rather than reporting a fabricated zero.

Controlled failure codes are `database_open`, `settings_load`,
`settings_update`, `onboarding_save`, `data_directory`, `report_query`,
`report_render`, `process_launch`, `collector_acquire`,
`collector_readiness`, `collector_heartbeat`, `collector_poll`,
`collector_store`, `collector_stop`, `collector_release`, `import_discovery`,
`import_source`, `protocol_record`, and `log_write`. Health codes are
`collector_health_running`, `collector_health_degraded`, and
`collector_health_stale`. Data-directory origin codes are `explicit`,
`environment`, `current_default`, and `legacy_default`.

Collector summaries contain the fixed outcomes `imported`, `duplicates`,
`ignored_non_usage`, `unchanged_snapshot`, `deferred_incomplete_tail`,
`unsupported_usage`, `malformed_record`, and `failed_source`. Prefixes identify
whether each value belongs to the most recent completed poll or the current
collector run. Counts are nonnegative and capped at the largest exactly
representable JSON integer; known `poll_duration_ms` values are capped at 24 hours. Unknown
durations are omitted. No arbitrary strings, maps, paths, errors, or core event
fields can enter through the application logger interface.

Core #163 is still open and is not present in a published core tag. Increment
B remains outstanding. When a reviewed core release is available, the adapter
must preserve this catalogue, add only validated compatible fields (keeping
version 1 for additive changes), document best-effort correlation/delivery,
and prevent duplicate reporting of the same core operation.

Increment B requires the public optional observer before `Store.Open` schema
preflight and through the supported collector observer path, plus typed core
errors where released APIs expose them. The CLI will add only its process/run
and version context at the serialization boundary; it will validate the core
event fields against an allowlist and correlate duplicate core/CLI reports.
The observer remains best effort and independent of ingestion commits. No
permanent module replacement may be used to bypass the required reviewed core
release.

## Production logger resource comparison

Reproduce the Linux comparison from the repository root with:

```sh
ISSUE56_RESOURCE_EVIDENCE=1 \
ISSUE56_RESOURCE_OUTPUT=/tmp/komposyn-lg-issue57-resource.json \
go test ./plugins/cli/token-logger \
  -run '^TestIssue56ProductionLoggerResourceComparison$' -count=1 -v -timeout=15m
```

This invokes the existing serialized production-collector harness with 96
synthetic Codex fixture files. It compares logging enabled (the production
`eventLogger`) and disabled (the constrained discard logger), in alternating
order across three repetitions, for ordinary unchanged collection and
repeated required-source failures. Each worker uses a 100 ms poll interval,
3 seconds of warmup, and a 30 second measurement window; collector and logger
shutdown are measured separately. No provider history or user data directory
is used. Raw JSON is mode 0600 under `/tmp` and should not be committed.

The harness measures whole worker-process CPU time, start/end and 100 ms
sampled RSS, process HWM, Go total allocation delta and ending heap, managed
log bytes, log-event count, suppression/drop counters, collector exit, and
process I/O counter deltas. `/proc/self/io` read/write values are process
counter deltas, not physical-device traffic. Go allocation is not retained
heap; sampled RSS is not HWM. Fixture generation and the worker harness occur
outside each worker's measurement window. These Linux observations do not
establish macOS or Windows cost.

Run provenance: CLI source base `2365e07062ceb3d1b688fd15d6cf588f28da1227`
with the issue #57 worktree changes; `go.mod` pins published core v0.9.0; Go
1.26.9;
linux/amd64, CGO enabled, default `go test` build flags, empty `GOFLAGS`,
`GOTOOLCHAIN=auto`; Intel Core i7-10510U (4 cores / 8 threads), 15 GiB RAM,
Ubuntu 24.04 kernel `7.0.0-38-generic`, ext4 `/tmp`. The host was shared;
CPU scaling was dynamic and no fixed performance profile was established.
The test binary reported CLI/core `0.0.0-dev` because the default test build
does not apply release linker metadata; module resolution was pinned to the
published core v0.9.0 module. Harness file SHA-256:
`7bad1f458e9d087146f336f87441035ee9c4ace1ce4a0df723b4651506d204e1`. Raw
result SHA-256: `a44bf96abb04dccb28365f1b4c4ce8560581d94489beb7eb8f2564b98b789c37`.

### Results

The opt-in command passed on 2026-10-26. All 12 workers exited 0. Each mode
had three 30 second measured runs; fixture generation and warmup were outside
the counter window. Values below are observed min–max ranges, not estimates of
population performance.

| Synthetic workload / metric | Logging off | Logging on |
| --- | ---: | ---: |
| Ordinary: process CPU time | 12.92–13.27 s | 13.02–13.13 s |
| Ordinary: RSS at measurement end | 22.9–23.1 MiB | 22.9–23.2 MiB |
| Ordinary: 100 ms sampled RSS peak | 23.1–23.2 MiB | 23.2–23.4 MiB |
| Ordinary: process HWM at end | 22.9–23.1 MiB | 23.1–23.3 MiB |
| Ordinary: Go allocation delta | 1.74–1.77 GB | 1.76–1.77 GB |
| Ordinary: end Go heap allocation | 1.05–2.62 MiB | 0.76–2.11 MiB |
| Ordinary: process I/O read/write deltas | 0 / 5.69–5.78 MB | 0 / 5.75–5.78 MB |
| Repeated missing-required-source: process CPU time | 3.65–3.89 s | 3.64–3.85 s |
| Repeated failure: RSS at measurement end | 21.0–21.2 MiB | 20.8–21.3 MiB |
| Repeated failure: 100 ms sampled RSS peak | 22.2–22.5 MiB | 21.8–22.3 MiB |
| Repeated failure: process HWM at end | 21.6–21.9 MiB | 21.3–22.0 MiB |
| Repeated failure: Go allocation delta | 80.7–89.9 MiB | 77.8–89.8 MiB |
| Repeated failure: end Go heap allocation | 2.56–3.92 MiB | 2.75–3.72 MiB |
| Repeated failure: process I/O read/write deltas | 0 / 114.7–129.1 MB | 0 / 110.3–129.2 MB |

For repetitions 1/2/3, ordinary CPU seconds were off `13.27, 12.96, 12.92`
and on `13.13, 13.09, 13.02`; repeated-failure CPU seconds were off
`3.65, 3.89, 3.83` and on `3.78, 3.85, 3.64`. The ranges overlap and do not
show a general CPU improvement. End RSS and Go heap ranges also overlap. The
ordinary sampled RSS peak was slightly higher in the logging runs; this small
three-run difference is inconclusive. RSS is not retained heap, and total Go
allocation is not live memory.

Logging emitted exactly five records / 1,959 bytes in each ordinary 30 second
run and six records / 2,231–2,233 bytes in each repeated-failure run. The
failure workload was observed as failed/degraded in all six workers. It
produced zero suppressed or dropped events: one failure episode and its
initial bounded summary were sufficient, so repeat suppression was not
needed in this 30 second window. Logging was not the source of the workload's
process write counter (about 110–129 MB); managed log volume was about 2.2 KB.
The ordinary workload imported 192 fixture records in each run and observed
15,456–15,648 unchanged-source outcomes. The failure workload imported none.
Each worker completed cooperative collector shutdown; measured collector
shutdown ranged 2.1–2.8 ms ordinary with logging and 2.6–5.9 ms without, and
3.0–159.7 ms repeated-failure without logging and 38.1–103.6 ms with logging.
Logger flush took 0.011–0.025 ms ordinary and 0.007–0.016 ms on failure runs.
The failure shutdown ranges are noisy and do not support a latency claim.

The harness does not record per-poll latency, so no latency comparison is
available. It samples worker RSS at 100 ms; short peaks may be missed. Process
HWM is a lifetime high-water mark, reported at measurement start/end rather
than as a measurement-window delta. I/O is the worker process's Linux counter,
not physical disk traffic. The host was shared and dynamically clocked. These
small samples are evidence about this synthetic Linux workload only; they do
not establish an overhead bound for larger workloads or other platforms.
Raw samples remain at `/tmp/komposyn-lg-issue57-resource.json` on the
measurement host and are not part of the repository.
