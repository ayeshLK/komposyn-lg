#!/usr/bin/env python3
# Copyright 2026 Ayesh Almeida
# Licensed under the Apache License, Version 2.0.
import importlib.util
import unittest
from pathlib import Path

script = Path(__file__).with_name("verify-protocol-smoke.py")
spec = importlib.util.spec_from_file_location("protocol_smoke", script)
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


def response(request_id, result):
    return {"protocol": "komposyn.token-logger", "version": 1, "id": request_id, "ok": True, "result": result}


class ProtocolSmokeTest(unittest.TestCase):
    def test_accepts_expected_success_responses(self):
        module.validate([
            response("init", {"cli_version": "0.7.1", "plugin_version": "0.7.0"}),
            response("stop", {"stopped": True}),
        ], "0.7.1", "0.7.0")

    def test_rejects_initialize_error_even_if_database_was_created(self):
        initialize_error = {"protocol": "komposyn.token-logger", "version": 1, "id": "init", "error": {"code": "initialize_failed"}}
        shutdown_error = {"protocol": "komposyn.token-logger", "version": 1, "id": "stop", "error": {"code": "not_initialized"}}
        with self.assertRaises(ValueError):
            module.validate([initialize_error, shutdown_error], "0.7.1", "0.7.0")


if __name__ == "__main__":
    unittest.main()
