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

old_tag="${1:-v0.6.0}"
candidate_dir="${2:?usage: verify-active-signed-candidate-upgrade.sh <historical-tag> <signed-candidate-dir>}"
repo='ayeshLK/komposyn'
identity='^https://github.com/ayeshLK/komposyn/.github/workflows/release.yml@refs/heads/master$'
candidate_identity='^https://github.com/ayeshLK/komposyn-lg/.github/workflows/release.yml@refs/heads/master$'
if [[ "$old_tag" == "v0.8.1" ]]; then
	repo='ayeshLK/komposyn-lg'
	identity="$candidate_identity"
fi
issuer='https://token.actions.githubusercontent.com'
host_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
host_arch="$(uname -m)"
case "$host_os:$host_arch" in
	linux:x86_64|linux:amd64) target="linux_amd64" ;;
	linux:aarch64|linux:arm64) target="linux_arm64" ;;
	darwin:x86_64) target="darwin_amd64" ;;
	darwin:arm64) target="darwin_arm64" ;;
	*) echo "unsupported native Unix verification host: $host_os:$host_arch" >&2; exit 1 ;;
esac

for tool in gh cosign python3 jq; do
	command -v "$tool" >/dev/null 2>&1 || { echo "$tool is required" >&2; exit 1; }
done
if command -v sha256sum >/dev/null 2>&1; then
	sha_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then
	sha_tool=shasum
else
	echo 'sha256sum or shasum is required' >&2
	exit 1
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/komposyn-active-upgrade.XXXXXX")"
old_version="${old_tag#v}"
old_asset="komposyn-lg_${old_version}_${target}.tar.gz"
candidate_version="$(sed -n 's/^# version=//p' "$candidate_dir/SHA256SUMS" | head -1)"
candidate_version="${candidate_version#v}"
candidate_asset="komposyn-lg_${candidate_version}_${target}.tar.gz"
old_dir="$tmp_dir/old-release"
install_dir="$tmp_dir/bin"
data_dir="$tmp_dir/data"
source_dir="$tmp_dir/source"
mkdir -p "$old_dir" "$install_dir" "$data_dir" "$source_dir"
# macOS exposes temporary directories through symlinked paths (for example,
# /var/folders -> /private/var/folders). The CLI resolves registered source
# paths, so use the physical path for both registration and the preservation
# assertion below.
source_dir="$(cd "$source_dir" && pwd -P)"
cleanup() {
	if [[ -x "$install_dir/komposyn-lg" && -f "$data_dir/token-logger.db" ]]; then
		TOKEN_LOGGER_DATA_DIR="$data_dir" "$install_dir/komposyn-lg" watch stop --timeout 10s >/dev/null 2>&1 || true
	fi
	rm -rf "$tmp_dir"
}
trap cleanup EXIT

for candidate_file in SHA256SUMS SHA256SUMS.sig SHA256SUMS.pem "$candidate_asset" "$candidate_asset.sig" "$candidate_asset.pem"; do
	[[ -f "$candidate_dir/$candidate_file" ]] || { echo "missing signed candidate file: $candidate_file" >&2; exit 1; }
done
cosign verify-blob --signature "$candidate_dir/SHA256SUMS.sig" --certificate "$candidate_dir/SHA256SUMS.pem" \
	--certificate-identity-regexp "$candidate_identity" --certificate-oidc-issuer "$issuer" "$candidate_dir/SHA256SUMS"
cosign verify-blob --signature "$candidate_dir/$candidate_asset.sig" --certificate "$candidate_dir/$candidate_asset.pem" \
	--certificate-identity-regexp "$candidate_identity" --certificate-oidc-issuer "$issuer" "$candidate_dir/$candidate_asset"
if [[ "$sha_tool" == sha256sum ]]; then
	(cd "$candidate_dir" && grep "  ${candidate_asset}$" SHA256SUMS | sha256sum -c -)
else
	(cd "$candidate_dir" && grep "  ${candidate_asset}$" SHA256SUMS | shasum -a 256 -c -)
fi

gh release download "$old_tag" --repo "$repo" --dir "$old_dir" \
	--pattern "$old_asset" --pattern SHA256SUMS --pattern SHA256SUMS.sig --pattern SHA256SUMS.pem \
	--pattern "$old_asset.sig" --pattern "$old_asset.pem"
if [[ "$sha_tool" == sha256sum ]]; then
	(cd "$old_dir" && grep "  ${old_asset}$" SHA256SUMS | sha256sum -c -)
