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
dist="${1:?usage: list-release-assets.sh <release-dist-directory>}"
[[ -d "$dist" ]] || { echo "release directory does not exist: $dist" >&2; exit 1; }
mapfile -t archives < <(find "$dist" -maxdepth 1 -type f \( -name 'komposyn-lg_*.tar.gz' -o -name 'komposyn-lg_*.zip' \) -print | sort)
[[ "${#archives[@]}" -eq 6 ]] || { echo "expected six release archives, found ${#archives[@]}" >&2; exit 1; }
assets=("${archives[@]}" "$dist/SHA256SUMS")
for path in "$dist/SHA256SUMS.sig" "$dist/SHA256SUMS.pem"; do
	[[ -f "$path" ]] || { echo "missing signed release asset: $path" >&2; exit 1; }
	assets+=("$path")
done
for archive in "${archives[@]}"; do
	for suffix in .sig .pem; do
		path="${archive}${suffix}"
		[[ -f "$path" ]] || { echo "missing signed release asset: $path" >&2; exit 1; }
		assets+=("$path")
	done
done
printf '%s\n' "${assets[@]}"
