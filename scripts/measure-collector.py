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
    for suffix, scale in (("ms", 0.001), ("s", 1), ("m", 60), ("h", 3600)):
        if value.lower().endswith(suffix):
            try:
                return float(value[:-len(suffix)]) * scale
            except ValueError:
                break
    raise argparse.ArgumentTypeError("duration must use ms, s, m, or h (for example 120s)")


def event_line(profile: str, index: int, counter: int, session_id: str, *, usage: bool = True) -> bytes:
    stamp = (dt.datetime(2026, 1, 1, tzinfo=dt.timezone.utc) + dt.timedelta(seconds=index)).isoformat().replace("+00:00", "Z")
    if profile == "claude":
        if not usage:
            row = {"type": "summary", "sessionId": session_id, "summary": "synthetic non-usage append"}
            return (json.dumps(row, separators=(",", ":")) + "\n").encode()
        row = {"type": "assistant", "uuid": f"measure-{index}", "sessionId": session_id,
               "timestamp": stamp, "cwd": "/synthetic/measurement", "message": {"role": "assistant",
               "model": "synthetic-model", "content": "synthetic"}}
        row["message"]["usage"] = {"input_tokens": 100, "output_tokens": 20,
            "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
    else:
        row = {"type": "event_msg", "id": f"measure-{index}", "timestamp": stamp,
               "payload": {"type": "token_count", "session_id": session_id,
               "info": {"total_token_usage": {"input_tokens": 100 * counter, "cached_input_tokens": 10 * counter,
                         "output_tokens": 20 * counter, "reasoning_output_tokens": 0, "total_tokens": 120 * counter}}}}
        if not usage:
            row = {"type": "turn_context", "timestamp": stamp,
                   "payload": {"type": "turn_context", "session_id": session_id, "model": "synthetic-model"}}
    return (json.dumps(row, separators=(",", ":")) + "\n").encode()


def header_line(profile: str, session_id: str) -> bytes:
    if profile == "claude":
        row = {"type": "user", "sessionId": session_id, "timestamp": "2026-01-01T00:00:00Z",
               "cwd": "/synthetic/measurement", "project_id": "measurement-project", "project_name": "Synthetic Measurement"}
    else:
        row = {"type": "session_meta", "timestamp": "2026-01-01T00:00:00Z", "payload": {
            "id": session_id, "project_id": "measurement-project",
            "project_name": "Synthetic Measurement", "cwd": "/synthetic/measurement", "model": "synthetic-model"}}
    return (json.dumps(row, separators=(",", ":")) + "\n").encode()


def generated_counts(count: int, files: int) -> list[int]:
    per_file = (count + files - 1) // files
    remaining = count
    result = []
    for _ in range(files):
        current = min(per_file, remaining)
        result.append(current)
        remaining -= current
    return result


def generate(root: Path, profile: str, count: int, files: int, padding: int) -> list[Path]:
    root.mkdir(parents=True, exist_ok=True)
    paths = []
    index = 0
    pad = "x" * padding
    for file_number, file_count in enumerate(generated_counts(count, files)):
        path = root / f"synthetic-{file_number:05d}.jsonl"
        paths.append(path)
        session_id = f"measurement-session-{file_number}"
        with path.open("wb") as target:
            target.write(header_line(profile, session_id))
            for local_index in range(file_count):
                line = event_line(profile, index, local_index + 1, session_id)
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


def run_json(binary: Path, args: list[str], logs: Path, label: str, timeout: float = 30) -> dict:
    result = subprocess.run([str(binary), *args], capture_output=True, text=True, timeout=timeout, check=False)
    (logs / f"{label}.stdout.json").write_text(result.stdout)
    (logs / f"{label}.stderr.txt").write_text(result.stderr)
    if result.returncode:
        raise RuntimeError(f"command failed ({result.returncode}): {args}\n{result.stderr[-2000:]}")
    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"command returned invalid JSON: {args}\n{result.stdout[-2000:]}") from exc