else
	(cd "$old_dir" && grep "  ${old_asset}$" SHA256SUMS | shasum -a 256 -c -)
fi
cosign verify-blob --signature "$old_dir/$old_asset.sig" --certificate "$old_dir/$old_asset.pem" \
	--certificate-identity-regexp "$identity" --certificate-oidc-issuer "$issuer" "$old_dir/$old_asset"
cosign verify-blob --signature "$old_dir/SHA256SUMS.sig" --certificate "$old_dir/SHA256SUMS.pem" \
	--certificate-identity-regexp "$identity" --certificate-oidc-issuer "$issuer" "$old_dir/SHA256SUMS"
tar -xzf "$old_dir/$old_asset" -C "$old_dir"
cp "$old_dir/komposyn-lg" "$install_dir/komposyn-lg"
chmod 0755 "$install_dir/komposyn-lg"
binary="$install_dir/komposyn-lg"

run_old() { TOKEN_LOGGER_DATA_DIR="$data_dir" "$binary" "$@"; }
fixture="$(cd "$(dirname "${BASH_SOURCE[0]}")/../plugins/library/token-logger/testdata/codex" && pwd)/normal.jsonl"
cp "$fixture" "$source_dir/normal.jsonl"
run_old settings --collector off --auto-start off >/dev/null
run_old watch add "$source_dir" >/dev/null
run_old import "$source_dir/normal.jsonl" --format json >/dev/null
if [[ "$old_tag" == "v0.8.1" ]]; then
	python3 - "$data_dir/token-logger.db" <<'PY'
import sqlite3
import sys
db = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
try:
    version = db.execute("SELECT MAX(version) FROM schema_migrations").fetchone()[0]
finally:
    db.close()
if version != 15:
    raise SystemExit(f"signed CLI v0.8.1 seeded schema {version}; expected 15")
PY
fi
run_old harness <<'EOF' >/dev/null
{"protocol":"komposyn.token-logger","version":1,"id":"init","method":"initialize","params":{}}
{"protocol":"komposyn.token-logger","version":1,"id":"record","method":"record","params":{"event":{"event_id":"issue-46-active-id","schema_version":1,"event_type":"model.request","occurred_at":"2026-10-11T12:00:00Z","project_id":"issue-46-active-project","session_id":"issue-46-active-session","client":{"name":"issue-46-active"},"provider":"openai","requested_model":"gpt-test","usage":{"input_tokens":7,"output_tokens":3},"provenance":{"adapter":"fixture","source_id":"issue-46-active","source_record":"1"},"ingestion_key":"issue-46:active-id"},"project":{"id":"issue-46-active-project","root":"/issue-46-active-fixture","display_name":"Issue 46 active fixture"},"session":{"id":"issue-46-active-session","project_id":"issue-46-active-project","client":{"name":"issue-46-active"}}}}
{"protocol":"komposyn.token-logger","version":1,"id":"stop","method":"shutdown"}
EOF
python3 - "$data_dir/token-logger.db" <<'PY'
import hashlib
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
try:
    fingerprint = hashlib.sha256(b"issue-46-active-reviewed-diagnostic").hexdigest()
    connection.execute(
        "INSERT INTO diagnostics(fingerprint, message, first_seen_at, last_seen_at, status) VALUES (?, ?, ?, ?, 'reviewed')",
        (fingerprint, "legacy sanitized diagnostic", "2026-10-11T12:00:00.000000000Z", "2026-10-11T12:00:00.000000000Z"),
    )
    connection.commit()
finally:
    connection.close()
PY
before_report="$(run_old usage --format json)"
before_usage="$(jq -c '.summary | {request_count,input_tokens,cached_input_tokens,cache_write_input_tokens,output_tokens,reasoning_output_tokens,total_tokens}' <<<"$before_report")"
run_old watch start --interval 1s >/dev/null

old_status=''
for _ in {1..50}; do
	old_status="$(run_old watch status --format json)"
	if [[ "$(jq -r '.state' <<<"$old_status")" == running ]]; then
		break
	fi
	sleep 0.1
done
old_pid="$(jq -er 'select(.state == "running" and .mode == "background") | .pid | select(. > 0)' <<<"$old_status")" || {
	echo "historical collector did not become active: $old_status" >&2
	exit 1
}
old_instance="$(jq -er '.instance_id' <<<"$old_status")"
settings_digest="$("$sha_tool" "$data_dir/settings.json" | awk '{print $1}')"

