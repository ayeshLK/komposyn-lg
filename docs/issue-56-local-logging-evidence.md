# Issue #56 local logging evidence

This note gathers synthetic implementation and resource evidence for the
bounded local JSONL logger, including the production collector comparison
added for issue #56. It is not a release-readiness claim or a benchmark of
customer workloads.

## Reproduction

At the time of measurement, the dedicated branch was based on CLI commit
`304804f8c13d6f1d81fd193ec4f518ba44110d6b` (master). The Go test benchmark
harness is `plugins/cli/token-logger/app_log_test.go`, SHA-256
`bdafe9733ef8ba997b71b16625c042395d6371e7144d09f881a15bbf1ac0660c`.
The compiled test binary was
`/tmp/komposyn-lg-issue56-logbench.test`, SHA-256
`f3190833098b3e53cae528ff87b746763b7e3fc1b349c0d47915a198db1be69b`.
No dependency override was used; the CLI's ordinary core dependency was used.

Build and run from the repository root:

```sh
go test -c -o /tmp/komposyn-lg-issue56-logbench.test ./plugins/cli/token-logger
for scenario in normal-version error-unknown-command; do
  for mode in logging-off logging-on; do
    for repetition in 1 2 3; do
      /usr/bin/time -f 'user=%U sys=%S elapsed=%e maxrss_kb=%M' \
        /tmp/komposyn-lg-issue56-logbench.test -test.run '^$' \
        -test.bench "^BenchmarkRunVersionLogging/${scenario}/${mode}$" \
        -test.benchtime=3000x -test.benchmem
    done
  done
done
```

The test invokes the CLI process wrapper in-process 3,000 times per sample.
The normal case runs `version`; the error case runs an unknown command and
records its fixed failure lifecycle events. The logging-on case creates its
log directory under Go's temporary test directory. Runs were serialized.

## Host and settings

- Linux `7.0.0-38-generic`, x86_64, Go `go1.26.9 linux/amd64`, `CGO_ENABLED=1`.
- Intel Core i7-10510U at 1.80 GHz, 8 logical CPUs; 16 GiB installed RAM.
- `/tmp` is on the root ext4 filesystem; filesystem storage is shared with the
  host. CPU governor was `powersave`.
- Host load averages after the run were 0.93, 1.30, 1.36. This was a shared
  development host, so these numbers are noisy and not a controlled device
  comparison. No other benchmark was run concurrently.
- No special `GOFLAGS` or build flags were set.

## Results

Ranges are min–max across three serialized samples, each with 3,000 invocations.

| Scenario | Logging | ns/op | B/op | allocs/op | log bytes per sample | process CPU / wall time | process HWM |
|---|---|---:|---:|---:|---:|---|---:|
| Version | Off | 227.5–395.6 | 96 | 3 | 0 | below `/usr/bin/time`'s 0.01s display precision | 9,896–10,288 KiB |
| Version | On | 88,836–91,321 | 25,763–25,769 | 122 | 1,580,854–1,580,886 | user 0.17–0.18s; sys 0.13–0.15s; wall 0.27s | 15,424–15,536 KiB |
| Unknown command | Off | 331.6–409.4 | 48 | 2 | 0 | below `/usr/bin/time`'s 0.01s display precision | 9,840–10,096 KiB |
| Unknown command | On | 91,496–94,452 | 26,990–27,015 | 126–127 | 2,369,744–2,369,799 | user 0.17–0.18s; sys 0.14–0.15s; wall 0.28s | 15,240–15,416 KiB |

`ns/op`, `B/op`, and `allocs/op` come from Go's benchmark harness. The
`/usr/bin/time` CPU, wall, and maximum-RSS figures describe the whole short-lived
test process, not a per-invocation sample. The memory figure is process HWM;
it is not sampled RSS during a command, retained Go heap, or allocation volume.
The `log-bytes` metric is the managed log directory's resulting file size,
including the active file and retained archives. These are logical file bytes;
no physical device I/O counters were collected.

Logging adds work and allocations to these deliberately tiny commands, where
startup/close overhead dominates. The results do not predict collector runtime
cost, do not establish a user-visible latency regression for normal workloads,
and do not claim a CPU or memory improvement. The bounded queue, batch writes,
and rotation are covered separately by synthetic concurrency, recovery, and
resource-bound tests.

