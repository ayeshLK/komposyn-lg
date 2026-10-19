# Issue #23 CLI resource baseline

This report records the standalone CLI before-state for issue #23. It is a
single-host diagnostic baseline from generated synthetic histories, not a
performance budget or a claim about typical user workloads. It does not compare
candidate core changes.

## Revisions and environment

- CLI source: `265bcd375cbd626232998ec9a3e3793ec09492f9` (current `master` after
  merging CLI measurement and lifecycle coverage).
- Core dependency: published `github.com/ayeshLK/komposyn v0.7.0`.
- Published core source commit: `d66b4117d8b7771f9c3c22b3b251e19054ba9e88`.
- Core module checksum: `h1:y7c65NSsBgcQ4Wo8Cwac903dP9dzQ/53wz1nJMh9vaA=`.
- Executable: `komposyn-lg 0.8.0 (core 0.7.0)`, Linux `amd64`, Go `1.26.9`,
  built with `CGO_ENABLED=0`, `-trimpath`, release version injection, and
  `-s -w`.
- Executable SHA-256:
  `87af9e7634062017a2707b1ee2d6c3387f9928cccede4708f85c5afda104b6bc`
- Host: Ubuntu 24.04.4, Linux `7.0.0-38-generic`, Intel Core i7-10510U
  (4 cores / 8 logical CPUs), 15.4 GiB RAM, `powersave` governor, ext4 on `/tmp`.
- Measurements ran on 2026-10-18. The host was not isolated from other
  background activity; results should be repeated under controlled load before
  making close comparisons.
- Metrics are from the harness's Linux `/proc` sampling at 0.5-second
  intervals. CPU is a percentage of one logical CPU. `RSS` is the mean sampled
  resident set; `VmHWM` is the maximum sampled process high-water RSS, including
  startup and warmup. `Write bytes` is the process-attributed kernel counter
  delta during measurement. These values do not represent Go heap retention.

## Measurements

All workloads passed the harness's generated-fixture sanity checks and final
usage validation, except for the explicitly excluded 1-second append attempt
described below. Warm windows start after the initial poll and configured
warmup. The cold wall-time window starts before process launch and ends after
the first successful poll.

| Workload | Poll interval / window | Mean CPU (one core) | Mean RSS | `VmHWM` | Write bytes |
| --- | ---: | ---: | ---: | ---: | ---: |
| Cold Claude import, 10,000 events / 4 files | 5s / 2.712s to first poll | — | 16.5 MiB | 24.1 MiB | 33.0 MiB |
| Unchanged Codex history, 10,000 events / 1 file | 5s / 120s | 0.46% | 26.1 MiB | 45.7 MiB | 1.55 MiB |
| Claude usage append, 10,000 initial events; 23 appends | 5s / 120s | 17.72% | 35.5 MiB | 55.0 MiB | 2.73 MiB |
| Codex non-usage append, 10,000 initial events; 23 appends | 5s / 120s | 20.26% | 34.7 MiB | 53.7 MiB | 1.75 MiB |
| Unchanged Claude history, 10,000 events / 100 files | 5s / 120s | 0.99% | 19.6 MiB | 19.8 MiB | 1.54 MiB |
| Unchanged Claude history, 1,000 records padded by 16 KiB each | 5s / 120s | 1.49% | 64.5 MiB | 66.0 MiB | 1.51 MiB |
| Claude usage append soak, 10,000 initial events; 119 appends | 5s / 600s | 17.00% | 36.3 MiB | 58.2 MiB | 14.7 MiB |
| **Stress:** unchanged Claude history, 10,000 events / 1 file | **1s / 60s** | **2.84%** | **26.2 MiB** | **43.7 MiB** | **1.89 MiB** |

The append scenarios repeatedly scan a synthetic history and should be read as
stress workloads. Their CPU values are substantially higher than the unchanged
history runs. The soak's sampled RSS and `VmHWM` grew over the run, but this
measurement alone does not establish a memory leak; heap retention was not
measured.

An initial 1-second usage-append stress attempt generated 59 appends. The
harness's final pre-shutdown summary saw 58; the cooperative shutdown poll then
imported the final event. Because the harness returned a correctness failure,
that run is excluded from the table. The 1-second stress baseline above uses an
unchanged workload and passed exact-count validation.

## Reproduction

From the CLI source revision above, build the same executable with:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w -X github.com/ayeshLK/komposyn/plugins/library/token-logger.Version=0.7.0 -X main.cliVersion=0.8.0' -o /tmp/komposyn-lg-issue23-baseline ./plugins/cli/token-logger
```

The completed run manifests, samples, and command logs were kept under
`/tmp/issue23-baseline` on the measurement host and are intentionally not
committed. Re-run the documented commands in
[`resource-measurements.md`](contributing/resource-measurements.md) with an
`--artifacts` directory for new raw output. Preserve the executable flags,
host, generated fixture parameters, poll interval, warmup, and measurement
duration when comparing a future candidate.

This historical baseline used the published core `v0.7.0`, not the exact
pre-optimization source commit requested for attribution. The matched
before/after measurements using core commits `0943bc8` and `36fea986` are in
[`issue-23-core-137-comparison.md`](issue-23-core-137-comparison.md).