export KOMPOSYN_VERSION="v$candidate_version" KOMPOSYN_RELEASE_DIR="$candidate_dir" KOMPOSYN_INSTALL_DIR="$install_dir"
export TOKEN_LOGGER_DATA_DIR="$data_dir"
install_output="$(bash "$(dirname "${BASH_SOURCE[0]}")/install.sh")"
[[ "$install_output" == *'restarted the background collector after upgrade'* ]] || {
	echo "signature-verifying installer did not report collector restart: $install_output" >&2
	exit 1
}
new_status="$("$binary" watch status --format json)"
new_pid="$(jq -er 'select(.state == "running" and .mode == "background" and .stale == false) | .pid | select(. > 0)' <<<"$new_status")" || {
	echo "upgraded collector is not running in background mode: $new_status" >&2
	exit 1
}
new_instance="$(jq -er '.instance_id' <<<"$new_status")"
[[ "$new_pid" != "$old_pid" && "$new_instance" != "$old_instance" ]] || {
	echo "collector did not stop and restart with a new process and lease identity" >&2
	exit 1
}
kill -0 "$new_pid" 2>/dev/null || { echo 'restarted collector process is not alive' >&2; exit 1; }
if kill -0 "$old_pid" 2>/dev/null; then
	echo 'historical collector process survived the installer cooperative stop' >&2
	exit 1
fi
new_version="$($binary version)"
[[ "$new_version" == *"komposyn-lg $candidate_version (core 0.8.0)"* ]] || {
	echo "candidate did not replace the historical executable: $new_version" >&2
	exit 1
}
after_report="$("$binary" usage --format json)"
after_usage="$(jq -c '.summary | {request_count,input_tokens,cached_input_tokens,cache_write_input_tokens,output_tokens,reasoning_output_tokens,total_tokens}' <<<"$after_report")"
[[ "$after_usage" == "$before_usage" ]] || {
	echo "usage totals changed during upgrade: before=$before_usage after=$after_usage" >&2
	exit 1
}
[[ "$("$sha_tool" "$data_dir/settings.json" | awk '{print $1}')" == "$settings_digest" ]] || {
	echo 'settings or consent changed during upgrade' >&2
	exit 1
}
python3 - "$data_dir/token-logger.db" "$new_pid" "$new_instance" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
try:
    rows = connection.execute(
        "SELECT pid, instance_id, state, mode FROM collector_runtime"
    ).fetchall()
finally:
    connection.close()
expected = [(int(sys.argv[2]), sys.argv[3], "running", "background")]
if rows != expected:
    raise SystemExit(f"expected one healthy lease owner {expected!r}, found {rows!r}")
PY
[[ "$("$binary" projects list --format json)" == *'issue-46-active-project'* ]] || {
	echo 'project identity was not preserved by upgrade' >&2
	exit 1
}
[[ "$("$binary" sessions list --project issue-46-active-project --format json)" == *'issue-46-active-session'* ]] || {
	echo 'session identity was not preserved by upgrade' >&2
	exit 1
}
reimport_result="$("$binary" import "$source_dir/normal.jsonl" --format json)"
[[ "$reimport_result" == *'"imported_events":0'* && "$reimport_result" == *'"duplicate_events":0'* ]] || {
	echo "source cursor was not preserved: $reimport_result" >&2
	exit 1
}
registered_sources="$("$binary" watch list --format json)"
jq -e --arg path "$source_dir" '.sources | any(.[]; .path == $path)' <<<"$registered_sources" >/dev/null || {
	echo "collector registry was not preserved: expected path=$source_dir, sources=$registered_sources" >&2
	exit 1
}
python3 - "$data_dir/token-logger.db" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
try:
    diagnostic = connection.execute(
        "SELECT status FROM diagnostics WHERE message = 'legacy sanitized diagnostic'"
    ).fetchone()
    stopped = connection.execute(
        "SELECT COUNT(*) FROM collector_runs WHERE terminal_state = 'stopped'"
    ).fetchone()[0]
finally:
    connection.close()
if diagnostic is None or diagnostic[0] != "reviewed":
    raise SystemExit(f"diagnostic review state was not preserved: {diagnostic!r}")
if stopped < 1:
    raise SystemExit(f"historical cooperative stop was not recorded: {stopped}")
PY
backup="$(compgen -G "$data_dir/.komposyn-pre-migration-*.sqlite" | head -1)"
[[ -n "$backup" && -f "$backup" ]] || { echo 'v0.6.0 upgrade did not create a migration backup' >&2; exit 1; }
if [[ "$old_tag" == "v0.8.1" ]]; then
	python3 - "$data_dir/token-logger.db" "$backup" <<'PY'