## Validation scope and limitations

The tests exercise lifecycle records, privacy exclusions, stdout/protocol/JSON/
CSV equivalence, bounded queue and suppression state, lock timeout, rotation,
torn-tail recovery, and concurrent process writers. The Linux host was native;
Windows amd64 and macOS arm64 can only be reported as cross-compilation unless
native CI runners execute their platform tests. No native Windows or macOS
runtime measurement was performed. These checks do not replace repository CI
or release verification.

## Queue comparison requested in issue comment 6416610258

The request asked us to evaluate `github.com/ayeshLK/lib-disruptor v0.6.0`
as an alternative to the bounded buffered channel before deciding whether to
adopt it. The comparison used CLI implementation commit `1297700` and released
core tag `v0.9.0`. The candidate tag resolves to
`2a453be90ec1838857a1fff30d2e82ed29732223`; the module checksum is
`h1:ShU1UkFxkZ7tECF8ozo9YfipD91M3XR0DkilK4QbtBU=`. Its `go.mod` declares Go
1.25. The optional Linux-only harness is
[`app_log_queue_compare_test.go`](../plugins/cli/token-logger/app_log_queue_compare_test.go),
SHA-256 `b7f374a8a2193c3526a9f3cbdcf722c66192d83967898723a1b663668f57271d`.
It is excluded from ordinary builds and uses no production dependency change.

Reproduce from the repository root with a temporary modfile (the commands do
not edit the committed `go.mod` or `go.sum`):

```sh
cp go.mod /tmp/issue56-queuecompare.mod
cp go.sum /tmp/issue56-queuecompare.sum
go mod edit -modfile=/tmp/issue56-queuecompare.mod \
  -require=github.com/ayeshLK/lib-disruptor@v0.6.0
go test -mod=mod -modfile=/tmp/issue56-queuecompare.mod \
  -tags=queuecompare ./plugins/cli/token-logger \
  -run '^TestIssue56Queue(IdleMetrics|RecoveryAfterPressure|PacedFileSinkAndRetainedPayload|FailedWriterAcknowledgesAndRecovers|ShutdownBudgetAndJoin|RingReuseClearsPayloadTail)$' \
  -count=3 -v
go test -mod=mod -modfile=/tmp/issue56-queuecompare.mod \
  -tags=queuecompare ./plugins/cli/token-logger -run '^$' \
  -bench '^BenchmarkIssue56(Queue|FileSink|ErrorBurst)$' \
  -benchtime=50000x -benchmem -count=3 -cpu=1,8
```

Go resolved v0.6.0 from the published module with checksum above; the CLI
module remained pinned to core v0.9.0 and gained no permanent dependency.
Both adapters use a 256-entry waiting bound, pre-encoded valid JSON payloads
capped at 8 KiB, batches of at most 64, nonblocking publication, and the same
`jsonlLogSink.appendBatch` implementation for file-sink cases. The channel
consumer removes up to 64 entries into its batch before writing, so those slots
become available while the batch is in flight. The ring keeps those entries
inside its 256 gated slots until the batch is acknowledged. This is the existing
channel behavior versus the candidate ring ownership model, and it affects burst
capacity; it is not hidden in the results. The channel transfers ownership of
the event byte slice. The candidate uses
`MultiProducer`, `TryPublish`, one `BlockingWait` processor, a gate registered
before publication, and lazily allocated slot-owned buffers capped at 8 KiB.
It clears the full reused slot before copying the next event. Its handler copies
the event bytes into the sink batch before acknowledging the ring sequence;
write failures are counted and acknowledged, never replayed.
Publication and shutdown are synchronized, and processor termination closes
admission. These are queue-adapter measurements, not measurements of the full
`slog` encoder path.

The host was the same shared Linux x86_64 system described above: Go 1.26.9,
Intel i7-10510U, 16 GiB RAM, ext4, `powersave` governor, load averages near
1.4. Runs were serialized. Results are ranges across three runs unless stated
otherwise; these intentionally forced bursts are capacity/latency stress tests
and dropped counts are part of the result. Benchmark ns/op is producer enqueue
time while the consumer runs; sink drain and shutdown are outside the timed
region. Sink write counts are collected after the queue drains.

