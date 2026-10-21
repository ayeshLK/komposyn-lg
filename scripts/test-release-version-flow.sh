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

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
core_version="$(bash "$root_dir/scripts/resolve-release-core-version.sh")"
[[ "$core_version" == 0.9.0 ]] || { echo "unexpected validated release core: $core_version" >&2; exit 1; }

bash "$root_dir/scripts/verify-cli-version-output.sh" 'komposyn-lg 0.10.0 (core 0.9.0)' 0.10.0 0.9.0
if bash "$root_dir/scripts/verify-cli-version-output.sh" 'komposyn-lg 0.10.0 (core 0.8.0)' 0.10.0 0.9.0 >/dev/null 2>&1; then
	echo 'accepted stale core version metadata' >&2
	exit 1
fi
if bash "$root_dir/scripts/verify-cli-version-output.sh" 'komposyn-lg 0.9.0 (core 0.9.0)' 0.10.0 0.9.0 >/dev/null 2>&1; then
	echo 'accepted incorrect CLI version metadata' >&2
	exit 1
fi

python3 - "$root_dir/.github/workflows/release.yml" "$root_dir/scripts/build-release.sh" <<'PY'
import sys
from pathlib import Path

workflow = Path(sys.argv[1]).read_text(encoding="utf-8")
builder = Path(sys.argv[2]).read_text(encoding="utf-8")
required = {
    "prepare output from the validated dependency": "core_version: ${{ steps.core.outputs.version }}",
    "release commit resolver": "bash scripts/resolve-release-core-version.sh",
    "native runtime verifier": "EXPECTED_CORE_VERSION: ${{ needs.prepare.outputs.core_version }}",
    "Unix active-upgrade verifier": '"${{ needs.prepare.outputs.version }}" "${{ needs.prepare.outputs.core_version }}"',
    "Windows active-upgrade verifier": "-ExpectedCliVersion \"${{ needs.prepare.outputs.version }}\" -ExpectedCoreVersion \"${{ needs.prepare.outputs.core_version }}\"",
    "post-publication verifier": '"${{ needs.prepare.outputs.core_version }}"',
}
for description, fragment in required.items():
    if fragment not in workflow:
        raise SystemExit(f"release workflow omits {description}")
if 'resolve-release-core-version.sh' not in builder:
    raise SystemExit("release builder does not share the validated core resolver")
if "EXPECTED_CORE_VERSION: 0.8.0" in workflow or '"$RELEASE_VERSION" 0.8.0' in workflow:
    raise SystemExit("release workflow still has a stale hard-coded core expectation")
PY

echo 'release core-version flow checks passed'