import sqlite3
import sys
def read(path, query):
    db = sqlite3.connect(f"file:{path}?mode=ro", uri=True)
    try:
        return db.execute(query).fetchone()
    finally:
        db.close()
new_schema = read(sys.argv[1], "SELECT MAX(version) FROM schema_migrations")[0]
backup_integrity = read(sys.argv[2], "PRAGMA integrity_check")[0]
source, target = read(sys.argv[2], "SELECT source_schema,target_schema FROM komposyn_migration_backup")
backup_schema = read(sys.argv[2], "SELECT MAX(version) FROM schema_migrations")[0]
if (new_schema, backup_integrity, source, target, backup_schema) != (16, "ok", 15, 16, 15):
    raise SystemExit(f"schema-15-to-16 backup mismatch: {(new_schema, backup_integrity, source, target, backup_schema)!r}")
PY
fi
backup_digest="$("$sha_tool" "$backup" | awk '{print $1}')"
recovery_dir="$tmp_dir/restored-data"
mkdir -p "$recovery_dir"
cp "$backup" "$recovery_dir/token-logger.db"
restored_report="$(TOKEN_LOGGER_DATA_DIR="$recovery_dir" "$old_dir/komposyn-lg" usage --format json)"
grep -Eq '"total_tokens"[[:space:]]*:[[:space:]]*[0-9]+' <<<"$restored_report" || {
	echo 'historical v0.6.0 executable could not read the restored migration backup' >&2
	exit 1
}
restored_usage="$(jq -c '.summary | {request_count,input_tokens,cached_input_tokens,cache_write_input_tokens,output_tokens,reasoning_output_tokens,total_tokens}' <<<"$restored_report")"
[[ "$restored_usage" == "$before_usage" ]] || {
	echo "restored historical usage differs from seeded totals: expected=$before_usage actual=$restored_usage" >&2
	exit 1
}
if [[ "$old_tag" == "v0.8.1" ]]; then
	"$binary" watch stop --timeout 10s >/dev/null
	refusal_before="$(python3 - "$data_dir/token-logger.db" <<'PY'
import hashlib
import pathlib
import sys
database = pathlib.Path(sys.argv[1])
for suffix in ("", "-wal", "-shm"):
    path = pathlib.Path(str(database) + suffix)
    print(f"{suffix}:{hashlib.sha256(path.read_bytes()).hexdigest()}" if path.is_file() else f"{suffix}:missing")
PY
)"
	if TOKEN_LOGGER_DATA_DIR="$data_dir" "$old_dir/komposyn-lg" usage --format json >"$tmp_dir/old-schema-refusal.txt" 2>&1; then
		echo 'signed CLI v0.8.1 unexpectedly opened the migrated schema-16 database' >&2
		exit 1
	fi
	grep -Fq 'newer than supported version' "$tmp_dir/old-schema-refusal.txt" || {
		echo "signed CLI v0.8.1 failed for an unexpected reason: $(cat "$tmp_dir/old-schema-refusal.txt")" >&2
		exit 1
	}
	refusal_after="$(python3 - "$data_dir/token-logger.db" <<'PY'
import hashlib
import pathlib
import sys
database = pathlib.Path(sys.argv[1])
for suffix in ("", "-wal", "-shm"):
    path = pathlib.Path(str(database) + suffix)
    print(f"{suffix}:{hashlib.sha256(path.read_bytes()).hexdigest()}" if path.is_file() else f"{suffix}:missing")
PY
)"
	[[ "$refusal_after" == "$refusal_before" ]] || {
		echo "signed CLI v0.8.1 refusal mutated schema-16 database/sidecars: before=$refusal_before after=$refusal_after" >&2
		exit 1
	}
fi
if [[ "$old_tag" != "v0.8.1" ]]; then
	"$binary" watch stop --timeout 10s >/dev/null
fi
bash "$(dirname "${BASH_SOURCE[0]}")/uninstall.sh" >/dev/null
[[ ! -e "$binary" && -f "$data_dir/token-logger.db" && -f "$data_dir/settings.json" ]] || {
	echo 'uninstall failed to remove the binary or preserve local data' >&2
	exit 1
}
[[ -f "$backup" && "$("$sha_tool" "$backup" | awk '{print $1}')" == "$backup_digest" ]] || {
	echo 'uninstall removed or changed the migration backup' >&2
	exit 1
}
echo "signed historical $old_tag to standalone v$candidate_version active-collector upgrade passed on $target"