| Workload | Buffered channel | MultiProducer ring | Reading |
|---|---|---|---|
| Queue-only, 256-byte payload, 50,000 concurrent attempts, 1 CPU | 28.81–36.16 ns/op; 0 B/op; 0 allocs/op | 43.45–46.22 ns/op; about 1 B/op; 0 allocs/op | Channel was faster; both dropped 49,744 attempts because the producer burst outran the consumer. |
| Queue-only, 256-byte payload, 8 CPUs | 68.95–73.46 ns/op; 0 B/op | 87.38–95.98 ns/op; 0 allocs/op | Channel was faster; drop totals varied with scheduler timing. |
| Queue-only, 8 KiB payload, 1 CPU | 28.59–29.91 ns/op; 0 B/op | 47.34–73.13 ns/op; about 42 B/op | Channel was faster; the ring lazily allocates bounded slot storage as events arrive. |
| Queue-only, 8 KiB payload, 8 CPUs | 70.28–73.53 ns/op; 0 B/op | 98.43–143.7 ns/op; 42–52 B/op | Ring had wider, slower results; the channel remained faster in all three runs. |
| Actual JSONL sink, burst, producer enqueue, 8 CPUs | 86.18–94.74 ns/op; 259–416 written | 72.71–75.39 ns/op; 1,804–2,166 written | Ring admitted more and was faster in this oversubscribed run; both still dropped over 95% of 50,000 attempts. |
| Slow consumer (20 µs per batch), burst, 8 CPUs | 84.01–89.70 ns/op; 259–331 consumed | 81.57–86.24 ns/op; 345–665 consumed | Ring admitted more with overlapping enqueue times, but still dropped over 98.6%. |

The paced file-sink test sent 1,024 valid 8 KiB records at 100 µs intervals.
All events were accepted and written by both adapters. It crossed the 256-slot
ring four times, rotated the actual log, and parsed all 1,024 records from the
active file and archives. Across three runs:

| Metric | Buffered channel | MultiProducer ring |
|---|---:|---:|
| Enqueue p50 | 1.01–1.18 µs | 2.80–2.98 µs |
| Enqueue p95 | 3.15–3.58 µs | 11.06–11.35 µs |
| Enqueue p99 | 4.64–12.99 µs | 29.16–31.56 µs |
| Heap change after drain and GC | approximately -4.4–22.8 KiB (measurement noise around baseline) | 2.095–2.096 MiB retained |

After idle startup, each adapter used one worker goroutine. Process CPU usage
was below `/proc`'s 10 ms tick resolution during each one-second idle sample.
The channel's idle heap delta was 5.8–12.1 KiB; the ring's was 13.7–13.9 KiB.
Idle shutdown took 13–23 µs for the channel and 24–364 µs for the ring. A
five-run pressure/recovery test accepted 320–448 of 5,000 immediate events
with the channel and 256 with the ring; both drained and accepted all 100 later
paced events. A three-run synthetic writer-failure test made the first five
sink batches fail, then succeeded: both continued, counting five failed events
and writing the remaining 195. A blocked-sink shutdown test returned at a 20 ms budget for both adapters; after
releasing the synthetic sink, each worker exited. This confirms the wait budget
does not interrupt the writer call. The optional tests and
concurrent-publication benchmark also passed under `-race`.

### Decision

Keep the buffered channel in production. It is already behind the CLI-owned
`applicationLog` interface, needs one worker, and provides the required bounded
nonblocking behavior. At the realistic paced rate it had lower enqueue latency,
near-zero retained payload memory after draining, and no drops. The ring was
slower in queue-only and paced tests, retained up to 256 × 8 KiB of slot
buffers after a large-record burst, and added processor supervision, explicit
acknowledgement, admission shutdown, and drain coordination. It admitted more events and had lower producer enqueue time in the artificial
high-concurrency slow-writer and actual-sink bursts, but still dropped most of
the 50,000-event burst; the production logger also suppresses repeated
lifecycle errors. That stress-only capacity difference does not justify the
extra runtime dependency and lifecycle machinery for this CLI event volume.

The app logger serializes its event encoding and suppression path under a mutex,
so the concurrent-producer bursts exceed its expected production topology. The
comparison does not establish behavior on native macOS or Windows, does not
replace the native multiprocess file tests, and does not claim the channel is
faster for every workload. If event volume or producer topology changes,
repeat this comparison with representative traffic before reconsidering the
queue.

