#!/usr/bin/env bash
set -euo pipefail

dist="${1:?usage: verify-signed-runtime.sh <signed-release-dir> <expected-target> <expected-version> <expected-core-version>}"
target="${2:?expected target required}"
version="${3:?expected CLI version required}"
core_version="${4:?expected core version required}"
identity='^https://github.com/ayeshLK/komposyn-lg/.github/workflows/release.yml@refs/heads/master$'
issuer='https://token.actions.githubusercontent.com'
host="$(uname -s):$(uname -m)"
case "$host" in
  Linux:x86_64|Linux:amd64) native=linux_amd64 ;;
  Linux:aarch64|Linux:arm64) native=linux_arm64 ;;
  Darwin:x86_64) native=darwin_amd64 ;;
  Darwin:arm64) native=darwin_arm64 ;;
  MINGW64_NT*:x86_64|MSYS_NT*:x86_64|CYGWIN_NT*:x86_64|MINGWARM64_NT*:aarch64|MINGWARM64_NT*:arm64|MSYS_NT*:aarch64|MSYS_NT*:arm64|CYGWIN_NT*:aarch64|CYGWIN_NT*:arm64)
    case "$target" in windows_amd64|windows_arm64) native="$target" ;; *) native=windows_amd64 ;; esac ;;
  *) echo "unsupported native runtime host: $host" >&2; exit 1 ;;
esac
[[ "$native" == "$target" ]] || { echo "native target $native does not match $target" >&2; exit 1; }
if [[ "$target" == windows_* ]]; then suffix=zip; binary=komposyn-lg.exe; else suffix=tar.gz; binary=komposyn-lg; fi
asset="komposyn-lg_${version}_${target}.${suffix}"
for path in "$dist/SHA256SUMS" "$dist/SHA256SUMS.sig" "$dist/SHA256SUMS.pem" "$dist/$asset" "$dist/$asset.sig" "$dist/$asset.pem"; do
  [[ -f "$path" ]] || { echo "missing signed runtime asset $path" >&2; exit 1; }
done
cosign verify-blob --signature "$dist/SHA256SUMS.sig" --certificate "$dist/SHA256SUMS.pem" --certificate-identity-regexp "$identity" --certificate-oidc-issuer "$issuer" "$dist/SHA256SUMS"
cosign verify-blob --signature "$dist/$asset.sig" --certificate "$dist/$asset.pem" --certificate-identity-regexp "$identity" --certificate-oidc-issuer "$issuer" "$dist/$asset"
if command -v sha256sum >/dev/null; then (cd "$dist" && grep -F "  $asset" SHA256SUMS | sha256sum -c -); else (cd "$dist" && grep -F "  $asset" SHA256SUMS | shasum -a 256 -c -); fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
if [[ "$suffix" == zip ]]; then unzip -q "$dist/$asset" -d "$tmp"; else tar -xzf "$dist/$asset" -C "$tmp"; fi
for required in "$binary" LICENSE NOTICE README.md THIRD_PARTY_LICENSES.md third_party_licenses; do [[ -e "$tmp/$required" ]] || { echo "archive missing $required" >&2; exit 1; }; done
if [[ "$binary" == komposyn-lg.exe ]]; then
  archive_path="$(cygpath -w "$tmp/$binary")"
  metadata="$tmp/go-version.txt"
  go version -m "$archive_path" > "$metadata"
  grep -Eq 'GOOS=windows' "$metadata" && grep -Eq "GOARCH=${target#windows_}" "$metadata"
  cli_data="$(cygpath -w "$tmp/probe-data")"
  caller_data="$(cygpath -w "$tmp/unrelated-data")"
  mkdir -p "$tmp/probe-data" "$tmp/unrelated-data"
  export TOKEN_LOGGER_DATA_DIR="$caller_data"
  output="$("$archive_path" version)"
  [[ "$output" == *"komposyn-lg $version (core $core_version)"* ]] || { echo "unexpected version metadata: $output" >&2; exit 1; }
  init_request="$(python3 - "$cli_data" <<'PY'
import json
import sys
print(json.dumps({"protocol":"komposyn.token-logger","version":1,"id":"init","method":"initialize","params":{"data_dir":sys.argv[1]}}))
PY
)"
  printf '%s\n' "$init_request" '{"protocol":"komposyn.token-logger","version":1,"id":"stop","method":"shutdown"}' | "$archive_path" harness > "$tmp/protocol.jsonl"
else
  go version -m "$tmp/$binary" > "$tmp/go-version.txt"
  grep -Eq 'GOOS=(linux|darwin)' "$tmp/go-version.txt"
  grep -Eq "GOARCH=${target#*_}" "$tmp/go-version.txt"
  mkdir -p "$tmp/probe-data" "$tmp/unrelated-data"
  export TOKEN_LOGGER_DATA_DIR="$tmp/unrelated-data"
  output="$("$tmp/$binary" version)"
  [[ "$output" == *"komposyn-lg $version (core $core_version)"* ]] || { echo "unexpected version metadata: $output" >&2; exit 1; }
  init_request="$(python3 - "$tmp/probe-data" <<'PY'
import json
import sys
print(json.dumps({"protocol":"komposyn.token-logger","version":1,"id":"init","method":"initialize","params":{"data_dir":sys.argv[1]}}))
PY
)"
  printf '%s\n' "$init_request" '{"protocol":"komposyn.token-logger","version":1,"id":"stop","method":"shutdown"}' | "$tmp/$binary" harness > "$tmp/protocol.jsonl"
fi
grep -q '"version":1' "$tmp/protocol.jsonl" || { echo 'protocol v1 initialize/shutdown failed' >&2; exit 1; }
[[ -f "$tmp/probe-data/token-logger.db" ]] || { echo 'protocol initialize did not use its isolated data directory' >&2; exit 1; }
[[ ! -e "$tmp/unrelated-data/token-logger.db" && ! -e "$tmp/unrelated-data/settings.json" ]] || { echo 'runtime ignored explicit protocol data directory' >&2; exit 1; }
echo "signed runtime smoke passed for $target CLI $version core $core_version (protocol v1; not full lifecycle coverage)"