def expected_usage(profile: str, count: int) -> dict[str, int]:
    return {
        "request_count": count,
        "input_tokens": count * 100,
        "cached_input_tokens": count * (10 if profile == "codex" else 0),
        "cache_write_input_tokens": 0,
        "output_tokens": count * 20,
        "reasoning_output_tokens": 0,
        "total_tokens": count * 120,
    }


def read_usage_summary(binary: Path, data_dir: Path, logs: Path, label: str, timeout: float = 30) -> dict:
    report = run_json(binary, ["usage", "--format", "json", "--from", "1970-01-01T00:00:00Z",
                               "--data-dir", str(data_dir)], logs, label, timeout)
    return report["summary"]


def assert_usage_summary(summary: dict, profile: str, count: int, label: str) -> None:
    expected = expected_usage(profile, count)
    actual = {key: summary.get(key) for key in expected}
    if actual != expected:
        raise RuntimeError(f"{label} usage totals mismatch: expected {expected}, got {actual}")


def validate_generated_fixture(binary: Path, source: Path, profile: str, count: int,
                                paths: list[Path], logs: Path, workspace: Path) -> dict:
    validation_data = workspace / "fixture-validation-data"
    validation_data.mkdir()
    import_result = run_json(binary, ["import", str(source), "--format", "json", "--data-dir", str(validation_data)],
                             logs, "fixture-validation-import")
    if import_result.get("imported_events") != count or import_result.get("complete") is not True:
        raise RuntimeError(f"fixture cold import mismatch: expected {count} events and complete result, got {import_result}")
    assert_usage_summary(read_usage_summary(binary, validation_data, logs, "fixture-validation-usage"), profile,
                         count, "fixture cold import")

    first_path = paths[0]
    original_size = first_path.stat().st_size
    per_file_count = generated_counts(count, len(paths))[0]
    if per_file_count < 1:
        raise RuntimeError("the first generated file must contain an event so usage append sanity can target it")
    with first_path.open("ab") as fixture:
        fixture.write(event_line(profile, count + 1, per_file_count + 1, "measurement-session-0"))
    try:
        append_result = run_json(binary, ["import", str(source), "--format", "json", "--data-dir", str(validation_data)],
                                 logs, "fixture-validation-append")
        if append_result.get("imported_events") != 1 or append_result.get("complete") is not True:
            raise RuntimeError(f"fixture usage append must import exactly one event: got {append_result}")
        assert_usage_summary(read_usage_summary(binary, validation_data, logs, "fixture-validation-append-usage"),
                             profile, count + 1, "fixture usage append")
    finally:
        with first_path.open("r+b") as fixture:
            fixture.truncate(original_size)

    return {"initial_imported_events": count, "initial_usage_totals": expected_usage(profile, count),
            "append_imported_events": 1, "append_usage_totals": expected_usage(profile, count + 1),
            "files_checked": len(paths), "sessions": len(paths)}


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


def process_alive(pid: int) -> bool | None:
    if os.name == "nt":
        return windows_process_alive(pid)
    try:
        os.kill(pid, 0)
        return True
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    except OSError:
        return None


def windows_process_alive(pid: int, kernel32=None) -> bool | None:
    """Query process state without sending a signal or terminating it."""
    import ctypes
    from ctypes import wintypes

    if kernel32 is None:
        kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
        kernel32.OpenProcess.argtypes = (wintypes.DWORD, wintypes.BOOL, wintypes.DWORD)
        kernel32.OpenProcess.restype = wintypes.HANDLE
        kernel32.WaitForSingleObject.argtypes = (wintypes.HANDLE, wintypes.DWORD)
        kernel32.WaitForSingleObject.restype = wintypes.DWORD
        kernel32.CloseHandle.argtypes = (wintypes.HANDLE,)
        kernel32.CloseHandle.restype = wintypes.BOOL

    synchronize = 0x00100000
    query_limited_information = 0x1000
    wait_object_0 = 0
    wait_timeout = 258
    handle = kernel32.OpenProcess(synchronize | query_limited_information, False, pid)
    if not handle:
        error = ctypes.get_last_error()
        if error == 87:  # ERROR_INVALID_PARAMETER: the process ID no longer exists.
            return False
        if error == 5:  # ERROR_ACCESS_DENIED still means a process exists.
            return True
        return None
    try:
        result = kernel32.WaitForSingleObject(handle, 0)
        if result == wait_object_0:
            return False
        if result == wait_timeout:
            return True
        return None
    finally:
        kernel32.CloseHandle(handle)


