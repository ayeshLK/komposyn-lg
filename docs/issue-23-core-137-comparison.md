# Issue #23: CLI measurements for core #137

This report compares the CLI against core immediately before and after the
single-acquisition optimization. It records process-level measurements and
compatibility evidence. It does not set a performance budget or establish a
general latency or memory improvement.

## Provenance

The original issue #23 baseline in [`issue-23-baseline-linux-amd64.md`](issue-23-baseline-linux-amd64.md)
used CLI `265bcd375cbd626232998ec9a3e3793ec09492f9` and the published core
`v0.7.0` module (`h1:y7c65NSsBgcQ4Wo8Cwac903dP9dzQ/53wz1nJMh9vaA=`), whose
source commit is `d66b4117d8b7771f9c3c22b3b251e19054ba9e88`. That historical
baseline predates the requested before-state and is not used to attribute
performance changes here.

The matched comparison used the same CLI source commit on both sides:

| Side | CLI source | Core source | Executable SHA-256 |
| --- | --- | --- | --- |
| Before | `265bcd375cbd626232998ec9a3e3793ec09492f9` | `0943bc8d3ef63eef98e907c856211af6d93424a7` | `0d283667fe3eeeb8fc1007ef2deef1a08ed23267d7c9b703820fa591ee6857e3` |
| Candidate | `265bcd375cbd626232998ec9a3e3793ec09492f9` | `36fea986b18d2c5ad32f4876b50412a03f9d933f` | `d273ef48efc54451d2cf9db4f4903ce901fe4044ab7a822b755c5d94b6e015ac` |

Both binaries reported `komposyn-lg 0.8.0 (core 0.7.0)` and were built on Linux
amd64 using Go 1.26.9, `CGO_ENABLED=0`, `-trimpath`, `-s -w`, and identical
release version injection. The source module directories were detached clones
at the exact commits above. Each temporary CLI source copy had a local
`replace github.com/ayeshLK/komposyn => ...` pointing to its corresponding
clone; `go list -m` confirmed the resolved directory before build. The normal
repository dependency remains pinned to published `v0.7.0`; no replace or
dependency change is committed.

The original baseline artifact directory `/tmp/issue23-baseline` is retained
outside the repository. Matched raw manifests, `/proc` samples, and logs are
under `/tmp/issue23-core137/{before,candidate}` on the measurement host and are
not committed.

## Host and workload method

The host was Ubuntu 24.04.4, kernel `7.0.0-38-generic`, Intel Core i7-10510U
(4 cores / 8 logical CPUs), 15.4 GiB RAM, `powersave` governor, ext4 `/tmp`,
Go 1.26.9. Background activity was not isolated. Runs were serialized on this
same host, alternating before and candidate for each repeated pair.

Fixtures were synthetic Claude and Codex histories. Default histories used
10,000 events, normally one file; cold backfills used four files. The many-file
case spread 10,000 events over 100 files. Large-record histories used 1,000
records padded by 16 KiB each. Append scenarios began with 10,000 events and
appended one record every 5 seconds; each 120-second run validated 23 appended
records. The 600-second Claude usage soak validated 119 appends. Unchanged
polling did not append records.

Default polling was 5 seconds. Standard steady-state windows were 120 seconds;
warmup was 15 seconds for unchanged histories and 10 seconds for append,
many-file, and large-record histories. Cold wall time began before process
launch and ended after the first successful completed poll, with no warmup.
Clearly labeled stress runs used unchanged histories at 1-second polling for
60 seconds after 10 seconds warmup. Detached mode ran unchanged histories at
the default 5-second interval for 60 seconds after 10 seconds warmup. The soak
used the default 5-second interval for 600 seconds after 10 seconds warmup.

The harness sampled `/proc/<pid>/stat`, `status`, and `io` every 0.5 seconds.
CPU is process CPU seconds (user plus system) during the measured window;
sampled RSS is the average resident set during that window. `VmHWM` is the
highest sampled process high-water RSS and includes startup/warmup. `rchar` is
bytes returned by read-like calls, not physical disk traffic. `read_bytes` is
the kernel's process-attributed physical read counter. All values below are
rounded; repetitions show ranges rather than implying precision unsupported
by this single-host experiment.

## Results

Each cell is before → candidate. CPU is seconds; RSS and HWM are MiB; `rchar`
and write counters are MiB. Repeated ranges include successful runs only.

