#!/usr/bin/env python3
"""Focused unit checks for the developer collector measurement harness."""

import importlib.util
import io
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).with_name("measure-collector.py")
SPEC = importlib.util.spec_from_file_location("measure_collector", SCRIPT)
HARNESS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HARNESS)


@unittest.skipUnless(sys.platform.startswith("linux"), "Linux process-identity cleanup regressions")
class DetachedCleanupTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="measure-cleanup-test-")
        self.root = Path(self.temp.name)
        self.logs = self.root / "logs"
        self.logs.mkdir()
        self.binary = self.root / "komposyn-lg"
        self.binary.touch()
        self.data = self.root / "data"
        self.data.mkdir()
        self.pid = 321
        self.instance = "owned-instance"

    def tearDown(self):
        self.temp.cleanup()

    def status(self, state):
        return {"state": state, "pid": self.pid, "instance_id": self.instance}

    def test_nonzero_stop_is_reported_but_confirmed_exit_is_safe(self):
        statuses = iter((self.status("running"), self.status("stopped")))
        with (mock.patch.object(HARNESS, "run_json", side_effect=lambda *a, **k: next(statuses)),
              mock.patch.object(HARNESS.subprocess, "run",
                                return_value=subprocess.CompletedProcess([], 2, "", "stop failed")),
              mock.patch.object(HARNESS, "linux_process_identity", return_value=False)):
            confirmed, pid, detail = HARNESS.stop_detached(self.binary, self.data, self.logs, timeout=0.01)
        self.assertTrue(confirmed)
        self.assertEqual(pid, self.pid)
        self.assertIn("exited 2", detail)

    def test_stop_timeout_uses_identity_checked_term_fallback(self):
        state = {"alive": True}

        def status(*_args, **_kwargs):
            return self.status("running" if state["alive"] else "stopped")

        def identity(*_args, **_kwargs):
            return state["alive"]

        def kill(_pid, sig):
            if sig == HARNESS.signal.SIGTERM:
                state["alive"] = False

        with (mock.patch.object(HARNESS, "run_json", side_effect=status),
              mock.patch.object(HARNESS.subprocess, "run", side_effect=subprocess.TimeoutExpired([], 15)),
              mock.patch.object(HARNESS, "linux_process_identity", side_effect=identity),
              mock.patch.object(HARNESS.os, "kill", side_effect=kill),
              mock.patch.object(HARNESS.time, "sleep", return_value=None)):
            confirmed, pid, detail = HARNESS.stop_detached(self.binary, self.data, self.logs, timeout=0.001)
        self.assertTrue(confirmed)
        self.assertEqual(pid, self.pid)
        self.assertIn("SIGTERM", detail)

    def test_surviving_child_is_not_confirmed_and_workspace_is_retained(self):
        signals = []

        def clock():
            clock.tick += 1
            return clock.tick

        clock.tick = 0
        with (mock.patch.object(HARNESS, "run_json", side_effect=lambda *a, **k: self.status("running")),
              mock.patch.object(HARNESS.subprocess, "run", side_effect=subprocess.TimeoutExpired([], 15)),
              mock.patch.object(HARNESS, "linux_process_identity", return_value=True),
              mock.patch.object(HARNESS.os, "kill", side_effect=lambda pid, sig: signals.append(sig)),
              mock.patch.object(HARNESS.time, "monotonic", side_effect=clock),
              mock.patch.object(HARNESS.time, "sleep", return_value=None)):
            confirmed, pid, detail = HARNESS.stop_detached(self.binary, self.data, self.logs, timeout=1)
        self.assertFalse(confirmed)
        self.assertEqual(pid, self.pid)
        self.assertEqual(signals, [HARNESS.signal.SIGTERM, HARNESS.signal.SIGKILL])
        self.assertTrue(HARNESS.cleanup_workspace(self.root, keep=False, collector_terminated=confirmed))
        self.assertTrue(self.root.exists())

    def test_unconfirmed_shutdown_retains_workspace(self):
        self.assertTrue(HARNESS.cleanup_workspace(self.root, keep=False, collector_terminated=False))
        self.assertTrue(self.root.exists())