def linux_process_identity(pid: int, binary: Path, data_dir: Path, instance_id: str | None = None) -> bool | None:
    proc = Path("/proc") / str(pid)
    try:
        executable = Path(os.readlink(proc / "exe")).resolve()
        argv = [part.decode(errors="replace") for part in (proc / "cmdline").read_bytes().split(b"\0") if part]
    except FileNotFoundError:
        return False
    except OSError:
        return None
    if executable != binary.resolve() or "watch" not in argv or "serve" not in argv:
        return False
    try:
        data_index = argv.index("--data-dir")
        if Path(argv[data_index + 1]).resolve() != data_dir.resolve():
            return False
        if instance_id is not None:
            instance_index = argv.index("--instance")
            if argv[instance_index + 1] != instance_id:
                return False
    except (ValueError, IndexError):
        return False
    try:
        fields = (proc / "stat").read_text().split()
        if fields[2] == "Z":
            return False
    except (OSError, IndexError):
        return False
    return True


def find_linux_collectors(binary: Path, data_dir: Path) -> list[tuple[int, str | None]]:
    matches = []
    for entry in Path("/proc").iterdir():
        if not entry.name.isdigit():
            continue
        pid = int(entry.name)
        identity = linux_process_identity(pid, binary, data_dir)
        if identity:
            argv = [part.decode(errors="replace") for part in (entry / "cmdline").read_bytes().split(b"\0") if part]
            instance_id = None
            try:
                instance_id = argv[argv.index("--instance") + 1]
            except (ValueError, IndexError):
                pass
            matches.append((pid, instance_id))
    return matches


def stop_detached(binary: Path, data_dir: Path, logs: Path, timeout: float = 10.0) -> tuple[bool, int | None, str]:
    """Stop only the collector owned by this isolated data directory."""
    status: dict | None = None
    pid: int | None = None
    instance_id: str | None = None
    try:
        status = run_json(binary, ["watch", "status", "--format", "json", "--data-dir", str(data_dir)],
                          logs, "watch-status-before-stop", timeout=10)
        status_pid = status.get("pid")
        pid = int(status_pid) if status_pid else None
        instance_id = status.get("instance_id") or None
    except (OSError, RuntimeError, subprocess.TimeoutExpired):
        pass
    if pid is None and sys.platform.startswith("linux"):
        matches = find_linux_collectors(binary, data_dir)
        if len(matches) == 1:
            pid, instance_id = matches[0]

    stop_rc: int | None = None
    stop_error = ""
    try:
        result = subprocess.run([str(binary), "watch", "stop", "--timeout", "10s", "--data-dir", str(data_dir)],
                                capture_output=True, text=True, timeout=15, check=False)
        stop_rc = result.returncode
        (logs / "watch-stop.stdout.txt").write_text(result.stdout)
        (logs / "watch-stop.stderr.txt").write_text(result.stderr)
        if stop_rc != 0:
            stop_error = f"watch stop exited {stop_rc}"
    except subprocess.TimeoutExpired:
        stop_error = "watch stop command timed out"
    except OSError as exc:
        stop_error = f"could not run watch stop: {exc}"

    def verify_terminated() -> bool:
        nonlocal status, pid, instance_id
        try:
            status = run_json(binary, ["watch", "status", "--format", "json", "--data-dir", str(data_dir)],
                              logs, "watch-status-after-stop", timeout=5)
        except (OSError, RuntimeError, subprocess.TimeoutExpired):
            status = None
        if status is not None:
            current_instance = status.get("instance_id") or None
            current_pid = status.get("pid")
            if current_instance and instance_id and current_instance != instance_id:
                return False
            if pid is None and current_pid:
                pid = int(current_pid)
                instance_id = current_instance
            if status.get("state") not in ("stopped", "idle", "none"):
                return False
        if pid is None:
            if sys.platform.startswith("linux"):
                return not find_linux_collectors(binary, data_dir)
            return status is not None and status.get("state") in ("stopped", "idle", "none")
        if sys.platform.startswith("linux"):
            identity = linux_process_identity(pid, binary, data_dir, instance_id)
            return status is not None and status.get("state") in ("stopped", "idle", "none") and identity is False
        alive = process_alive(pid)
        return status is not None and status.get("state") in ("stopped", "idle", "none") and alive is False

    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if verify_terminated():
            return True, pid, stop_error
        time.sleep(0.25)

    if pid is not None and sys.platform.startswith("linux"):
        identity = linux_process_identity(pid, binary, data_dir, instance_id)
        if identity:
            try:
                os.kill(pid, signal.SIGTERM)
                term_deadline = time.monotonic() + 5
                while time.monotonic() < term_deadline:
                    if verify_terminated():
                        fallback_detail = "used identity-checked SIGTERM fallback"
                        return True, pid, f"{stop_error}; {fallback_detail}" if stop_error else fallback_detail
                    time.sleep(0.2)
                if linux_process_identity(pid, binary, data_dir, instance_id):
                    os.kill(pid, signal.SIGKILL)
                    kill_deadline = time.monotonic() + 2
                    while time.monotonic() < kill_deadline:
                        if verify_terminated():
                            fallback_detail = "used identity-checked SIGKILL fallback"
                            return True, pid, f"{stop_error}; {fallback_detail}" if stop_error else fallback_detail
                        time.sleep(0.1)
            except OSError as exc:
                stop_error = f"identity-checked fallback failed: {exc}"

    detail = stop_error or "could not confirm that the owned collector terminated"
    return False, pid, detail


