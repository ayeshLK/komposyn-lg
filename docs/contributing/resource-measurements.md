# Collector resource measurements

This developer-only harness measures the standalone `komposyn-lg` executable
against generated Codex or Claude JSONL histories. It uses an isolated source
tree and an isolated, disposable database for every run. It adds no CLI command,
telemetry, profile listener, or performance threshold. Generated input contains
synthetic content only; the tool never reads your real provider histories.

## Requirements and build selection

Use Python 3.10 or newer and a built standalone executable. Build from the revision being
measured, or point `--binary` at a separately built candidate. Always record the
binary path and the output of `komposyn-lg version`, which reports both the CLI
version and pinned core version.

```sh
go build -o /tmp/komposyn-lg-measure ./plugins/cli/token-logger
python3 scripts/measure-collector.py --binary /tmp/komposyn-lg-measure --scenario unchanged --duration 120s --warmup 15s --artifacts /tmp/measure-unchanged
```

The harness writes a manifest, time-series CSV, and command logs to the chosen
artifact directory. It creates its source and database in a fresh temporary
directory and removes them on success, failure, timeout, or Ctrl-C. Use
`--keep-workdir` only when you need to inspect the synthetic fixture or isolated
database. Do not commit measurement artifacts or raw profiles.

## Scenarios

Run each workload on the same host, with the same binary build settings and
interval, when comparing revisions. Start with the default five-second polling
interval. Short intervals are stress runs and must be labeled as such.

```sh
# Cold import / backfill of 10,000 Claude events
python3 scripts/measure-collector.py --binary /tmp/komposyn-lg-measure --scenario cold --profile claude --events 10000 --duration 60s --interval 5s --artifacts /tmp/measure-cold

# Warm, unchanged history
python3 scripts/measure-collector.py --binary /tmp/komposyn-lg-measure --scenario unchanged --profile codex --events 10000 --warmup 15s --duration 120s --artifacts /tmp/measure-unchanged

# Append usage events or non-usage records during measurement
python3 scripts/measure-collector.py --binary /tmp/komposyn-lg-measure --scenario usage-append --profile claude --events 10000 --append-every 5s --duration 120s --artifacts /tmp/measure-usage-append
python3 scripts/measure-collector.py --binary /tmp/komposyn-lg-measure --scenario non-usage-append --profile codex --events 10000 --append-every 5s --duration 120s --artifacts /tmp/measure-non-usage-append

# Many files and larger records
python3 scripts/measure-collector.py --binary /tmp/komposyn-lg-measure --scenario unchanged --profile claude --events 10000 --files 100 --duration 120s --artifacts /tmp/measure-many-files
python3 scripts/measure-collector.py --binary /tmp/komposyn-lg-measure --scenario unchanged --profile claude --events 1000 --record-padding-bytes 16384 --duration 120s --artifacts /tmp/measure-large-records

# Explicit opt-in active soak. This appends a usage record at each cadence.
python3 scripts/measure-collector.py --binary /tmp/komposyn-lg-measure --scenario soak --profile claude --events 10000 --append-every 5s --duration 10m --artifacts /tmp/measure-soak
```

`--events`, `--files`, and `--record-padding-bytes` control generated workload
size. `--warmup` is excluded from measured CSV samples. The `cold` scenario
starts collection against an empty database and includes process start and
backfill in its measurement. Append and soak scenarios modify a generated file
while the collector is running. They are diagnostic workloads, not claims about
typical user activity.

The `--mode foreground` default measures the foreground collector PID directly.
Use `--mode detached` to start the managed background collector. The harness
reads `watch status --format json` and samples the PID reported by the collector,
not the short-lived command used to launch or inspect it. Both modes request a
cooperative stop and escalate cleanup only if the foreground process does not
exit. The detached collector is stopped in a `finally` cleanup path, including
on Ctrl-C and errors.

## Reading Linux measurements

On Linux, `samples.csv` samples `/proc/<pid>/stat`, `status`, and `io` twice per
second. `cpu_percent_one_core` is process CPU time gained over the sampling
window divided by wall time: 100% means one logical CPU fully occupied. It is
not normalized by the host's logical CPU count. `user_cpu_seconds` and
`system_cpu_seconds` are process CPU time gained since the measurement window
began; warmup is excluded from these columns. `rss_mib` is resident
physical memory at that sample. `peak_rss_mib` is the kernel's process high-water
RSS since process start. `read_bytes` and `written_bytes` are storage I/O bytes
attributed by the kernel; `rchar_bytes` and `wchar_bytes` count bytes passed
through read/write calls and can include cache hits. The latter are not disk
traffic.

RSS and peak RSS differ from Go heap allocations and retained heap. The harness
does not collect allocation profiles, garbage-collector heap statistics, or
virtual address space. A sampled RSS series can miss short peaks; `VmHWM` is a
kernel high-water mark but includes startup and warmup. Use a separate explicit
profile run if heap attribution is needed, and keep profile files private and
outside the repository.

Review the manifest with each result. It records the selected executable and
CLI/core versions, Go build information when available, OS and architecture,
CPU model, RAM, logical CPU count, fixture type/count/bytes, appended records,
mode, scenario, interval, warmup, and measurement duration. Also note power
mode, filesystem, build flags, and competing workload when these are not
reported by the binary. The same machine and conditions matter more than a
single absolute number. The reported five-second diagnostic baseline is not a
budget and does not establish a leak.

## Detached collector and embedded REPL

The detached mode is automated. If inspecting a manually started collector,
obtain its PID from `komposyn-lg watch status --format json --data-dir <isolated-dir>`
and verify the instance, mode, state, and executable before attaching native
process tools. Never identify a collector by matching a broad process name when
multiple instances may exist.

For embedded collection, use a disposable `TOKEN_LOGGER_DATA_DIR`, launch the
normal interactive CLI, opt into collection only for this local run, and start
the embedded collector through the interface. The embedded collector shares the
REPL process PID and its consent behavior. Read that PID from the isolated
directory's `watch status` output while the REPL is open, then use the platform
tools below. Stop collection cooperatively in the REPL or with
`komposyn-lg watch stop --data-dir <isolated-dir>` before closing it. The harness
does not automate the interactive consent flow.

## macOS and Windows

The Python harness creates fixtures and runs foreground or detached workloads
on these systems, but it does not claim cross-platform metrics. For each run,
use native tools against the reported foreground PID or detached PID from
`watch status`, at the same sample interval (for example, one second), and save
the unedited export beside the manifest:

- **macOS:** Activity Monitor or `/usr/bin/time -l` can report process CPU and
  maximum resident set size. For a time series, use Instruments Activity
  Monitor on the exact PID. Record bytes read/written from Instruments or
  `iostat` as system-level context; they are not process-attributed unless the
  selected tool explicitly says so.
- **Windows:** Task Manager or Performance Monitor can sample CPU and Working
  Set for the exact PID. Process Explorer can report I/O read/write bytes and
  peak working set. Use `Get-Process -Id <pid>` for a point-in-time check; do
  not treat `WorkingSet64` as peak RSS.

Platform counters have different definitions. Report the tool, counter names,
units, sample interval, and whether I/O is process-attributed. Do not compare
macOS/Windows working set or private bytes directly with Linux RSS, and do not
present Linux `/proc` values as cross-platform evidence.
