# Issue #23: standalone CLI before-state baseline

> **Historical record.** Historical baseline for CLI v0.8.0 against the core #137 before-state, with the recorded CLI/core commits, host, and fixture. It is limited to the recorded environment and does not establish current performance or release readiness. See the maintained [resource-measurement guide](../../contributing/resource-measurements.md).


This report isolates the CLI measurements for the exact before-state of core
issue #137. It records baseline behavior only; it does not make a before/after
performance claim.

## Provenance

- CLI source: `265bcd375cbd626232998ec9a3e3793ec09492f9`.
- Core source before-state: `0943bc8d3ef63eef98e907c856211af6d93424a7`.
- Executable: `komposyn-lg 0.8.0 (core 0.7.0)`, Linux amd64, Go 1.26.9,
  `CGO_ENABLED=0`, `-trimpath`, `-s -w`, release version injection.
- Binary SHA-256: `0d283667fe3eeeb8fc1007ef2deef1a08ed23267d7c9b703820fa591ee6857e3`.
- Core source was built from a detached checkout at the exact commit above.
  The temporary CLI copy used `replace github.com/ayeshLK/komposyn =>
  /tmp/issue23-core-before`; `go list -m` confirmed the resolved source path.
  The repository's `go.mod` remains pinned to published `v0.7.0` with no
  committed replacement.
- Host: Ubuntu 24.04.4, kernel `7.0.0-38-generic`, Intel Core i7-10510U
  (4 cores / 8 logical CPUs), 15.4 GiB RAM, `powersave` governor, ext4 `/tmp`.
  The host was not isolated from background activity.
- Raw manifests, logs, and process samples are under
  `/tmp/issue23-core137/before` on the measurement host; they are not committed.

This is distinct from the earlier historical baseline in
[`issue-23-baseline-linux-amd64.md`](./issue-23-baseline-linux-amd64.md), which
used published core `v0.7.0` source commit `d66b4117d8b7771f9c3c22b3b251e19054ba9e88`.

## Workloads and measurement window

Generated fixtures used Claude and Codex profiles. Normal histories contained
10,000 events; cold backfills split them across four files. The many-file case
used 100 files and 10,000 events. Large-record histories contained 1,000
records padded by 16 KiB. Append scenarios began with 10,000 events and added
one record every 5 seconds. Warm unchanged, append, many-file, and large-record
runs used 120-second measurement windows. Unchanged runs warmed up for 15
seconds; other warm runs warmed up for 10 seconds. Cold timing began before
launch and ended at the first successful completed poll. A 1-second polling
stress case used unchanged data for 60 seconds after 10 seconds warmup.
Detached-mode unchanged runs used the default 5-second interval for 60 seconds
after 10 seconds warmup. The Claude usage soak ran 600 seconds at 5 seconds
after 10 seconds warmup, with 119 appended records.

The harness sampled `/proc/<pid>/stat`, `status`, and `io` every 0.5 seconds.
CPU seconds are user plus system CPU accrued during the measurement window.
Mean RSS is average sampled resident memory during that window. `VmHWM` is the
sampled process high-water RSS over the process lifetime, including warmup.
`rchar` is bytes returned by read-like calls; it is not physical disk I/O.
`read_bytes` and `write_bytes` are the process-attributed kernel counters.
Values are rounded; one-run rows are diagnostic measurements, not stable
performance claims.

## Baseline results