## Production collector resource comparison

The opt-in Linux harness
[`app_log_resource_linux_test.go`](../plugins/cli/token-logger/app_log_resource_linux_test.go)
compares the production foreground collector with its production `eventLogger`
enabled and with `discardApplicationLog`. It runs each sample in a separate
test-binary process, serially. Each of three repetitions covers both workloads
with logging on and off; the mode order alternates between repetitions.

Reproduce from the repository root. Results and all generated fixtures remain
outside the repository:

```sh
ISSUE56_RESOURCE_EVIDENCE=1 \
ISSUE56_RESOURCE_OUTPUT=/tmp/issue56-resource-results.json \
go test ./plugins/cli/token-logger \
  -run '^TestIssue56ProductionLoggerResourceComparison$' -count=1 -v -timeout=15m
```

The harness and logger changes are CLI commit
`b64d45e5cfffc6f4bb28d8aab40a98d5bf410164`, based on merged master
`e771dc0448b0f256b29db091d3a911f8df5f5e82`. The harness SHA-256 is
`f61378eabf90e8ed545419a36ac894b19925d43d1203e2c0804254c20063ec26`. It uses
Go `1.26.9 linux/amd64`, default `go test` build flags,
`CGO_ENABLED=1`, no `GOFLAGS`, and the repository's pinned core `v0.9.0`.
The test binary has the source default CLI version `0.0.0-dev`; it uses
`tokenlogger.CoreVersion()` for core metadata. The event logger writes only
inside the synthetic per-sample data directory.
The source template is
`plugins/library/token-logger/testdata/codex/normal.jsonl`, SHA-256
`63342f9fd2dc6345bac2610c334f0c214fb0bd8294addd63b6a2136d4c346cf2`. Before
the timed runs, the harness creates 96 independent synthetic Codex files by
replacing the template session ID, project ID, and token-count IDs with
numbered synthetic values. Both logger modes use the same files. The collector
polls every 100 ms; the harness waits for the initial import to finish, warms
for 3 seconds, then measures for 30 seconds. Each ordinary sample imports 192
usage records, then scans the unchanged sources during the measurement window.

The second workload points at the same 96 distinct, deliberately absent
synthetic paths in every mode. It exercises recurring source-poll failures and
the logger's suppression summaries. It imports no records. Both scenarios run
with disposable databases and log directories below the test's temporary
directory. No provider files, user home, or normal application data are read
or changed. Collection shutdown and logger shutdown are measured after the
30-second window. The test fails if ordinary collection emits errors or
suppression, if accounting differs from the fixture's 192 records, or if the
failure workload produces no suppression summary.

Measurements are process CPU seconds, RSS sampled every 100 ms, process HWM on
the per-sample child, Linux `/proc/self/io` counter deltas, Go total allocation
delta, and `HeapAlloc` at the end of the measurement window. `HeapAlloc` is not
a post-GC retained-heap measurement. The I/O values are per-process kernel
counters, not log-file bytes or a complete measure of physical device traffic.
Poll latency is unavailable from the current collector interface. Log volume
is the logical size of active and rotated log files after orderly shutdown.

Host: Linux `7.0.0-38-generic`, x86_64, Intel Core i7-10510U with 8 logical
CPUs, 15.4 GiB installed RAM, `/tmp` on the root ext4 filesystem, and the
`powersave` governor. Load averages after the matrix were 0.90, 0.90, 1.44.
Runs were serialized without another benchmark. Results are ranges over three
samples per mode. RSS shows each process's starting RSS through its peak 100 ms
sample. HWM is process lifetime HWM at the end of the measurement. MiB is
1,048,576 bytes.

