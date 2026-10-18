#!/usr/bin/env python3
"""Measure a standalone KompoSyn collector against generated local fixtures.

Linux process metrics come from /proc. Other platforms can use this script to
generate fixtures and run workloads, but must collect native process metrics
as described in docs/contributing/resource-measurements.md.
"""

from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import platform
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from pathlib import Path


def parse_duration(value: str) -> float:
    suffixes = {"s": 1, "m": 60, "h": 3600}
    try:
        return float(value[:-1]) * suffixes[value[-1].lower()]
    except (ValueError, KeyError, IndexError) as exc:
        raise argparse.ArgumentTypeError("duration must use s, m, or h (for example 120s)") from exc


def event_line(profile: str, index: int, *, usage: bool = True) -> bytes:
    stamp = (dt.datetime(2026, 1, 1, tzinfo=dt.timezone.utc) + dt.timedelta(seconds=index)).isoformat().replace("+00:00", "Z")
    if profile == "claude":
        if not usage:
            row = {"type": "summary", "sessionId": "measurement-session", "summary": "synthetic non-usage append"}
            return (json.dumps(row, separators=(",", ":")) + "\n").encode()
        row = {"type": "assistant", "uuid": f"measure-{index}", "sessionId": "measurement-session",
               "timestamp": stamp, "cwd": "/synthetic/measurement", "message": {"role": "assistant",
               "model": "synthetic-model", "content": "synthetic"}}
        row["message"]["usage"] = {"input_tokens": 100, "output_tokens": 20,
            "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
    else:
        row = {"type": "event_msg", "id": f"measure-{index}", "timestamp": stamp,
               "payload": {"type": "token_count", "session_id": "measurement-session",
               "info": {"total_token_usage": {"input_tokens": 100, "cached_input_tokens": 10,
                         "output_tokens": 20, "reasoning_output_tokens": 0, "total_tokens": 120}}}}
        if not usage:
            row = {"type": "turn_context", "timestamp": stamp,
                   "payload": {"type": "turn_context", "session_id": "measurement-session", "model": "synthetic-model"}}
    return (json.dumps(row, separators=(",", ":")) + "\n").encode()


def header_line(profile: str) -> bytes:
    if profile == "claude":
        row = {"type": "user", "sessionId": "measurement-session", "timestamp": "2026-01-01T00:00:00Z",
               "cwd": "/synthetic/measurement", "project_id": "measurement-project", "project_name": "Synthetic Measurement"}
    else:
        row = {"type": "session_meta", "timestamp": "2026-01-01T00:00:00Z", "payload": {
            "id": "measurement-session", "project_id": "measurement-project",
            "project_name": "Synthetic Measurement", "cwd": "/synthetic/measurement", "model": "synthetic-model"}}
    return (json.dumps(row, separators=(",", ":")) + "\n").encode()


def generate(root: Path, profile: str, count: int, files: int, padding: int) -> list[Path]:
    root.mkdir(parents=True, exist_ok=True)
    paths = []
    per_file = (count + files - 1) // files
    index = 0
    pad = "x" * padding
    for file_number in range(files):
        path = root / f"synthetic-{file_number:05d}.jsonl"
        paths.append(path)
        with path.open("wb") as target:
            target.write(header_line(profile))
            for _ in range(min(per_file, count - index)):
                line = event_line(profile, index)
                if padding:
                    record = json.loads(line)
                    record["synthetic_padding"] = pad
                    line = (json.dumps(record, separators=(",", ":")) + "\n").encode()
                target.write(line)
                index += 1
    return paths


def linux_metrics(pid: int) -> dict[str, int] | None:
    proc = Path("/proc") / str(pid)
    try:
        fields = (proc / "stat").read_text().split()
        status = {}
        for line in (proc / "status").read_text().splitlines():
            if ":" in line:
                key, value = line.split(":", 1)
                status[key] = value.strip()
        io = {}
        for line in (proc / "io").read_text().splitlines():
            key, value = line.split(":", 1)
            io[key] = int(value.strip())
        return {
            "user_ticks": int(fields[13]), "system_ticks": int(fields[14]),
            "rss_kib": int(status.get("VmRSS", "0 kB").split()[0]),
            "peak_rss_kib": int(status.get("VmHWM", "0 kB").split()[0]),
            "read_bytes": io.get("read_bytes", 0), "write_bytes": io.get("write_bytes", 0),
            "rchar_bytes": io.get("rchar", 0), "wchar_bytes": io.get("wchar", 0),
        }
    except (OSError, ValueError, IndexError):
        return None


def run_checked(args: list[str], log: Path, timeout: float = 30) -> str:
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                            text=True, timeout=timeout, check=False)
    log.write_text(result.stdout)
    if result.returncode:
        raise RuntimeError(f"command failed ({result.returncode}): {args[0]} {args[1:]}\n{result.stdout[-2000:]}")
    return result.stdout