class WindowsProcessProbeTests(unittest.TestCase):
    def test_windows_process_liveness_dispatch_avoids_os_kill(self):
        with (mock.patch.object(HARNESS.os, "name", "nt"),
              mock.patch.object(HARNESS, "windows_process_alive", return_value=True) as probe,
              mock.patch.object(HARNESS.os, "kill", side_effect=AssertionError("Windows probe must not signal"))):
            self.assertTrue(HARNESS.process_alive(321))
            self.assertTrue(HARNESS.process_alive(321))
        self.assertEqual(probe.call_count, 2)

    def test_repeated_windows_liveness_checks_only_query_and_wait(self):
        class Kernel32:
            def __init__(self):
                self.waits = 0
                self.closed = 0

            def OpenProcess(self, access, inherit, pid):
                self.asserted_access = access
                self.asserted_pid = pid
                return 123

            def WaitForSingleObject(self, handle, timeout):
                self.waits += 1
                return 258  # WAIT_TIMEOUT: process is still running.

            def CloseHandle(self, handle):
                self.closed += 1
                return True

        kernel = Kernel32()
        self.assertTrue(HARNESS.windows_process_alive(321, kernel))
        self.assertTrue(HARNESS.windows_process_alive(321, kernel))
        self.assertEqual(kernel.waits, 2)
        self.assertEqual(kernel.closed, 2)
        self.assertEqual(kernel.asserted_pid, 321)
        self.assertEqual(kernel.asserted_access, 0x00100000 | 0x1000)

    @unittest.skipUnless(os.name == "nt", "native Windows child-process regression")
    def test_native_repeated_liveness_checks_leave_child_running(self):
        child = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(30)"])
        try:
            for _ in range(3):
                self.assertTrue(HARNESS.process_alive(child.pid))
                self.assertIsNone(child.poll())
        finally:
            child.terminate()
            child.wait(timeout=5)

    @unittest.skipUnless(os.name == "nt" and os.getenv("KOMPOSYN_LG_TEST_BINARY"),
                         "native Windows detached collector integration")
    def test_detached_generated_workload_survives_measurement_probes(self):
        root = Path(tempfile.mkdtemp(prefix="measure-windows-probe-"))
        self.addCleanup(lambda: shutil.rmtree(root, ignore_errors=True))
        artifacts = root / "artifacts"
        result = subprocess.run(
            [sys.executable, str(SCRIPT), "--binary", os.environ["KOMPOSYN_LG_TEST_BINARY"],
             "--mode", "detached", "--scenario", "unchanged", "--duration", "1s",
             "--warmup", "0s", "--startup-timeout", "30s", "--events", "2",
             "--artifacts", str(artifacts)],
            capture_output=True, text=True, timeout=90, check=False,
        )
        self.assertEqual(result.returncode, 0, f"stdout={result.stdout}\nstderr={result.stderr}")
        manifest = json.loads((artifacts / "manifest.json").read_text())
        self.assertTrue(manifest["cleanup_confirmed"], manifest)
        self.assertEqual(manifest["fixture_sanity"]["initial_imported_events"], 2)


@unittest.skipUnless(sys.platform.startswith("linux"), "Linux /proc metrics regression")
class ColdSamplingTests(unittest.TestCase):
    def test_forced_completion_sample_captures_sub_half_second_tail(self):
        metrics_before = {
            "user_ticks": 10, "system_ticks": 4, "rss_kib": 1000, "peak_rss_kib": 1200,
            "read_bytes": 1, "write_bytes": 2, "rchar_bytes": 3, "wchar_bytes": 4,
        }
        metrics_after = {**metrics_before, "user_ticks": 25, "system_ticks": 7, "rss_kib": 1800}
        sample_file = io.StringIO()
        with (mock.patch.object(HARNESS, "linux_metrics", return_value=metrics_after),
              mock.patch.object(HARNESS.time, "monotonic", return_value=100.39),
              mock.patch.object(HARNESS.os, "sysconf", return_value=100)):
            previous, previous_time, ticks, last_sample = HARNESS.sample_cold_metrics(
                sample_file, 321, (10, 4), metrics_before, 100.0, 100.0, 100.0)
            self.assertIs(previous, metrics_before)
            self.assertEqual(previous_time, 100.0)
            self.assertEqual(last_sample, 100.0)
            previous, previous_time, ticks, last_sample = HARNESS.sample_cold_metrics(
                sample_file, 321, ticks, previous, previous_time, 100.0, last_sample, force=True)

        self.assertIs(previous, metrics_after)
        self.assertEqual(ticks, (10, 4))
        self.assertEqual(sample_file.getvalue().count("\n"), 1)
        self.assertIn("0.150,0.030", sample_file.getvalue())

if __name__ == "__main__":
    unittest.main()
