# Issue #23: CLI measurements for core append checkpoints

This report compares the shipped standalone CLI against published core v0.7.1
and the merged, not-yet-published core checkpoint implementation from [core
PR #145](https://github.com/ayeshLK/komposyn/pull/145). The latest [issue #23
handoff](https://github.com/ayeshLK/komposyn-lg/issues/23#issuecomment-6261406629)
records that core release PR #146 was still pending publication. This measures
the checkpoint phase for [issue #23](https://github.com/ayeshLK/komposyn-lg/issues/23),
coordinated under [issue #20](https://github.com/ayeshLK/komposyn-lg/issues/20).
The comparison does not change the CLI dependency pin or establish a universal
latency or memory improvement.

## Revisions and reproduction

Both sides use identical CLI Go sources from shipped v0.8.1:
`6fcc75ab0543667d24fd97f21a91af61812138bc`.

| Side | CLI source | Core source | CLI/core version output | Binary SHA-256 |
| --- | --- | --- | --- | --- |
| Before | `6fcc75ab0543667d24fd97f21a91af61812138bc` | published v0.7.1, `4519551527b28461b250bfdbc7f9f66473a6a219` | `komposyn-lg 0.8.1 (core 0.7.1)` | `60d72000b147d2e7aa8cf08a5932d210be3d4e42efd9cd2b8c7d51a87338495a` |
| Candidate | `6fcc75ab0543667d24fd97f21a91af61812138bc` | merged #145, `e5d396a61a6222610d3f8e43772d5861692a01a9` | `komposyn-lg 0.8.1 (core 0.8.0-dev+e5d396a)` | `b9c9e1ab71edae97500b31b0eb1be0a747ffd141da7cdc7255065658c33e64e5` |

The after-side version string identifies the exact unpublished source commit;
it is not a published core version. Both builds used Go `1.26.9`, Linux
`amd64`, `CGO_ENABLED=0`, `GOWORK=off`, `-trimpath`, and `-ldflags '-s -w
-X github.com/ayeshLK/komposyn/plugins/library/token-logger.Version=<core-id>
-X main.cliVersion=0.8.1'`. `go version -m` records the same CLI module and
dependency graph on each binary; the only module replacement is the temporary
core source directory. Before build, `git rev-parse HEAD` and
`GOWORK=off go list -m -f '{{.Replace.Dir}}' github.com/ayeshLK/komposyn`
confirmed each required core SHA and resolved directory. No replacement was
committed, and the report branch retains the v0.8.1 dependency pin.

The actual module directories resolved to
`/tmp/komposyn-core-before-checkpoint` and
`/tmp/komposyn-core-after-checkpoint`. `go version -m` reported the CLI main
module at `(devel)`, the core module at `v0.7.1` with the matching local
`(devel)` replacement, and identical third-party dependency versions. The
binary build records `-compiler=gc`, `-trimpath=true`, `CGO_ENABLED=0`,
`GOARCH=amd64`, `GOOS=linux`, and `GOAMD64=v1` on both sides.

The harness is the corrected script from the exact CLI source commit above.
Its SHA-256 is
`e2a53f22d9cd21c11d538bcaa3c2eca159a3488a99de5a68e487e90b42930904`.
The pinned core work-counter benchmark script has SHA-256
`9042028ceb360d107c6c7c727170de237371e94557831f89615a6149564cac02`.

Example setup and build commands (the two CLI source trees are disposable):

```sh
git -C /path/to/komposyn worktree add --detach /tmp/core-before 4519551527b28461b250bfdbc7f9f66473a6a219
git -C /path/to/komposyn worktree add --detach /tmp/core-after e5d396a61a6222610d3f8e43772d5861692a01a9
git -C /path/to/komposyn-lg worktree add --detach /tmp/cli-before 6fcc75ab0543667d24fd97f21a91af61812138bc
git -C /path/to/komposyn-lg worktree add --detach /tmp/cli-after 6fcc75ab0543667d24fd97f21a91af61812138bc
go -C /tmp/cli-before mod edit -replace=github.com/ayeshLK/komposyn=/tmp/core-before
go -C /tmp/cli-after mod edit -replace=github.com/ayeshLK/komposyn=/tmp/core-after
git -C /path/to/komposyn-lg show 6fcc75ab0543667d24fd97f21a91af61812138bc:scripts/measure-collector.py > /tmp/measure-collector.py
echo 'e2a53f22d9cd21c11d538bcaa3c2eca159a3488a99de5a68e487e90b42930904  /tmp/measure-collector.py' | sha256sum --check
GOWORK=off go -C /tmp/cli-before list -m -f '{{.Replace.Dir}}' github.com/ayeshLK/komposyn
GOWORK=off go -C /tmp/cli-after list -m -f '{{.Replace.Dir}}' github.com/ayeshLK/komposyn
CGO_ENABLED=0 GOWORK=off go -C /tmp/cli-before build -trimpath -ldflags '-s -w -X github.com/ayeshLK/komposyn/plugins/library/token-logger.Version=0.7.1 -X main.cliVersion=0.8.1' -o /tmp/komposyn-lg-before ./plugins/cli/token-logger
CGO_ENABLED=0 GOWORK=off go -C /tmp/cli-after build -trimpath -ldflags '-s -w -X github.com/ayeshLK/komposyn/plugins/library/token-logger.Version=0.8.0-dev+e5d396a -X main.cliVersion=0.8.1' -o /tmp/komposyn-lg-after ./plugins/cli/token-logger
```

For example, this is one matched standard append run; the same pinned driver
and options were used for each side:

```sh
python3 /tmp/measure-collector.py --binary /tmp/komposyn-lg-before --mode foreground --scenario usage-append --profile claude --interval 5s --events 10000 --warmup 15s --duration 60s --append-every 5s --artifacts /tmp/measure-before
python3 /tmp/measure-collector.py --binary /tmp/komposyn-lg-after --mode foreground --scenario usage-append --profile claude --interval 5s --events 10000 --warmup 15s --duration 60s --append-every 5s --artifacts /tmp/measure-after
```

The primary matrix has 58 successful runs: three repetitions per provider and
side for unchanged, usage-append, and non-usage-append histories; plus one
before/after pair per provider for cold backfill, 100-file histories,
greater-than-64-KiB records, 1-second stress, and detached mode; and one
10-minute Claude append soak per side. Repeated pairs alternated before/after
order. Standard windows were 60 seconds at the default 5-second interval with
15 seconds warmup. Each append run started with 10,000 usage records and added
11 records at a 5-second cadence. Stress and detached cases used 10 seconds
warmup. Cold backfill used 10,000 events over four files and ended at the first
completed poll. Many-file and large-record runs used 10 seconds warmup.
Large-record fixtures used 10 records with 70,000 bytes of synthetic padding
per record. The soak used 10 seconds warmup, ran for 600 seconds, and validated
119 appends. A separate 15-second candidate diagnostic retained a synthetic
workspace long enough to inspect its checkpoint row; it is not included in the
58 resource runs. One additional matched Claude usage-append pair used a
10-second warmup; it is retained as supplemental data but excluded so the
primary three-run range uses a uniform 15-second warmup. One exploratory run
was interrupted before it wrote a sample; a retry then encountered its
existing artifact directory before collector launch. Both attempts are
excluded.

Raw manifests, `/proc` samples, command logs, the full core benchmark output,
and synthetic lifecycle fixtures remain outside the repository under
`/tmp/issue23-core139` on the measurement host. The harness removes each
temporary source/database workspace after a run unless explicitly asked to
retain it for the separate checkpoint diagnostic.

## Checkpoint eligibility and correctness evidence

The measured append fixtures are eligible for continuation: each provider
history has complete LF-terminated records, stable session/project/model
attribution, and appends usage or non-usage records to the same existing
session. They avoid late metadata changes and new scopes that would require
reconciliation. The generated histories contain synthetic content only.

The candidate CLI diagnostic created a Claude checkpoint row after its initial
import and usage appends. Its `boundary_offset` exactly matched the final
source-file length (`3,319,748` bytes), and the source cursor had the same
offset. The row stored 420 bytes of normalized state. This is direct evidence
that the standalone candidate created and advanced a durable checkpoint for a
measured-fixture shape; resource counters alone are not used to infer activation.

The exact candidate core also passed its deterministic work-counter test
`TestParserCheckpointContinuationSkipsHistory` for both providers. It asserts
that a recreated collector reuses the database checkpoint, imports one suffix
event, reports the 12 historical duplicate receipts required for replay
equivalence, decodes one parser record, makes zero historical insertion
attempts, and makes one suffix insertion attempt. The five-repeat core
benchmark below independently reports the same work pattern at 10,000 events.
An unchanged snapshot decodes zero parser records and makes zero event insert
attempts on both sides.

Missing, corrupt, oversized, incompatible, or prefix-mismatched checkpoints,
unsafe attribution changes, truncation/rewrite, and state-limit cases use
verified full replay. The candidate core's `TestParserCheckpointFallbackCases`
covers fallback selection for both providers. In the five-repeat benchmark,
corrupt-checkpoint and state-limit cases parse 10,002 records, attempt 10,000
historical inserts and 10,001 total inserts, matching the full-replay oracle's
accounting. These are fallback costs, not successful continuation results.

For a CLI-level process-restart check, each side imported a 100-event generated
history into an isolated data directory, then a separate CLI process imported
one appended event using the same database. Both before and after reported
`imported_events=1`, `duplicate_events=100`, and exact 101-request totals. On
the candidate, the durable checkpoint table contained one row after restart
reuse.

Pending-tail completion was checked through separate CLI processes for both
providers and both core revisions. The first import contained two complete
records and one valid usage record without a final LF; it imported three
events. The second import appended the LF and a fourth usage record; it
reported one new event and three duplicates, including the previously imported
tail and the two complete prefix events. All four comparisons ended at exactly
four requests and the expected token totals, with `complete=true` and
`pending=false`.

For the schema upgrade check, the before CLI created a schema-15 database with
20 events and a source cursor. Opening it with the candidate migrated it to
schema 16 while preserving all 20 events; the unchanged snapshot correctly
left the checkpoint table empty. Appending one event then forced the
missing-checkpoint full-replay bootstrap: the candidate reported one import
and 20 duplicates, preserved exact 21-request totals, and created a Claude
checkpoint at the new source length. The generated pre-migration backup
passed SQLite `integrity_check`. Core migration tests also passed for schema
15 failure/backup preservation and refusal of newer schemas. This migration
and bootstrap was a correctness check only; its duration is not included in the
steady-state performance results or reported as a separate timing.

## Host and measurement method

The host was Ubuntu 24.04.5 LTS, kernel `7.0.0-38-generic`, Intel Core
i7-10510U (4 cores / 8 logical CPUs), 15.4 GiB RAM, `powersave` CPU governor,
and ext4 on `/tmp`. Go was `1.26.9`. Builds and measurements were serialized.
The desktop was not isolated: a pre-existing interactive CLI process used
about one full CPU core throughout the run, and browser/Codex processes were
active. Available memory varied around 0.9–1.2 GiB, with approximately
3.6–3.9 GiB of 4 GiB swap in use. That process was left untouched. This shared
host load makes absolute timings noisy; paired order was alternated for the
repeated cases.

CPU is collector-PID user plus system seconds gained during the measurement
window. Mean RSS is the average sampled resident set. HWM is the highest
sampled `/proc` `VmHWM` value and includes startup/warmup. `rchar`/`wchar` are
bytes passed through read/write calls; `read_bytes`/`write_bytes` are
kernel-attributed storage I/O deltas between measurement-window samples. They
are process-wide counters, not source-file counters. `read_bytes` near zero is
consistent with cached fixture reads and does not establish that no data was
read. No Go allocation profile or retained-heap measurement was taken. The
harness performs fixture generation/appends in a separate Python process, so
those writes are not attributed to the measured collector PID. Measurement,
append drain, and shutdown are distinct manifest fields. Foreground final
drain ranged from 0.000 to 1.449 seconds (cold runs have no append drain), and
graceful shutdown took 0.007–0.016 seconds. Detached final-poll drain was
0.508–0.930 seconds, followed by 1.239–2.051 seconds to cooperatively stop and
confirm process exit. These intervals are outside the resource measurement
window.

## Standalone CLI process results

Ranges below are three paired runs for each standard workload. CPU is seconds;
RSS/HWM and I/O are MiB. The append runs each validated exactly 11 appends.

| Workload | CPU before → candidate | Mean RSS before → candidate | HWM before → candidate | `rchar` before → candidate | `read_bytes` before → candidate | `write_bytes` before → candidate | `wchar` before → candidate |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Claude unchanged, 5s | 0.17–0.20 → 0.16–0.19 | 32.09–35.43 → 34.39–35.02 | 39.34–44.60 → 37.09–43.42 | 37.98 → 37.98 | 0 → 0 | 0.77 → 0.77 | 0.38 → 0.38 |
| Codex unchanged, 5s | 0.17–0.19 → 0.17–0.18 | 34.08–36.58 → 33.48–35.20 | 37.88–39.35 → 36.34–44.14 | 34.24 → 34.24 | 0 → 0 | 0.77 → 0.77 | 0.38 → 0.38 |
| Claude usage append, 5s | 9.82–9.87 → 0.67–0.71 | 35.07–36.01 → 22.02–26.38 | 45.35–49.43 → 37.73–44.99 | 60.52–60.55 → 72.99–73.02 | 0 → 0–0.14 | 1.29–1.32 → 1.47–1.53 | 0.89–0.91 → 1.03–1.07 |
| Codex usage append, 5s | 11.38–11.51 → 0.66–0.82 | 34.71–35.31 → 23.21–27.55 | 45.39–49.35 → 36.56–44.54 | 54.58–60.34 → 65.81–65.82 | 0 → 0 | 1.24–1.38 → 1.48–1.54 | 0.84–0.96 → 1.04–1.09 |
| Claude non-usage append, 5s | 10.88–11.12 → 0.72–0.85 | 35.25–35.94 → 22.70–23.08 | 45.73–49.76 → 43.90–44.39 | 66.68–66.74 → 72.85 | 0 → 0 | 0.89 → 1.04 | 0.49 → 0.59 |
| Codex non-usage append, 5s | 11.60–12.30 → 0.63–0.69 | 34.59–35.30 → 24.75–27.05 | 43.95–50.29 → 38.14–39.50 | 54.48–65.88 → 59.97–65.69 | 0 → 0 | 0.86–0.89 → 0.98–1.04 | 0.47–0.49 → 0.56–0.60 |

The CLI collector used substantially fewer CPU seconds in these synthetic
usage and non-usage append windows. The repeated CPU ranges do not overlap.
Mean sampled RSS was lower in the candidate append runs; HWM ranges overlap in
some groups. This is process-level evidence for these fixtures, not a general
latency guarantee or evidence that a memory leak was fixed. The unchanged
polling CPU ranges overlap and have matching `rchar`, with no clear resource
change. Cold backfill was also similar: Claude CPU was 1.70 → 1.65 seconds and
Codex 1.71 → 1.73 seconds; one run per side is inconclusive.

The process-wide `rchar` increased by about 20% in Claude usage append and
roughly 9–20% in Codex usage append; non-usage append also increased or
overlapped at its range edge. One Claude candidate append run recorded 0.14 MiB
of `read_bytes`; the other append windows rounded to zero. Candidate append
`write_bytes` and `wchar` were also modestly higher. These counters include
database and metadata access, and cannot attribute bytes to parser work. The
core work-counter benchmark reports equal source bytes read and only a small
increase in full-source bytes hashed on the candidate. Full-file verification
remains in place, so these measurements do not support an append-byte-only I/O
claim or a physical disk-read improvement.

The 10-minute Claude soak was one run per side: CPU was 114.20 → 7.50 seconds,
mean RSS 35.62 → 20.91 MiB, HWM 49.03 → 37.82 MiB, and `rchar` 664.42 →
756.26 MiB for 119 appends. `write_bytes` was 14.91 → 16.73 MiB and `wchar`
10.76 → 12.12 MiB; `read_bytes` rounded to zero on both sides. This confirms
accounting over one sustained run, not a leak or general performance result.

Single-run diagnostics also completed for cold Claude/Codex, 100-file
Claude/Codex histories, 70-KiB-padded Claude/Codex records, 1-second stress,
and detached Claude/Codex collection. Results were mixed and mostly small:
many-file CPU was 0.61 → 0.79 seconds (Claude) and 0.64 → 0.76 (Codex);
large-record CPU was 0.08 → 0.09 and 0.07 → 0.08; stress CPU was 0.75 → 0.87
and 0.71 → 0.83; detached CPU was 0.16 → 0.18 and 0.16 → 0.17. These
single-pair cases are diagnostic only. All four detached runs confirmed
cooperative stop of the sampled PID and exact instance; all measured foreground
PIDs exited. No harness workspace remained after cleanup.

## Core deterministic benchmark

Command: `BENCH_REPEAT_COUNT=5 GOWORK=off scripts/benchmark-append-checkpoint.sh`
at candidate core commit `e5d396a61a6222610d3f8e43772d5861692a01a9`. It builds
the benchmark against the independent v0.7.1 worktree and the candidate, with
five `-benchtime=1x` samples per case. This is core-level parser/storage work,
not standalone CLI timing.

| Provider / case | v0.7.1 time range | Candidate time range | Parser records | Historical insert attempts | Source bytes read | Source bytes hashed | Allocated bytes/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Claude / 10k unchanged | 6.93–10.70 ms | 7.12–7.65 ms | 0 → 0 | 0 → 0 | 1,869,025 → 1,869,025 | 1,869,025 → 1,869,025 | 96,016 → 110,408 |
| Codex / 10k unchanged | 8.28–9.96 ms | 8.57–13.24 ms | 0 → 0 | 0 → 0 | 2,281,332 → 2,281,332 | 2,281,332 → 2,281,332 | 95,368–95,400 → 120,376 |
| Claude / 10k one-record append | 833–960 ms | 34.8–46.7 ms | 10,002 → 1 | 10,000 → 0 | 3,738,426 → 3,738,426 | 9,335,557 → 9,345,876 | ~116,849,000 → ~2,133,000 |
| Codex / 10k one-record append | 923–1,069 ms | 44.3–59.0 ms | 10,002 → 1 | 10,000 → 0 | 4,563,128 → 4,563,128 | 11,397,154 → 11,407,587 | ~137,681,000 → ~2,639,000 |

For each supported append case, event insertion attempts fell from 10,001 to
one, including the new event; the 10,000 historical attempts were omitted while
the equivalent duplicate receipt was preserved. Candidate full-source hashing
was slightly higher, not append-only. Unchanged timing ranges overlap. At
10,000 records, the candidate used more allocations on unchanged snapshots
(Claude 275 → 518 allocs/op; Codex 265 → 554), despite similar timings.

Corrupt-checkpoint and state-limit fallback cases on both providers decoded
all 10,002 records and repeated 10,000 historical and 10,001 total event
insertion attempts. Their timing ranges overlap or are noisy; fallback remains
full-replay cost. The candidate source’s core tests also passed for corrupt,
oversized, old-format/parser, prefix-mismatch, interior-boundary, and missing
checkpoint cases.

## Validation and delivery status

The following passed against the exact candidate core override and unchanged
CLI v0.8.1 Go sources:

```text
GOWORK=off go -C /tmp/komposyn-cli-after-checkpoint mod verify
  all modules verified
GOWORK=off go -C /tmp/komposyn-cli-after-checkpoint vet ./...
  passed
GOWORK=off go -C /tmp/komposyn-cli-after-checkpoint test ./...
  passed
GOWORK=off go -C /tmp/komposyn-cli-after-checkpoint test -race ./...
  passed
GOWORK=off go -C /tmp/komposyn-core-after-checkpoint test ./plugins/library/token-logger -run '^(TestParserCheckpointContinuationSkipsHistory|TestParserCheckpointRereadsUnterminatedRecord|TestParserCheckpointFallbackCases|TestCodexCheckpointPreservesResponseAndCumulativeState|TestCodexCheckpointCapturesEveryProcessedBoundaryBeforeTail|TestCodexCheckpointReplaysNewSessionScope|TestParserCheckpointTransactionRollsBackIdentityEventsAndCursor|TestParserCheckpointCancellationLeavesCoupledStateUnchanged|TestMigrationSixteenFailurePreservesSchemaFifteenAndBackup|TestMigrationBackupFailureLeavesSchemaUnmigratedAndPreservesDestination|TestMigrationFailureLeavesOriginalAndVerifiedBackupUsable|TestOpenRefusesNewerSchemaWithoutChangingDatabaseOrWAL|TestMigrationsOneThroughFifteenPreserveLegacyData)$' -count=1
  passed
GOWORK=off go -C /tmp/komposyn-core-after-checkpoint test ./...
  passed
BENCH_REPEAT_COUNT=5 GOWORK=off scripts/benchmark-append-checkpoint.sh
  passed, five samples per benchmark case
```

The candidate CLI suite includes `TestHarnessProtocolFixture`. A direct
five-request binary exchange also passed on both builds: protocol-v1
initialize, record, flush, health, and shutdown responses were valid JSONL;
stdout contained only those responses, stderr was empty, the record imported
once, and shutdown confirmed `stopped=true`. The initialize response retained
`cli_version=0.8.1` and reported the corresponding core version on each side.

The focused core test selection covered continuation, tail completion, both
provider fallback cases, Codex cumulative/deduplication state, transaction
rollback, cancellation, schema-16 migration failure, verified-backup failure,
and newer-schema refusal. The complete core suite passed as well. The CLI
release-provenance check was not run against a temporary replacement: this is
an unpublished-core measurement, not a tagged release build. The committed
dependency pin and its release guard remain unchanged. No signed six-target
release artifacts or release-upgrade claim are part of this report.

The measurement phase is ready for review. The evidence supports reduced
collector CPU and historical parser/insertion work on the tested stable-session
append fixtures, while unchanged polling was broadly similar and process-wide
read/write counters increased on append cases. Integrate a published core
version only after its release is available and reviewed. Keep #23 and #20
open; no release, tag, dependency update, merge, or issue closure is included.