def interrupt_foreground(process: subprocess.Popen[str]) -> None:
    if os.name == "nt":
        process.send_signal(signal.CTRL_BREAK_EVENT)
    else:
        process.send_signal(signal.SIGINT)


def machine_metadata() -> dict[str, object]:
    result: dict[str, object] = {"cpu_model": platform.processor() or None, "total_memory_bytes": None}
    if sys.platform.startswith("linux"):
        try:
            for line in Path("/proc/cpuinfo").read_text().splitlines():
                if line.lower().startswith("model name"):
                    result["cpu_model"] = line.split(":", 1)[1].strip()
                    break
            for line in Path("/proc/meminfo").read_text().splitlines():
                if line.startswith("MemTotal:"):
                    result["total_memory_bytes"] = int(line.split()[1]) * 1024
                    break
        except (OSError, ValueError, IndexError):
            pass
    return result


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path, help="explicit standalone executable to measure")
    parser.add_argument("--profile", choices=("claude", "codex"), default="claude")
    parser.add_argument("--mode", choices=("foreground", "detached"), default="foreground")
    parser.add_argument("--scenario", choices=("cold", "unchanged", "usage-append", "non-usage-append", "soak"), default="unchanged")
    parser.add_argument("--duration", type=parse_duration, default=60.0, help="measurement duration; default 60s")
    parser.add_argument("--warmup", type=parse_duration, default=10.0, help="unmeasured warmup for non-cold scenarios")
    parser.add_argument("--interval", default="5s", help="collector interval (5s default; shorter values are stress runs)")
    parser.add_argument("--events", type=int, default=10000, help="generated usage records")
    parser.add_argument("--files", type=int, default=1, help="number of generated JSONL files")
    parser.add_argument("--record-padding-bytes", type=int, default=0, help="synthetic extra field size on each record")
    parser.add_argument("--append-every", type=parse_duration, default=5.0, help="append cadence for append scenarios")
    parser.add_argument("--artifacts", type=Path, help="output directory for manifest, samples, and logs")
    parser.add_argument("--keep-workdir", action="store_true", help="retain generated source and disposable database")
    args = parser.parse_args()

    binary = args.binary.expanduser().resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        parser.error("--binary must name an executable file")
    if args.duration <= 0 or args.warmup < 0 or args.append_every <= 0 or min(args.events, args.files) < 1 or args.record_padding_bytes < 0:
        parser.error("duration, counts, and append cadence must be positive; warmup and record padding cannot be negative")

    artifacts = args.artifacts.expanduser().resolve() if args.artifacts else Path.cwd() / ("collector-measurement-" + dt.datetime.now().strftime("%Y%m%d-%H%M%S"))
    artifacts.mkdir(parents=True, exist_ok=False)
    workspace = Path(tempfile.mkdtemp(prefix="komposyn-lg-measure-"))
    source = workspace / "source"
    data = workspace / "data"
    data.mkdir()
    logs = artifacts / "logs"
    logs.mkdir()
    process = None
    detached_started = False
    try:
        paths = generate(source, args.profile, args.events, args.files, args.record_padding_bytes)
        fixture_bytes = sum(path.stat().st_size for path in paths)
        version = run_checked([str(binary), "version"], logs / "version.txt").strip()
        try:
            build_info = subprocess.run(["go", "version", "-m", str(binary)], capture_output=True,
                                        text=True, timeout=10, check=False).stdout.strip()
        except (OSError, subprocess.TimeoutExpired):
            build_info = "unavailable"
        run_checked([str(binary), "watch", "add", str(source), "--data-dir", str(data)], logs / "watch-add.txt")
        manifest = {
            "binary": str(binary), "version": version, "profile": args.profile, "scenario": args.scenario,
            "mode": args.mode, "interval": args.interval, "duration_seconds": args.duration,
            "warmup_seconds": args.warmup if args.scenario != "cold" else 0,
            "generated_files": len(paths), "usage_records": args.events, "fixture_bytes": fixture_bytes,
            "record_padding_bytes": args.record_padding_bytes, "append_every_seconds": args.append_every,
            "build_info": build_info, "machine_metadata": machine_metadata(),
            "source_path": str(source), "data_dir": str(data),
            "platform": platform.platform(), "machine": platform.machine(), "logical_cpus": os.cpu_count(),
            "run_prepared_utc": dt.datetime.now(dt.timezone.utc).isoformat(),
            "metrics_source": "/proc/<pid>/{stat,status,io}" if sys.platform.startswith("linux") else "native platform collector required",
        }
        (artifacts / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")

        def start() -> int:
            nonlocal process, detached_started
            if args.mode == "foreground":
                cmd = [str(binary), "watch", "foreground", "--interval", args.interval,
                       "--data-dir", str(data), str(source)]
                popen_args = {"creationflags": subprocess.CREATE_NEW_PROCESS_GROUP} if os.name == "nt" else {"start_new_session": True}
                with (logs / "collector-stdout.txt").open("w") as out, (logs / "collector-stderr.txt").open("w") as err:
                    process = subprocess.Popen(cmd, stdout=out, stderr=err, **popen_args)
                return process.pid
            detached_started = True
            run_checked([str(binary), "watch", "start", "--interval", args.interval, "--data-dir", str(data)], logs / "watch-start.txt")
            status = run_checked([str(binary), "watch", "status", "--format", "json", "--data-dir", str(data)], logs / "watch-status-start.json")
            return int(json.loads(status)["pid"])

        pid = start()
        manifest["collector_pid"] = pid
        manifest["measurement_started_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
        (artifacts / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        if args.scenario != "cold":
            time.sleep(args.warmup)
        measure_start = time.monotonic()
        previous = linux_metrics(pid) if sys.platform.startswith("linux") else None
        initial_user_ticks = previous["user_ticks"] if previous is not None else 0
        initial_system_ticks = previous["system_ticks"] if previous is not None else 0
        previous_sample_time = measure_start
        append_count = 0
        append_at = measure_start + args.append_every
        end = measure_start + args.duration
        with (artifacts / "samples.csv").open("w") as sample_file:
            sample_file.write("elapsed_seconds,cpu_percent_one_core,user_cpu_seconds,system_cpu_seconds,rss_mib,peak_rss_mib,read_bytes,written_bytes,rchar_bytes,wchar_bytes\n")
            while time.monotonic() < end:
                now = time.monotonic()
                if args.scenario in ("usage-append", "non-usage-append", "soak") and now >= append_at:
                    path = paths[0]
                    append_count += 1
                    line = event_line(args.profile, args.events + append_count, usage=args.scenario != "non-usage-append")
                    with path.open("ab") as target:
                        target.write(line)
                    append_at += args.append_every
                time.sleep(0.5)
                current = linux_metrics(pid) if sys.platform.startswith("linux") else None
                if current is not None:
                    elapsed = time.monotonic() - measure_start
                    ticks = os.sysconf("SC_CLK_TCK")
                    delta_cpu = 0 if previous is None else ((current["user_ticks"] - previous["user_ticks"]) + (current["system_ticks"] - previous["system_ticks"])) / ticks
                    sample_time = time.monotonic()
                    delta_time = max(0.001, sample_time - previous_sample_time)
                    cpu_percent = delta_cpu / delta_time * 100
                    measured_user = (current["user_ticks"] - initial_user_ticks) / ticks
                    measured_system = (current["system_ticks"] - initial_system_ticks) / ticks
                    sample_file.write(f"{elapsed:.3f},{cpu_percent:.3f},{measured_user:.3f},{measured_system:.3f},{current['rss_kib']/1024:.3f},{current['peak_rss_kib']/1024:.3f},{current['read_bytes']},{current['write_bytes']},{current['rchar_bytes']},{current['wchar_bytes']}\n")
                    sample_file.flush()
                    previous = current
                    previous_sample_time = sample_time
                elif process is not None and process.poll() is not None:
                    raise RuntimeError(f"foreground collector exited unexpectedly ({process.returncode})")

        if args.mode == "detached":
            run_checked([str(binary), "watch", "stop", "--timeout", "10s", "--data-dir", str(data)], logs / "watch-stop.txt")
            detached_started = False
        elif process is not None:
            interrupt_foreground(process)
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
                raise RuntimeError("foreground collector did not stop gracefully; escalated cleanup")
        manifest["measurement_finished_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
        manifest["elapsed_seconds"] = round(time.monotonic() - measure_start, 3)
        manifest["appended_records"] = append_count
        manifest["final_fixture_bytes"] = sum(path.stat().st_size for path in paths)
        (artifacts / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        print(f"Artifacts: {artifacts}")
        print(f"Measured {version}; scenario={args.scenario} mode={args.mode} pid={pid} interval={args.interval}")
        if not sys.platform.startswith("linux"):
            print("Native process metrics are unavailable in this script on this platform; follow the guide.")
        return 0
    except (OSError, RuntimeError, subprocess.TimeoutExpired, json.JSONDecodeError) as exc:
        print(f"measurement failed: {exc}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("interrupted; cleaning up collector", file=sys.stderr)
        return 130
    finally:
        if detached_started:
            try:
                subprocess.run([str(binary), "watch", "stop", "--timeout", "10s", "--data-dir", str(data)],
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=15, check=False)
            except (OSError, subprocess.TimeoutExpired):
                pass
        if process is not None and process.poll() is None:
            try:
                interrupt_foreground(process)
                process.wait(timeout=5)
            except (OSError, subprocess.TimeoutExpired):
                process.kill()
                process.wait()
        if not args.keep_workdir:
            shutil.rmtree(workspace, ignore_errors=True)
        else:
            print(f"Generated fixture and disposable database: {workspace}")


if __name__ == "__main__":
    raise SystemExit(main())