| Workload | Logging | CPU seconds / 30s | Sampled RSS start–peak (MiB) | HWM (MiB) | Go allocations (MiB) | End `HeapAlloc` (MiB) | `/proc/self/io` read / write (MiB) | Log bytes | Suppressed / dropped | Collector shutdown | Logger shutdown |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Ordinary, 96 Codex files | On | 11.21–11.75 | 22.03–23.42 | 22.84–23.46 | 1,812–1,866 | 1.32–2.43 | 0 / 5.89–6.05 | 512 | 0 / 0 | 2.24–5.86 ms | 0.020–0.050 ms |
| Ordinary, 96 Codex files | Off | 11.06–12.80 | 21.84–23.21 | 22.41–23.17 | 1,700–1,876 | 0.68–2.60 | 0 / 5.55–6.07 | 0 | 0 / 0 | 3.08–3.78 ms | n/a |
| 96 missing-source failures | On | 3.62–3.96 | 18.91–22.62 | 21.44–22.22 | 80.04–94.32 | 2.62–3.70 | 0 / 106.92–126.71 | 16.83–18.57 KiB | 72–88 / 0 | 1.95–44.23 ms | 0.140–0.230 ms |
| 96 missing-source failures | Off | 3.25–4.02 | 18.79–22.50 | 21.12–21.95 | 74.40–92.89 | 3.03–4.21 | 0 / 99.32–126.31 | 0 | 0 / 0 | 2.03–92.22 ms | n/a |

The ordinary CPU, sampled RSS, HWM, allocation, and I/O ranges overlap. Paired
ordinary CPU differences (logging on minus off) were `+0.03`, `+0.23`, and
`-1.05` seconds. Paired allocation differences were `-0.12`, `-10.40`, and
`+111.80` MiB, showing substantial run-to-run variability. The failure
workload ranges also overlap; paired CPU differences were `+0.70`, `-0.06`,
and `-0.26` seconds, with allocation and write-counter differences changing
direction across pairs. Its suppression summaries represented 72–88 suppressed
events, with no drops. These samples do not establish a consistent resource
regression or improvement. All ordinary runs imported the expected 192
records. Poll latency was not separately measured because the current
collector interface does not expose a poll-duration measurement.

Raw results are at `/tmp/issue56-resource-results-final.json` on the measurement
host and are not committed. Native macOS and Windows runtime evidence still
depends on their native CI runners; Linux results do not predict those
platforms.

## Remaining issue #56 acceptance audit

| Acceptance item | Evidence |
|---|---|
| Rotation remove/rename failures preserve valid records and unrelated files; archives remain bounded | `TestRotationFailuresPreserveRecordsAndRecoverGaps` in `app_log_test.go` |
| Failed write plus failed truncation records uncertainty, does not replay a completed prefix, and recovers suppression/drop evidence | `TestFailedTruncationAccountsUncertainSummaryWithoutReplayingPrefix` |
| Actual production event logger queue saturation, suppression/drop summaries, recovery, repeated/concurrent Close, and post-Close publication | `TestProductionEventLoggerPressureRecoveryAndClose` and `TestProductionEventLoggerPublishersRaceNormalClose` |
| Close wait budget does not claim to cancel blocked filesystem work; release then joins worker | `TestProductionEventLoggerCloseBudgetDoesNotCancelBlockedFilesystemWork` |
| Effective protected current-user ACL on directory, active file, stable lock, and rotated archive | `TestPrivateWindowsLogDirectoryAndFiles`; requires native Windows CI execution |
| Repeated production collector resource comparison with accounting, CPU, memory, I/O, log volume, suppression/drop, and shutdown | This section and `TestIssue56ProductionLoggerResourceComparison`; native Linux run completed |
| Native macOS and Windows package/lifecycle behavior | Outstanding until the follow-up PR's native macOS and Windows CI jobs complete |

### Validation on the follow-up candidate

The following commands passed on Linux at CLI commit
`b64d45e5cfffc6f4bb28d8aab40a98d5bf410164`:

```text
go mod verify       all modules verified
go vet ./...        passed
go test ./...       passed (token-logger package: 24.8s)
go test -race ./... passed (token-logger package: 96.3s)
make check          passed, including installer trust, release provenance,
                    release-version-flow and executable-copy checks
git diff --check    passed
```

Windows test code also compiled with
`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -c -o /tmp/komposyn-lg-issue56-windows.test.exe ./plugins/cli/token-logger`.
That is compile evidence only. Native Windows ACL execution and native macOS
runtime validation remain dependent on PR CI. Post-merge master CI and CodeQL
were both green on 2026-10-26 (runs `40351736761` and `40351736678`); those
results describe the base commit, not this follow-up candidate.