| Workload | CPU seconds | Mean sampled RSS | Sampled `VmHWM` | `rchar` MiB | Process `write_bytes` MiB |
| --- | ---: | ---: | ---: | ---: |
| Cold Claude, 10k / 4 files (one run) | 1.78 → 1.95 | 14.38 → 14.84 | 23.83 → 24.30 | 43.49 → 40.13 | 33.02 → 32.98 |
| Cold Codex, 10k / 4 files (one run) | 1.69 → 1.72 | 14.58 → 16.40 | 23.73 → 23.92 | 42.19 → 39.63 | 32.80 → 32.88 |
| Unchanged Claude, 5s, 120s (3 runs) | 0.45–0.62 → 0.39–0.58 | 26.12–27.62 → 23.37–26.52 | 43.07–44.81 → 42.39–42.68 | 132.94 → 63.31–66.47 | 1.32–1.33 → 1.30–1.33 |
| Unchanged Codex, 5s, 120s (3 runs) | 0.47–0.61 → 0.48–0.59 | 25.38–28.96 → 24.15–26.02 | 42.93–43.39 → 44.59–45.70 | 114.14–119.85 → 59.92 | 1.30–1.32 → 1.32–1.33 |
| Usage append Claude, 5s, 120s (2 paired runs; retry included) | 19.38–25.78 → 16.89–28.14 | 36.31–36.44 → 36.20–37.63 | 54.79–55.22 → 53.61–55.84 | 107.83–114.18 → 47.61–57.13 | 2.37–2.43 → 2.10–2.46 |
| Usage append Codex, 5s, 120s (2 runs) | 31.09–32.08 → 30.82–31.22 | 36.05–36.40 → 35.58–36.01 | 53.91–55.42 → 54.21–55.48 | 85.79–91.52 → 42.96–45.83 | 2.27 → 2.26–2.30 |
| Non-usage append Claude, 5s, 120s (2 runs) | 23.13–28.04 → 23.76–24.41 | 36.61–36.63 → 36.51–37.05 | 54.75–55.90 → 54.16–55.13 | 101.33–107.66 → 53.83 | 1.50–1.59 → 1.58–1.59 |
| Non-usage append Codex, 5s, 120s (2 runs) | 25.35–26.10 → 27.59–27.84 | 35.49–35.63 → 35.88–36.14 | 54.58–54.73 → 54.11–54.67 | 91.38–97.08 → 45.69 | 1.54–1.57 → 1.51–1.54 |
| Many files: Claude, 100 files, 5s, 120s (one run) | 1.37 → 1.46 | 19.66 → 20.24 | 20.73 → 21.15 | 136.14 → 63.84 | 3.46 → 1.32 |
| Many files: Codex, 100 files, 5s, 120s (one run) | 1.30 → 1.37 | 20.02 → 20.37 | 21.38 → 20.90 | 120.04 → 58.98 | 3.44 → 1.34 |
| Large records: Claude, 16 KiB padding, 5s, 120s (one run) | 2.13 → 2.16 | 64.36 → 49.98 | 64.90 → 58.40 | 670.43 → 335.22 | 1.38 → 1.38 |
| Large records: Codex, 16 KiB padding, 5s, 120s (2 runs) | 1.31–2.08 → 1.52–2.23 | 64.20–64.24 → 50.64–52.24 | 65.89–68.86 → 57.70–84.51 | 668.96 → 334.48 | 1.38–1.39 → 1.38–1.39 |
| **Stress:** Claude unchanged, 1s, 60s (one run) | 1.17 → 1.30 | 25.26 → 25.22 | 44.43 → 51.62 | 284.88 → 145.61 | 1.45 → 1.48 |
| **Stress:** Codex unchanged, 1s, 60s (one run) | 1.04 → 0.77 | 30.37 → 25.80 | 42.77 → 44.37 | 256.81 → 128.41 | 1.45 → 1.45 |
| Detached Claude unchanged, 5s, 60s (one run) | 0.22 → 0.21 | 25.89 → 24.42 | 46.38 → 43.32 | 56.98 → 28.49 | 0.59 → 0.59 |
| Detached Codex unchanged, 5s, 60s (one run) | 0.20 → 0.21 | 31.79 → 24.02 | 45.10 → 44.12 | 57.07 → 28.53 | 0.62 → 0.62 |
| Claude usage append soak, 5s, 600s (one run) | 108.87 → 107.86 | 37.19 → 37.23 | 55.45 → 55.60 | 619.42 → 310.51 | 14.77 → 14.73 |

The core optimization's lower acquisition volume is visible in the CLI process:
`rchar` is close to half on warm unchanged, append, many-file, large-record,
stress, detached, and soak workloads. Cold backfill is a smaller reduction
(about 6–9%) because startup and initial work dominate. Physical `read_bytes`
was 0–0.06 MiB in essentially every measured window, consistent with the
fixture data being served from cache; this counter does not show a measurable
disk-read benefit. Write counters were generally similar. In single-run
many-file cases, process `write_bytes` was about 121 MiB on both sides and
`wchar` about 119 MiB; these are process-wide counters including fixture and
database activity and are not evidence of reduced database writes.

