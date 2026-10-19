# Issue #23: CLI measurements for core #138

This report measures the incremental effect of core #138 in the standalone
CLI. The before source is core `36fea986b18d2c5ad32f4876b50412a03f9d933f`;
the candidate is published core `v0.7.1`, tag commit
`4519551527b28461b250bfdbc7f9f66473a6a219`. The comparison does not attribute
the earlier core #137 change and does not establish a general latency or memory
improvement. It is evidence for [issue #23](https://github.com/ayeshLK/komposyn-lg/issues/23),
coordinated under [issue #20](https://github.com/ayeshLK/komposyn-lg/issues/20).

## Provenance and reproduction

Both executables use CLI Go source commit
`265bcd375cbd626232998ec9a3e3793ec09492f9`, the same source as the previous
baseline. The #26 merge commit `83da1318ebaf776df044cfb14fe6b5cc74300fd0`
adds no changes to Go files, `go.mod`, or `go.sum`. The corrected measurement
driver is from `4f009bef1d3973baf404c7fa1cc5cece6451b8d3`; its SHA-256 is
`e2a53f22d9cd21c11d538bcaa3c2eca159a3488a99de5a68e487e90b42930904` on both
CLI trees.

| Side | CLI source | Core source and resolution | Runtime version | Binary SHA-256 |
| --- | --- | --- | --- | --- |
| Before | `265bcd375cbd626232998ec9a3e3793ec09492f9` | `36fea986b18d2c5ad32f4876b50412a03f9d933f`, resolved from `/tmp/issue23-core-candidate` | `komposyn-lg 0.8.0 (core 0.7.0)` | `d273ef48efc54451d2cf9db4f4903ce901fe4044ab7a822b755c5d94b6e015ac` |
| Candidate | `265bcd375cbd626232998ec9a3e3793ec09492f9` | tag `v0.7.1`, commit `4519551527b28461b250bfdbc7f9f66473a6a219`, resolved from `/tmp/komposyn-core-v071-138` | `komposyn-lg 0.8.0 (core 0.7.1)` | `45600fb987b4c9b185fcde6ce653f202343a3ba0a7c30b08394863a6646cc534` |

Both used Go `1.26.9`, Linux `amd64`, `CGO_ENABLED=0`, `-trimpath`, and
`-ldflags '-s -w -X github.com/ayeshLK/komposyn/plugins/library/token-logger.Version=<core-version> -X main.cliVersion=0.8.0'`.
The only build-flag value difference is the embedded core version. `go list -m`
confirmed each temporary local replacement directory, `git rev-parse` confirmed
its core commit, and `go version -m` recorded the build metadata. The before
binary and provenance were reused from the prior #137 comparison; fresh paired
cold and steady-state runs were also captured here. No replacement was added
to the repository module files.

The published module download resolves `v0.7.1` from
`refs/tags/v0.7.1` at `4519551527b28461b250bfdbc7f9f66473a6a219`, with module
checksum `h1:a6E0yyAmAaRmrWhLW+IOzWu5bA/2raYqVsrNn+vXtaE=` and go.mod checksum
`h1:Qgzn20ktpGuwlEOsGCn3wGBjqQ2AZ3Rxa6LgZDn7wxI=`.

The core worktrees and CLI copies were disposable. The harness is checked out
separately at the recorded commit and checksum, then passed to both CLI binaries.
This keeps its exact-usage drain fix independent of the older CLI source tree.
Reproduction uses temporary local module replacements:

```sh
git -C /path/to/komposyn worktree add --detach /tmp/core-before 36fea986b18d2c5ad32f4876b50412a03f9d933f
git -C /path/to/komposyn worktree add --detach /tmp/core-v071 4519551527b28461b250bfdbc7f9f66473a6a219
git -C /path/to/komposyn-lg worktree add --detach /tmp/cli-before 265bcd375cbd626232998ec9a3e3793ec09492f9
git -C /path/to/komposyn-lg worktree add --detach /tmp/cli-candidate 265bcd375cbd626232998ec9a3e3793ec09492f9
git -C /path/to/komposyn-lg show 4f009bef1d3973baf404c7fa1cc5cece6451b8d3:scripts/measure-collector.py > /tmp/measure-collector.py
echo 'e2a53f22d9cd21c11d538bcaa3c2eca159a3488a99de5a68e487e90b42930904  /tmp/measure-collector.py' | sha256sum --check
go -C /tmp/cli-before mod edit -replace=github.com/ayeshLK/komposyn=/tmp/core-before
go -C /tmp/cli-candidate mod edit -replace=github.com/ayeshLK/komposyn=/tmp/core-v071
go -C /tmp/cli-before list -m -f '{{.Replace.Dir}}' github.com/ayeshLK/komposyn
go -C /tmp/cli-candidate list -m -f '{{.Replace.Dir}}' github.com/ayeshLK/komposyn
CGO_ENABLED=0 go -C /tmp/cli-before build -trimpath -ldflags '-s -w -X github.com/ayeshLK/komposyn/plugins/library/token-logger.Version=0.7.0 -X main.cliVersion=0.8.0' -o /tmp/komposyn-lg-before ./plugins/cli/token-logger
CGO_ENABLED=0 go -C /tmp/cli-candidate build -trimpath -ldflags '-s -w -X github.com/ayeshLK/komposyn/plugins/library/token-logger.Version=0.7.1 -X main.cliVersion=0.8.0' -o /tmp/komposyn-lg-candidate ./plugins/cli/token-logger
python3 /tmp/measure-collector.py --binary /tmp/komposyn-lg-before --mode foreground --scenario unchanged --profile claude --interval 5s --events 10000 --warmup 15s --duration 120s --artifacts /tmp/measure-before
python3 /tmp/measure-collector.py --binary /tmp/komposyn-lg-candidate --mode foreground --scenario unchanged --profile claude --interval 5s --events 10000 --warmup 15s --duration 120s --artifacts /tmp/measure-candidate
```

The complete matrix adds Codex; cold backfill with four files and no warmup;
two repetitions each of usage and non-usage append for both providers; 100-file
and 1,000-record/16-KiB-padding cases; one-second stress and detached runs;
and a ten-minute Claude usage soak. Append cases start with 10,000 events and
append every five seconds. The standard window is 120 seconds at the default
five-second interval. Unchanged histories use 15 seconds warmup; append,
many-file, and large-record histories use 10 seconds. Stress runs use 60
seconds after 10 seconds warmup; detached runs use the default interval for 60
seconds after 10 seconds warmup; the soak uses 600 seconds after 10 seconds
warmup. Cold timing starts before launch and ends after the first successful
poll. The harness keeps measurement, append-drain, and shutdown phases
separate.

The host was Ubuntu 24.04.4, kernel `7.0.0-38-generic`, Intel Core i7-10510U
(4 cores / 8 logical CPUs), 15.4 GiB RAM, `powersave` governor, ext4 `/tmp`,
and Go `1.26.9`. All runs used this host and were serialized in alternating
before/candidate order. Background system activity was not isolated. Raw
manifests, logs, and `/proc` samples remain outside the repository under
`/tmp/issue23-core138` on the measurement host. The matrix runner SHA-256 is
`c22da2a4839ebce0f18b5d79c8f8291eaf235ae7c3e8b3d0a10787053b20f43a`.

## Results

All 54 captured runs passed the harness. This includes the 50-run matrix and
one additional cold repeat for each provider and side after the first cold
pair showed wide host/run variation. Exact initial counts, appended counts,
usage totals, and non-usage outcome counts were audited from the manifests and
JSON output. All foreground PIDs and temporary workspaces were gone after each
run; both detached modes confirmed cooperative stop and cleanup.

CPU is process user plus system time during the measured window (for cold runs,
from the first `/proc` sample until the first successful poll). Mean RSS is the
average sampled resident set during that window. HWM is the kernel process
high-water RSS and includes startup and warmup. I/O values are window deltas;
`rchar` and `wchar` count bytes passed through read/write-like calls, while
`read_bytes` and `write_bytes` are kernel-attributed storage traffic. These do
not measure Go allocations or retained heap.

Each cell shows before → candidate. Ranges include repeated runs; single-run
rows are diagnostic only.

| Workload | CPU seconds | Mean RSS MiB | HWM MiB | `rchar` MiB | `read_bytes` MiB | `write_bytes` MiB | `wchar` MiB |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Cold Claude, 10k / 4 files (2 runs) | 1.66–6.23 → 1.63–6.50 | 16.04–16.69 → 15.94–17.07 | 23.70–24.01 → 22.11–23.57 | 40.36–46.30 → 43.28–46.43 | <0.01 → <0.01 | 33.07–33.11 → 32.97–33.02 | 36.15–36.34 → 36.15–36.28 |
| Cold Codex, 10k / 4 files (2 runs) | 1.79–5.11 → 1.88–5.11 | 16.54–16.58 → 15.99–17.14 | 24.06–24.42 → 23.06–23.50 | 39.37–42.62 → 42.21–42.60 | <0.01 → <0.01 | 32.84–32.97 → 32.86–33.00 | 35.83–36.16 → 35.87–36.14 |
| Unchanged Claude, 5s, 120s (3 runs) | 0.44–1.56 → 0.34–1.20 | 24.80–28.28 → 33.38–34.29 | 42.25–50.12 → 37.76–39.30 | 75.97 → 75.97 | 0.00 | 1.54–1.55 → 1.55 | 0.76 → 0.76 |
| Unchanged Codex, 5s, 120s (3 runs) | 0.39–0.44 → 0.36–0.39 | 24.49–25.83 → 33.59–34.56 | 44.01–44.90 → 38.14–44.57 | 68.48 → 68.48 | 0.00 | 1.55 → 1.55 | 0.76 → 0.76 |
| Usage append Claude, 5s, 120s (2 runs; 23 appends) | 22.39–24.39 → 22.34–23.00 | 35.80–35.88 → 35.86–36.15 | 53.25–55.14 → 46.49–47.35 | 60.61–60.63 → 123.98–136.64 | 0.00 | 2.62–2.64 → 2.68–2.70 | 1.81–1.85 → 1.86–1.88 |
| Usage append Codex, 5s, 120s (2 runs; 23 appends) | 22.47–24.33 → 25.11–27.38 | 34.83–35.19 → 34.69–35.73 | 51.92–54.23 → 48.62–51.68 | 54.66–57.53 → 106.07–109.88 | 0.00 | 2.59–2.69 → 2.61–2.63 | 1.79–1.87 → 1.81 |
| Non-usage append Claude, 5s, 120s (2 runs; 23 appends) | 23.56–25.65 → 23.50–30.23 | 35.66–35.75 → 34.96–35.29 | 55.21–55.40 → 48.70–49.16 | 60.39–60.40 → 123.76–130.03 | 0.00 | 1.74–1.75 → 1.68–1.70 | 0.95–0.96 → 0.92–0.93 |
| Non-usage append Codex, 5s, 120s (2 runs; 23 appends) | 25.55–28.42 → 28.39–31.46 | 35.00–35.02 → 35.16–35.20 | 54.23–54.84 → 49.35–50.61 | 54.47 → 111.59–134.41 | 0.00 | 1.71–1.72 → 1.70 | 0.93–0.95 → 0.93 |
| Many files Claude, 100 files / 10k events, 5s, 120s (1 run) | 1.40 → 1.43 | 19.73 → 19.76 | 20.47 → 20.05 | 73.42 → 75.54 | 0.00 | 1.52 → 3.66 | 0.74 → 2.87 |
| Many files Codex, 100 files / 10k events, 5s, 120s (1 run) | 1.05 → 0.96 | 20.29 → 20.01 | 20.61 → 20.09 | 66.67 → 66.69 | 0.00 | 3.61 → 3.64 | 2.81 → 2.84 |
| Large records Claude, 1k × 16 KiB padding, 5s, 120s (1 run) | 1.72 → 1.31 | 49.97 → 18.24 | 59.80 → 18.39 | 367.14 → 367.14 | 0.00 | 1.51 → 1.50 | 0.74 → 0.74 |
| Large records Codex, 1k × 16 KiB padding, 5s, 120s (1 run) | 1.61 → 1.32 | 50.26 → 18.50 | 53.17 → 18.63 | 366.34 → 366.34 | 0.00 | 1.51 → 1.51 | 0.74 → 0.74 |
| **Stress:** Claude unchanged, 1s, 60s (1 run) | 1.01 → 0.75 | 23.77 → 30.95 | 45.17 → 44.80 | 186.75 → 186.75 | 0.00 | 1.89 → 1.89 | 0.94 → 0.94 |
| **Stress:** Codex unchanged, 1s, 60s (1 run) | 0.99 → 0.74 | 26.18 → 33.86 | 42.45 → 38.22 | 168.36 → 168.36 | 0.00 | 1.89 → 1.89 | 0.94 → 0.94 |
| Detached Claude unchanged, 5s, 60s (1 run) | 0.20 → 0.17 | 25.64 → 32.86 | 45.31 → 39.49 | 37.98 → 37.98 | 0.00 | 0.77 → 0.77 | 0.37 → 0.38 |
| Detached Codex unchanged, 5s, 60s (1 run) | 0.19 → 0.18 | 26.75 → 35.48 | 46.03 → 37.24 | 34.24 → 34.24 | 0.00 | 0.77 → 0.77 | 0.38 → 0.37 |
| Claude usage soak, 5s, 600s (1 run; 119 appends) | 121.32 → 136.03 | 36.87 → 35.73 | 55.80 → 48.75 | 310.70 → 629.33 | 0.00 | 14.77 → 14.84 | 10.62 → 10.78 |

### Findings

The unchanged histories had the same `rchar` on both sides. In both append
scenarios and the soak, candidate `rchar` was about twice the before value,
consistent with #138's intentional second read when an input changes. The
soak's CPU was 121.32 seconds before and 136.03 after; this is one run per side
and does not establish a general latency regression or improvement. Physical
`read_bytes` rounded to zero or less than 0.01 MiB across these cached-fixture
runs, so these results do not show a physical disk-read difference.

Mean sampled RSS was consistently about 8–10 MiB higher for the candidate in
the repeated unchanged Claude and Codex cases, and was also higher in both
detached and both 1-second stress runs. Candidate HWM was lower in most of
those rows. Large-record cases showed substantially lower sampled RSS and HWM
on the candidate. These are process RSS observations, not Go heap allocation
or retained-heap measurements; no heap profile was captured, and the soak
does not establish or rule out a memory leak. The 100-file Claude run had
higher candidate `write_bytes` and `wchar` than its before run, but each side
has one observation and the other provider did not show the same pattern; this
I/O result is inconclusive.

Cold CPU varied widely in the repeated runs on both sides. One paired capture
was around 5–6.5 CPU seconds, while the second was around 1.6–1.9 seconds; the
second pair matched the earlier #137 capture more closely. The range overlaps
between core versions, so cold CPU is inconclusive on this host. No broad
latency or memory claim is warranted.

## Compatibility and validation

Review of core `36fea…` to tag `451955…` found no exported API change, no
module dependency change, and no schema migration change. The implementation
reworks internal source acquisition and provider parsing. The core compatibility
policy and `plugin_version`/`cli_version` meanings remain intact. Core tag CI
passed Linux, macOS, Windows, race, standalone CLI compatibility, collector
baseline compatibility, and analysis checks in
[run 39185068851](https://github.com/ayeshLK/komposyn/actions/runs/39185068851).

The dedicated CLI branch pins the published module at `v0.7.1` with no
permanent `replace`; the minimum supported core floor is documented as
`v0.7.1`, equal to the pin, so no duplicate floor-version CI matrix is needed.
The release builder, current runtime assertions, Windows and Unix candidate
upgrade verifiers, and protocol smoke fixture now expect core `0.7.1`. Historical
v0.7.0 release and upgrade records remain historical.

These exact commands passed on the integration branch:

```text
go mod verify                         all modules verified
go vet ./...                          passed
go test ./...                         passed
go test -race ./...                   passed
make check                            passed
python3 scripts/test-protocol-smoke.py passed (2 tests)
bash scripts/test-release-assets.sh   passed
python3 scripts/test-measure-collector.py passed (10 tests, 2 skipped)
```

An unsigned candidate archive was built outside the repository with
`bash scripts/build-release.sh v0.8.0 /tmp/issue23-core138-candidate-release`.
The six archives passed checksum verification; only the native Linux archive
was run. `bash scripts/verify-release.sh /tmp/issue23-core138-candidate-release
/tmp/komposyn-lg-issue23-candidate linux_amd64` passed the local fresh-data,
core-v0.7.0-reference upgrade, refusal, protocol-v1, and data-preserving
uninstall checks. The extracted candidate reported
`komposyn-lg 0.8.0 (core 0.7.1)`; its direct protocol smoke returned
`cli_version: 0.8.0` and `plugin_version: 0.7.1` with valid protocol-v1 JSONL.

The CLI tests cover embedded, positional foreground, registry foreground,
restart, dynamic roots, lease/heartbeat, and cooperative stop behavior. The
native Linux candidate verifier exercised fresh data, same-schema upgrade
preservation, and newer-schema refusal without database mutation or a spurious
backup. Since v0.7.0 to v0.7.1 does not advance the schema, that local run did
not exercise migration-backup creation and restoration. PR #27's CI passed all
checks, including candidate builds and published historical upgrade fixtures
on six native targets, with the schema-advancing v0.6.0 fixture included
([workflow run](https://github.com/ayeshLK/komposyn-lg/actions/runs/39198383857)).
Those checks use unsigned candidate archives and do not substitute for the
signed six-target artifact and signed-candidate upgrade evidence required by a
future standalone release. No release was published, no tag was created or
moved, and issues #23 and #20 remain open.

## Recommendation

The v0.7.1 dependency integration and its measured evidence are ready for PR
review. Report the doubled `rchar` on changed inputs and the higher sampled RSS
on small unchanged histories prominently; this phase does not support a broad
performance-improvement claim. Release readiness remains gated on the signed
six-target and active signed-upgrade checks in the protected release workflow.
