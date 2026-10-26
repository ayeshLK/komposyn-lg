# Issue #56 local logging evidence

This note records a small synthetic resource check of the bounded local JSONL
logger. It is implementation evidence for the CLI changes in issue #56; it is
not a workload benchmark or a release-readiness claim.

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