def cleanup_workspace(workspace: Path, *, keep: bool, collector_terminated: bool) -> bool:
    """Remove the disposable tree only when no detached process can still own it."""
    if keep or not collector_terminated:
        return True
    shutil.rmtree(workspace, ignore_errors=False)
    return False


def status_snapshot(binary: Path, data_dir: Path, logs: Path, label: str = "watch-status-poll") -> dict:
    return run_json(binary, ["watch", "status", "--format", "json", "--data-dir", str(data_dir)], logs, label, timeout=10)


def completed_successfully(status: dict) -> bool:
    if not status.get("last_completed_at") or status.get("pending"):
        return False
    outcomes = status.get("outcomes") or {}
    return (status.get("state") == "running" and status.get("discovery_failures", 0) == 0
            and outcomes.get("malformed_record", 0) == 0 and outcomes.get("failed_source", 0) == 0
            and outcomes.get("unsupported_usage", 0) == 0)


def wait_for_initial_poll(binary: Path, data_dir: Path, logs: Path, profile: str, expected_count: int,
                          timeout: float, on_poll=None) -> dict:
    deadline = time.monotonic() + timeout
    last_status = None
    while time.monotonic() < deadline:
        try:
            last_status = status_snapshot(binary, data_dir, logs)
        except (OSError, RuntimeError, subprocess.TimeoutExpired):
            time.sleep(0.25)
            continue
        if on_poll is not None:
            on_poll()
        if last_status.get("state") == "stopped" and last_status.get("pid"):
            raise RuntimeError(f"collector stopped before its initial successful poll: {last_status}")
        if last_status.get("last_completed_at"):
            if not completed_successfully(last_status):
                raise RuntimeError(f"initial collector poll was not successful: {last_status}")
            if last_status.get("imported") != expected_count:
                raise RuntimeError(f"initial collector poll imported {last_status.get('imported')} events; expected {expected_count}")
            assert_usage_summary(read_usage_summary(binary, data_dir, logs, "initial-poll-usage"), profile,
                                 expected_count, "initial collector poll")
            return last_status
        time.sleep(0.25)
    raise TimeoutError(f"no successful initial collector poll within {timeout:g}s; last status: {last_status}")


