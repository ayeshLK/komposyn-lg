#!/usr/bin/env python3
"""Focused unit checks for the developer collector measurement harness."""

import importlib.util
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).with_name("measure-collector.py")
SPEC = importlib.util.spec_from_file_location("measure_collector", SCRIPT)
HARNESS = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(HARNESS)


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


if __name__ == "__main__":
    unittest.main()