CPU ranges overlap for most repeated groups, with no broad or stable latency
improvement. Codex non-usage append used somewhat more CPU in both candidate
runs, while Claude usage append varied substantially; these small sample sizes
do not establish a regression. Mean sampled RSS is similar for ordinary and
append workloads. Both large-record providers had lower candidate mean RSS,
but the Codex candidate had one `VmHWM` outlier at 84.51 MiB and another repeat
at 57.70 MiB, so peak-memory improvement is inconclusive. The 10-minute soak
showed nearly identical CPU, RSS, and HWM; it does not establish or rule out a
Go heap leak.

One initial Claude usage-append run failed the final exact-count check because
an append landed after a poll began but before it completed. Inspection showed
the shutdown poll imported the missing row. The harness was corrected to wait
for the exact expected summary totals after polling, with a focused regression
test, and the affected before and candidate repeat was rerun. The initial
failed measurement is excluded. Corrected runs and all other append/soak runs
passed exact accounting.

## Compatibility and checks

The merged CLI lifecycle, protocol, accounting, stdout/stderr, and exact JSON/
CSV output test coverage was run against the candidate binary's exact source
combination (CLI `265bcd3`, core `36fea98`):

```text
go mod verify       PASS — all modules verified
go vet ./...        PASS
go test ./...       PASS
go test -race ./... PASS
```

`make check` on that candidate source reached and passed formatting, vet, test,
build, and installer trust checks, then the release-provenance guard rejected
the temporary module replacement. That guard explicitly requires the published
core module without replacements. It was left intact. A separate full
`make check` against the unchanged published `v0.7.0` pin passed, including
release provenance and executable-copy checks. The focused measurement harness
tests passed 10 tests (2 platform-specific skips).

All foreground collector processes exited successfully. Both provider profiles
passed detached-mode cleanup checks, and the harness recorded
`cleanup_confirmed: true` for all four detached before/candidate runs. No
collector PID or generated fixture workspace remained after the audit. Usage
append and soak summaries matched expected counts and token totals; non-usage
append counts, lifecycle, protocol and exact output assertions passed in the
CLI suite.

## Reproduction

Use two clean source checkouts at the recorded revisions and temporary CLI
copies. For example, with local paths set to those checkouts:

```sh
git -C /path/to/komposyn checkout --detach 0943bc8d3ef63eef98e907c856211af6d93424a7
git -C /path/to/komposyn checkout --detach 36fea986b18d2c5ad32f4876b50412a03f9d933f
git -C /path/to/komposyn-lg checkout --detach 265bcd375cbd626232998ec9a3e3793ec09492f9
cp -a /path/to/komposyn-lg /tmp/cli-before
cp -a /path/to/komposyn-lg /tmp/cli-candidate
go -C /tmp/cli-before mod edit -replace=github.com/ayeshLK/komposyn=/path/to/core-before
go -C /tmp/cli-candidate mod edit -replace=github.com/ayeshLK/komposyn=/path/to/core-candidate
go -C /tmp/cli-before list -m -f '{{.Replace.Dir}}' github.com/ayeshLK/komposyn
go -C /tmp/cli-candidate list -m -f '{{.Replace.Dir}}' github.com/ayeshLK/komposyn
```

Confirm each module path is the intended checkout, then build both with the
same command and output filenames:

```sh
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w -X github.com/ayeshLK/komposyn/plugins/library/token-logger.Version=0.7.0 -X main.cliVersion=0.8.0' -o /tmp/komposyn-lg-before ./plugins/cli/token-logger
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w -X github.com/ayeshLK/komposyn/plugins/library/token-logger.Version=0.7.0 -X main.cliVersion=0.8.0' -o /tmp/komposyn-lg-candidate ./plugins/cli/token-logger
```

Run the matched matrix with the committed harness; detailed fixture and
sampling flags are recorded in each raw `manifest.json` and in
[`resource-measurements.md`](contributing/resource-measurements.md). Do not run
release checks with a temporary replacement: the provenance guard is intended
to reject it. Run compatibility tests with that exact replacement, and run
`make check` separately against the normal published dependency pin.

## Recommendation

The CLI confirms substantially fewer source bytes flowing through read calls
for steady-state polling and append-heavy histories, with no accounting,
lifecycle, protocol, or exact-output regression found. The measured CLI data
does not support claiming a general CPU, latency, or RSS improvement. This
phase's evidence is suitable for review and delivery as a measurement report;
final dependency integration remains gated on a reviewed, published core
version. No CLI dependency bump or release is part of this work. Issue #23 and
coordination issue #20 remain open.
