#!/usr/bin/env bash
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

set -euo pipefail

output="${1:?usage: verify-cli-version-output.sh <version-output> <expected-cli-version> <expected-core-version>}"
cli_version="${2:?expected CLI version required}"
core_version="${3:?expected core version required}"
expected="komposyn-lg $cli_version (core $core_version)"
if [[ "$output" != "$expected" ]]; then
	echo "expected executable metadata '$expected'; got '$output'" >&2
	exit 1
fi
