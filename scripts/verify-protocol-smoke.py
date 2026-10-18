#!/usr/bin/env python3
# Copyright 2026 Ayesh Almeida
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import json
import sys
from pathlib import Path


def validate(responses, cli_version, core_version):
    if len(responses) != 2:
        raise ValueError(f"expected two protocol responses, got {len(responses)}")
    initialize, shutdown = responses
    for response, request_id in ((initialize, "init"), (shutdown, "stop")):
        if response.get("protocol") != "komposyn.token-logger" or response.get("version") != 1:
            raise ValueError("response has an unexpected protocol identity")
        if response.get("id") != request_id or response.get("ok") is not True:
            raise ValueError(f"{request_id} did not return a successful matching response")
        if "error" in response:
            raise ValueError(f"{request_id} returned a protocol error")
    result = initialize.get("result")
    if not isinstance(result, dict):
        raise ValueError("initialize response has no result object")
    if result.get("cli_version") != cli_version or result.get("plugin_version") != core_version:
        raise ValueError("initialize returned unexpected CLI/core version metadata")
    if shutdown.get("result", {}).get("stopped") is not True:
        raise ValueError("shutdown response did not confirm a stopped server")


def main():
    if len(sys.argv) != 4:
        raise SystemExit("usage: verify-protocol-smoke.py <jsonl-responses> <cli-version> <core-version>")
    responses = [json.loads(line) for line in Path(sys.argv[1]).read_text(encoding="utf-8").splitlines() if line.strip()]
    validate(responses, sys.argv[2], sys.argv[3])


if __name__ == "__main__":
    main()
