#!/usr/bin/env bash
# Copyright 2026 Ayesh Almeida
# Licensed under the Apache License, Version 2.0.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
touch "$tmp/SHA256SUMS" "$tmp/SHA256SUMS.sig" "$tmp/SHA256SUMS.pem"
for target in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64 windows_amd64 windows_arm64; do
	if [[ "$target" == windows_* ]]; then suffix=zip; else suffix=tar.gz; fi
	archive="$tmp/komposyn-lg_0.8.1_${target}.${suffix}"
	touch "$archive" "$archive.sig" "$archive.pem"
done
mkdir -p "$tmp/third_party_licenses"
touch "$tmp/THIRD_PARTY_LICENSES.md"
mapfile -t assets < <("$root/scripts/list-release-assets.sh" "$tmp")
[[ "${#assets[@]}" -eq 21 ]] || { echo "expected 21 uploaded artifacts, got ${#assets[@]}" >&2; exit 1; }
for asset in "${assets[@]}"; do
	[[ "$asset" != *THIRD_PARTY_LICENSES.md && "$asset" != *third_party_licenses* ]] || { echo "staging attribution was incorrectly selected: $asset" >&2; exit 1; }
done
echo 'release asset allowlist regression passed'