def write_metric_sample(sample_file, pid: int, start_ticks: tuple[int, int] | None,
                        previous: dict | None, previous_time: float, started: float) -> tuple[dict | None, float, tuple[int, int] | None]:
    current = linux_metrics(pid) if sys.platform.startswith("linux") else None
    if current is None:
        return previous, previous_time, start_ticks
    ticks = os.sysconf("SC_CLK_TCK")
    if start_ticks is None:
        start_ticks = (current["user_ticks"], current["system_ticks"])
    sample_time = time.monotonic()
    delta_cpu = 0 if previous is None else ((current["user_ticks"] - previous["user_ticks"])
                                             + (current["system_ticks"] - previous["system_ticks"])) / ticks
    cpu_percent = delta_cpu / max(0.001, sample_time - previous_time) * 100
    elapsed = sample_time - started
    measured_user = (current["user_ticks"] - start_ticks[0]) / ticks
    measured_system = (current["system_ticks"] - start_ticks[1]) / ticks
    sample_file.write(f"{elapsed:.3f},{cpu_percent:.3f},{measured_user:.3f},{measured_system:.3f},"
                      f"{current['rss_kib']/1024:.3f},{current['peak_rss_kib']/1024:.3f},"
                      f"{current['read_bytes']},{current['write_bytes']},{current['rchar_bytes']},{current['wchar_bytes']}\n")
    sample_file.flush()
    return current, sample_time, start_ticks


