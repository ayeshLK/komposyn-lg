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
[[ "$core_version" == 0.10.0 ]] || { echo "unexpected validated release core: $core_version" >&2; exit 1; }

bash "$root_dir/scripts/verify-cli-version-output.sh" 'komposyn-lg 0.10.0 (core 0.10.0)' 0.10.0 0.10.0
if bash "$root_dir/scripts/verify-cli-version-output.sh" 'komposyn-lg 0.10.0 (core 0.9.0)' 0.10.0 0.10.0 >/dev/null 2>&1; then
	echo 'accepted stale core version metadata' >&2
	exit 1
fi
if bash "$root_dir/scripts/verify-cli-version-output.sh" 'komposyn-lg 0.9.0 (core 0.10.0)' 0.10.0 0.10.0 >/dev/null 2>&1; then
	echo 'accepted incorrect CLI version metadata' >&2
	exit 1
fi

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
output_file="$tmp_dir/github-output"
: > "$output_file"
failing_resolver="$tmp_dir/failing-resolver.sh"
printf '#!/usr/bin/env bash\nexit 19\n' > "$failing_resolver"
if bash "$root_dir/scripts/write-release-core-version-output.sh" "$output_file" "$failing_resolver" >/dev/null 2>&1; then
	echo 'release core output helper accepted a failing resolver' >&2
	exit 1
fi
[[ ! -s "$output_file" ]] || { echo 'failing resolver wrote a GitHub output value' >&2; exit 1; }

empty_resolver="$tmp_dir/empty-resolver.sh"
printf '#!/usr/bin/env bash\nexit 0\n' > "$empty_resolver"
if bash "$root_dir/scripts/write-release-core-version-output.sh" "$output_file" "$empty_resolver" >/dev/null 2>&1; then
	echo 'release core output helper accepted an empty resolver result' >&2
	exit 1
fi
[[ ! -s "$output_file" ]] || { echo 'empty resolver wrote a GitHub output value' >&2; exit 1; }

successful_resolver="$tmp_dir/successful-resolver.sh"
cat > "$successful_resolver" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' '0.10.0'
EOF
bash "$root_dir/scripts/write-release-core-version-output.sh" "$output_file" "$successful_resolver"
[[ "$(cat "$output_file")" == 'version=0.10.0' ]] || { echo 'release core output helper wrote an unexpected output' >&2; exit 1; }

python3 - "$root_dir/.github/workflows/release.yml" "$root_dir/scripts/build-release.sh" "$root_dir/scripts/write-release-core-version-output.sh" <<'PY'
import sys
from pathlib import Path

workflow = Path(sys.argv[1]).read_text(encoding="utf-8")
builder = Path(sys.argv[2]).read_text(encoding="utf-8")
writer = Path(sys.argv[3]).read_text(encoding="utf-8")
required = {
    "prepare output from the validated dependency": "core_version: ${{ steps.core.outputs.version }}",
    "release core output writer": "bash scripts/write-release-core-version-output.sh \"$GITHUB_OUTPUT\"",
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
if 'core_version="$(bash "$resolver")"' not in writer or 'printf \'version=%s\\n\' "$core_version" >> "$output_file"' not in writer:
    raise SystemExit("release core output helper must resolve successfully before writing the value")
if "EXPECTED_CORE_VERSION: 0.8.0" in workflow or '"$RELEASE_VERSION" 0.8.0' in workflow:
    raise SystemExit("release workflow still has a stale hard-coded core expectation")
PY

echo 'release core-version flow checks passed'