| Workload | Repetitions | CPU seconds | Mean RSS MiB | `VmHWM` MiB | `rchar` MiB | `read_bytes` MiB | `write_bytes` MiB |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Cold Claude import, 10k events / 4 files, 5s | 1 | 1.78 | 14.38 | 23.83 | 43.49 | 0.01 | 33.02 |
| Cold Codex import, 10k events / 4 files, 5s | 1 | 1.69 | 14.58 | 23.73 | 42.19 | 0.00 | 32.80 |
| Unchanged Claude, 10k / 1 file, 5s, 120s | 3 | 0.45–0.62 | 26.12–27.62 | 43.07–44.81 | 132.94 | 0.00 | 1.32–1.33 |
| Unchanged Codex, 10k / 1 file, 5s, 120s | 3 | 0.47–0.61 | 25.38–28.96 | 42.93–43.39 | 114.14–119.85 | 0.00 | 1.30–1.32 |
| Claude usage append, 10k initial, 23 appends, 5s, 120s | 2 | 19.38–25.78 | 36.31–36.44 | 54.79–55.22 | 107.83–114.18 | 0.00 | 2.37–2.43 |
| Codex usage append, 10k initial, 23 appends, 5s, 120s | 2 | 31.09–32.08 | 36.05–36.40 | 53.91–55.42 | 85.79–91.52 | 0.00 | 2.27 |
| Claude non-usage append, 10k initial, 23 appends, 5s, 120s | 2 | 23.13–28.04 | 36.61–36.63 | 54.75–55.90 | 101.33–107.66 | 0.00 | 1.50–1.59 |
| Codex non-usage append, 10k initial, 23 appends, 5s, 120s | 2 | 25.35–26.10 | 35.49–35.63 | 54.58–54.73 | 91.38–97.08 | 0.00 | 1.54–1.57 |
| Claude many files, 100 files / 10k events, 5s, 120s | 1 | 1.37 | 19.66 | 20.73 | 136.14 | 0.00 | 3.46 |
| Codex many files, 100 files / 10k events, 5s, 120s | 1 | 1.30 | 20.02 | 21.38 | 120.04 | 0.00 | 3.44 |
| Claude large records, 1k x 16 KiB padding, 5s, 120s | 1 | 2.13 | 64.36 | 64.90 | 670.43 | 0.00 | 1.38 |
| Codex large records, 1k x 16 KiB padding, 5s, 120s | 2 | 1.31–2.08 | 64.20–64.24 | 65.89–68.86 | 668.96 | 0.00 | 1.38–1.39 |
| **Stress:** Claude unchanged, 1s, 60s | 1 | 1.17 | 25.26 | 44.43 | 284.88 | 0.00 | 1.45 |
| **Stress:** Codex unchanged, 1s, 60s | 1 | 1.04 | 30.37 | 42.77 | 256.81 | 0.00 | 1.45 |
| Detached Claude unchanged, 5s, 60s | 1 | 0.22 | 25.89 | 46.38 | 56.98 | 0.00 | 0.59 |
| Detached Codex unchanged, 5s, 60s | 1 | 0.20 | 31.79 | 45.10 | 57.07 | 0.00 | 0.62 |
| Claude usage append soak, 10k initial, 119 appends, 5s, 600s | 1 | 108.87 | 37.19 | 55.45 | 619.42 | 0.00 | 14.77 |

The physical-read counter remained at or near zero because fixture data was
served from cache. Append CPU is much higher than unchanged polling on this
synthetic workload. The soak does not independently establish or rule out a
memory leak; these RSS measurements do not measure Go heap retention.

One initial Claude usage-append run failed exact final-count validation because
an append arrived after a poll started but before it completed. The final
shutdown poll imported that record. The harness was updated to wait for exact
expected usage totals after a completed poll, the focused regression test
passed, and the affected baseline repeat was recaptured. The failed initial
run is excluded. All included usage appends and the soak passed exact count and
token-total validation. Foreground processes exited successfully; all
detached before-state processes confirmed cleanup, and no collector PID or
generated fixture workspace remained after the audit.

## Reproduction

Build from the exact CLI commit in a temporary copy and point its module
resolution at a detached core checkout of `0943bc8d3ef63eef98e907c856211af6d93424a7`:

```sh
git -C /path/to/komposyn checkout --detach 0943bc8d3ef63eef98e907c856211af6d93424a7
git -C /path/to/komposyn-lg checkout --detach 265bcd375cbd626232998ec9a3e3793ec09492f9
cp -a /path/to/komposyn-lg /tmp/cli-before
go -C /tmp/cli-before mod edit -replace=github.com/ayeshLK/komposyn=/path/to/komposyn
go -C /tmp/cli-before list -m -f '{{.Replace.Dir}}' github.com/ayeshLK/komposyn
CGO_ENABLED=0 go -C /tmp/cli-before build -trimpath -ldflags '-s -w -X github.com/ayeshLK/komposyn/plugins/library/token-logger.Version=0.7.0 -X main.cliVersion=0.8.0' -o /tmp/komposyn-lg-before ./plugins/cli/token-logger
```

Run `scripts/measure-collector.py` using the workload flags recorded in the
raw manifests and the matrix guidance in
[`resource-measurements.md`](../../contributing/resource-measurements.md). Keep each
run's artifact directory outside the repository. Do not use the temporary
override for release checks; it is intentionally refused by the release
provenance guard.