def sample_cold_metrics(sample_file, pid: int, start_ticks: tuple[int, int] | None,
                        previous: dict | None, previous_time: float, started: float,
                        last_sample: float, *, force: bool = False) -> tuple[dict | None, float, tuple[int, int] | None, float]:
    now = time.monotonic()
    if not force and last_sample != 0 and now - last_sample < 0.5:
        return previous, previous_time, start_ticks, last_sample
    previous, previous_time, start_ticks = write_metric_sample(
        sample_file, pid, start_ticks, previous, previous_time, started)
    return previous, previous_time, start_ticks, now


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
    parser.add_argument("--startup-timeout", type=parse_duration, default=300.0,
                        help="maximum wait for the initial successful poll; default 300s")
    parser.add_argument("--drain-timeout", type=parse_duration, default=30.0,
                        help="maximum wait for the final generated append to be collected; default 30s")
    parser.add_argument("--artifacts", type=Path, help="output directory for manifest, samples, and logs")
    parser.add_argument("--keep-workdir", action="store_true", help="retain generated source and disposable database")
    args = parser.parse_args()

    binary = args.binary.expanduser().resolve()
    if not binary.is_file() or not os.access(binary, os.X_OK):
        parser.error("--binary must name an executable file")
    if (args.duration <= 0 or args.warmup < 0 or args.append_every <= 0 or args.startup_timeout <= 0
            or args.drain_timeout <= 0 or min(args.events, args.files) < 1 or args.record_padding_bytes < 0):
        parser.error("duration, counts, timeouts, and append cadence must be positive; warmup and record padding cannot be negative")
    try:
        interval_seconds = parse_duration(args.interval)
    except argparse.ArgumentTypeError as exc:
        parser.error(str(exc))
    if interval_seconds <= 0:
        parser.error("collector interval must be positive")
    if args.scenario == "cold" and args.mode != "foreground":
        parser.error("cold backfill measurement requires --mode foreground so launch and backfill have an observable process boundary")

    artifacts = args.artifacts.expanduser().resolve() if args.artifacts else Path.cwd() / ("collector-measurement-" + dt.datetime.now().strftime("%Y%m%d-%H%M%S"))
    artifacts.mkdir(parents=True, exist_ok=False)
    workspace = Path(tempfile.mkdtemp(prefix="komposyn-lg-measure-"))
    source = workspace / "source"
    data = workspace / "data"
    data.mkdir()
    logs = artifacts / "logs"
    logs.mkdir()
    process = None
    detached_attempted = False
    retain_workspace = False
    result_code = 0
    manifest: dict | None = None
    pid: int | None = None
    try:
        paths = generate(source, args.profile, args.events, args.files, args.record_padding_bytes)
        fixture_bytes = sum(path.stat().st_size for path in paths)
        version = run_checked([str(binary), "version"], logs / "version.txt").strip()
        try:
            build_info = subprocess.run(["go", "version", "-m", str(binary)], capture_output=True,
                                        text=True, timeout=10, check=False).stdout.strip()
        except (OSError, subprocess.TimeoutExpired):
            build_info = "unavailable"
        fixture_sanity = validate_generated_fixture(binary, source, args.profile, args.events, paths, logs, workspace)
        run_checked([str(binary), "watch", "add", str(source), "--data-dir", str(data)], logs / "watch-add.txt")
        manifest = {
            "binary": str(binary), "version": version, "profile": args.profile, "scenario": args.scenario,
            "mode": args.mode, "interval": args.interval,
            "duration_seconds": None if args.scenario == "cold" else args.duration,
            "cold_measurement_boundary": "before foreground process launch; ends after first successful completed poll" if args.scenario == "cold" else None,
            "warmup_seconds": args.warmup if args.scenario != "cold" else 0,
            "generated_files": len(paths), "usage_records": args.events, "fixture_bytes": fixture_bytes,
            "record_padding_bytes": args.record_padding_bytes, "append_every_seconds": args.append_every,
            "build_info": build_info, "machine_metadata": machine_metadata(),
            "source_path": str(source), "data_dir": str(data),
            "platform": platform.platform(), "machine": platform.machine(), "logical_cpus": os.cpu_count(),
            "fixture_sanity": fixture_sanity,
            "run_prepared_utc": dt.datetime.now(dt.timezone.utc).isoformat(),
            "metrics_source": "/proc/<pid>/{stat,status,io}" if sys.platform.startswith("linux") else "native platform collector required",
            "phase_poll_interval_seconds": 0.25,
        }
        (artifacts / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")

        def start() -> tuple[int, str | None, float]:
            nonlocal process, detached_attempted
            launch_start = time.monotonic()
            manifest["launch_started_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
            if args.mode == "foreground":
                cmd = [str(binary), "watch", "foreground", "--interval", args.interval,
                       "--data-dir", str(data), str(source)]
                popen_args = {"creationflags": subprocess.CREATE_NEW_PROCESS_GROUP} if os.name == "nt" else {"start_new_session": True}
                with (logs / "collector-stdout.txt").open("w") as out, (logs / "collector-stderr.txt").open("w") as err:
                    process = subprocess.Popen(cmd, stdout=out, stderr=err, **popen_args)
                return process.pid, None, time.monotonic() - launch_start
            detached_attempted = True
            run_checked([str(binary), "watch", "start", "--interval", args.interval, "--data-dir", str(data)],
                        logs / "watch-start.txt", timeout=args.startup_timeout + 15)
            status = run_json(binary, ["watch", "status", "--format", "json", "--data-dir", str(data)],
                              logs, "watch-status-start")
            return int(status["pid"]), status.get("instance_id"), time.monotonic() - launch_start

        launch_start = time.monotonic()
        if args.scenario == "cold":
            manifest["measurement_started_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
            measure_start = launch_start
        pid, instance_id, launch_elapsed = start()
        manifest["collector_pid"] = pid
        manifest["collector_instance_id"] = instance_id
        manifest["launch_elapsed_seconds"] = launch_elapsed
        manifest["process_cpu_sampling_origin"] = "first /proc sample; CPU before that sample is excluded" if sys.platform.startswith("linux") else "native platform tool"
        manifest["measurement_started_utc"] = manifest.get("measurement_started_utc") or dt.datetime.now(dt.timezone.utc).isoformat()
        (artifacts / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")

        def ensure_collector_running() -> None:
            if process is not None and process.poll() is not None:
                raise RuntimeError(f"foreground collector exited unexpectedly ({process.returncode})")
            if args.mode == "detached":
                if sys.platform.startswith("linux"):
                    identity = linux_process_identity(pid, binary, data, instance_id)
                    if identity is False:
                        raise RuntimeError(f"detached collector PID {pid} exited or no longer matches instance {instance_id}")
                elif process_alive(pid) is False:
                    raise RuntimeError(f"detached collector PID {pid} exited before measurement ended")

        sample_path = artifacts / "samples.csv"
        sample_header = "elapsed_seconds,cpu_percent_one_core,user_cpu_seconds,system_cpu_seconds,rss_mib,peak_rss_mib,read_bytes,written_bytes,rchar_bytes,wchar_bytes\n"
        append_count = 0

        with sample_path.open("w") as sample_file:
            sample_file.write(sample_header)
            if args.scenario == "cold":
                previous = None
                previous_time = measure_start
                cpu_origin = None
                last_sample = 0.0

                def sample_cold(force: bool = False) -> None:
                    nonlocal previous, previous_time, cpu_origin, last_sample
                    ensure_collector_running()
                    previous, previous_time, cpu_origin, last_sample = sample_cold_metrics(
                        sample_file, pid, cpu_origin, previous, previous_time, measure_start,
                        last_sample, force=force)

                sample_cold()
                cold_status = wait_for_initial_poll(binary, data, logs, args.profile, args.events,
                                                    args.startup_timeout, sample_cold)
                if cold_status.get("pid") != pid:
                    raise RuntimeError("cold poll status does not belong to the launched foreground PID")
                manifest["collector_instance_id"] = cold_status.get("instance_id")
                sample_cold(force=True)
                measurement_end = time.monotonic()
                manifest["measurement_finished_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
                manifest["elapsed_seconds"] = round(measurement_end - measure_start, 3)
                manifest["cold_launch_included"] = True
                manifest["cold_wall_boundary_note"] = "wall time begins before Popen; process CPU begins at first /proc sample"
            else:
                initial_status = wait_for_initial_poll(binary, data, logs, args.profile, args.events,
                                                       args.startup_timeout, ensure_collector_running)
                if initial_status.get("pid") != pid or (instance_id and initial_status.get("instance_id") != instance_id):
                    raise RuntimeError("initial poll status does not belong to the launched collector PID/instance")
                manifest["initial_poll_completed_utc"] = initial_status["last_completed_at"]
                manifest["initial_poll_imported_events"] = initial_status["imported"]
                initial_ignored_non_usage = (initial_status.get("outcomes") or {}).get("ignored_non_usage", 0)
                warmup_end = time.monotonic() + args.warmup
                while True:
                    remaining_warmup = warmup_end - time.monotonic()
                    if remaining_warmup <= 0:
                        break
                    ensure_collector_running()
                    time.sleep(min(0.25, remaining_warmup))
                manifest["warmup_finished_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
                measure_start = time.monotonic()
                manifest["measurement_started_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
                manifest["measurement_boundary"] = "after successful initial poll and configured warmup"
                previous = None
                previous_time = measure_start
                cpu_origin = None
                previous, previous_time, cpu_origin = write_metric_sample(
                    sample_file, pid, cpu_origin, previous, previous_time, measure_start)
                append_at = measure_start + args.append_every
                end = measure_start + args.duration
                last_append_utc = None
                while time.monotonic() < end:
                    ensure_collector_running()
                    now = time.monotonic()
                    if args.scenario in ("usage-append", "non-usage-append", "soak") and now >= append_at:
                        append_count += 1
                        file_counter = generated_counts(args.events, args.files)[0] + append_count
                        line = event_line(args.profile, args.events + append_count, file_counter,
                                          "measurement-session-0", usage=args.scenario != "non-usage-append")
                        with paths[0].open("ab") as target:
                            target.write(line)
                        last_append_utc = dt.datetime.now(dt.timezone.utc)
                        append_at += args.append_every
                    time.sleep(0.5)
                    previous, previous_time, cpu_origin = write_metric_sample(
                        sample_file, pid, cpu_origin, previous, previous_time, measure_start)
                measurement_end = time.monotonic()
                manifest["measurement_finished_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
                manifest["elapsed_seconds"] = round(measurement_end - measure_start, 3)

                expected_final = args.events + (append_count if args.scenario in ("usage-append", "soak") else 0)
                if last_append_utc is not None:
                    drain_deadline = time.monotonic() + max(args.drain_timeout, interval_seconds + 5)
                    while time.monotonic() < drain_deadline:
                        status = status_snapshot(binary, data, logs, "watch-status-drain")
                        completed_at = status.get("last_completed_at")
                        try:
                            completed_time = dt.datetime.fromisoformat(completed_at.replace("Z", "+00:00")) if completed_at else None
                        except ValueError:
                            completed_time = None
                        if (completed_successfully(status) and status.get("pid") == pid
                                and (not instance_id or status.get("instance_id") == instance_id)
                                and completed_time is not None and completed_time >= last_append_utc
                                and (args.scenario != "non-usage-append"
                                     or (status.get("outcomes") or {}).get("ignored_non_usage", 0)
                                     >= initial_ignored_non_usage + append_count)):
                            break
                        time.sleep(0.25)
                    else:
                        raise TimeoutError(f"collector did not complete a poll after the final append; pid={pid}")
                final_summary = read_usage_summary(binary, data, logs, "final-usage")
                assert_usage_summary(final_summary, args.profile, expected_final, "measured collector run")

        manifest["appended_records"] = append_count
        manifest["final_fixture_bytes"] = sum(path.stat().st_size for path in paths)
        manifest["measurement_drain_seconds"] = round(time.monotonic() - measurement_end, 3)
        (artifacts / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        print(f"Artifacts: {artifacts}")
        print(f"Measured {version}; scenario={args.scenario} mode={args.mode} pid={pid} interval={args.interval}")
        if not sys.platform.startswith("linux"):
            print("Native process metrics are unavailable in this script on this platform; follow the guide.")
    except (OSError, RuntimeError, subprocess.TimeoutExpired, json.JSONDecodeError) as exc:
        print(f"measurement failed: {exc}", file=sys.stderr)
        result_code = 1
    except KeyboardInterrupt:
        print("interrupted; cleaning up collector", file=sys.stderr)
        result_code = 130
    finally:
        shutdown_start = time.monotonic()
        if manifest is not None:
            manifest["shutdown_started_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
        if detached_attempted:
            confirmed, stopped_pid, cleanup_detail = stop_detached(binary, data, logs, timeout=args.drain_timeout)
            if manifest is not None:
                manifest["cleanup_confirmed"] = confirmed
                manifest["cleanup_pid"] = stopped_pid
                manifest["cleanup_detail"] = cleanup_detail
            if not confirmed:
                retain_workspace = True
                result_code = 1
                print(f"collector termination not confirmed (PID {stopped_pid}); retained workspace {workspace}: {cleanup_detail}", file=sys.stderr)
        if process is not None and process.poll() is None:
            try:
                interrupt_foreground(process)
                process.wait(timeout=5)
            except (OSError, subprocess.TimeoutExpired):
                try:
                    process.kill()
                    process.wait(timeout=5)
                except (OSError, subprocess.TimeoutExpired):
                    retain_workspace = True
                    result_code = 1
                    print(f"foreground process termination not confirmed (PID {process.pid}); retained workspace {workspace}", file=sys.stderr)
        if manifest is not None:
            manifest["shutdown_finished_utc"] = dt.datetime.now(dt.timezone.utc).isoformat()
            manifest["shutdown_elapsed_seconds"] = round(time.monotonic() - shutdown_start, 3)
            try:
                (artifacts / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
            except OSError as exc:
                print(f"could not write final manifest: {exc}", file=sys.stderr)
        try:
            retained = cleanup_workspace(workspace, keep=args.keep_workdir,
                                         collector_terminated=not retain_workspace)
        except OSError as exc:
            retained = True
            result_code = 1
            print(f"could not remove temporary workspace {workspace}: {exc}", file=sys.stderr)
        if retained:
            print(f"Generated fixture and disposable database: {workspace}")

    return result_code


if __name__ == "__main__":
    raise SystemExit(main())
