#!/usr/bin/env bash
set -euo pipefail

dist_dir="${1:?usage: verify-release.sh <release-dir> [verified-reference-binary] [expected-target]}"
reference_binary="${2:-}"
expected_target="${3:-}"
root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
version="$(sed -n 's/^# version=//p' "$dist_dir/SHA256SUMS" | head -1)"
if [[ -z "$version" ]]; then
  echo "release checksum manifest does not declare a version" >&2
  exit 1
fi

file_sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "sha256sum or shasum is required" >&2
    return 1
  fi
}

database_schema_version() {
  python3 - "$1" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(f"file:{sys.argv[1]}?mode=ro", uri=True)
try:
    row = connection.execute("SELECT MAX(version) FROM schema_migrations").fetchone()
finally:
    connection.close()
if row is None or row[0] is None:
    raise SystemExit("database has no migration history")
print(row[0])
PY
}

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$dist_dir" && sha256sum -c SHA256SUMS)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$dist_dir" && shasum -a 256 -c SHA256SUMS)
else
  echo "sha256sum or shasum is required" >&2
  exit 1
fi

host_os="$(uname -s)"
host_arch="$(uname -m)"
windows_host=false
case "$host_os:$host_arch" in
  Linux:x86_64) asset="komposyn-lg_${version}_linux_amd64.tar.gz" ;;
  Linux:aarch64|Linux:arm64) asset="komposyn-lg_${version}_linux_arm64.tar.gz" ;;
  Darwin:x86_64) asset="komposyn-lg_${version}_darwin_amd64.tar.gz" ;;
  Darwin:arm64) asset="komposyn-lg_${version}_darwin_arm64.tar.gz" ;;
  MINGW64_NT*:x86_64|MSYS_NT*:x86_64|CYGWIN_NT*:x86_64|MINGWARM64_NT*:aarch64|MINGWARM64_NT*:arm64|MSYS_NT*:aarch64|MSYS_NT*:arm64|CYGWIN_NT*:aarch64|CYGWIN_NT*:arm64)
    windows_arch=amd64
    case "$expected_target" in windows_amd64|windows_arm64) windows_arch="${expected_target#windows_}" ;; esac
    asset="komposyn-lg_${version}_windows_${windows_arch}.zip"
    windows_host=true
    ;;
  *) echo "unsupported verification host: $host_os:$host_arch" >&2; exit 1 ;;
esac

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
if [[ "$windows_host" == true ]]; then
  archive_path="$(cygpath -w "$dist_dir/$asset")"
  extraction_path="$(cygpath -w "$tmp_dir")"
  powershell.exe -NoProfile -File "$root_dir/scripts/expand-release-archive.ps1" \
    -ArchivePath "$archive_path" -DestinationPath "$extraction_path"
  binary_name="komposyn-lg.exe"
else
  tar -xzf "$dist_dir/$asset" -C "$tmp_dir"
  binary_name="komposyn-lg"
fi
for required in "$binary_name" LICENSE NOTICE README.md THIRD_PARTY_LICENSES.md third_party_licenses; do
  [[ -e "$tmp_dir/$required" ]] || { echo "archive missing $required" >&2; exit 1; }
done

install_dir="$tmp_dir/bin"
data_dir="$tmp_dir/data"
mkdir -p "$install_dir" "$data_dir"
binary_path="$install_dir/$binary_name"
run_cli() {
  local cli_data_dir="$data_dir"
  if [[ "$windows_host" == true ]]; then
    cli_data_dir="$(cygpath -w "$data_dir")"
  fi
  TOKEN_LOGGER_DATA_DIR="$cli_data_dir" "$binary_path" "$@"
}
source_dir="$tmp_dir/source"
mkdir -p "$source_dir"
bash "$root_dir/scripts/copy-release-executable.sh" "$tmp_dir/$binary_name" "$binary_path"
chmod 0755 "$binary_path"
target="${asset#komposyn-lg_${version}_}"
target="${target%.tar.gz}"
target="${target%.zip}"
target_os="${target%%_*}"
target_arch="${target#*_}"
if [[ -n "$expected_target" && "$target" != "$expected_target" ]]; then
  echo "release target $target does not match expected native target $expected_target" >&2
  exit 1
fi
metadata_binary="$binary_path"
if [[ "$windows_host" == true ]]; then
  metadata_binary="$(cygpath -w "$binary_path")"
fi
go_metadata="$(go version -m "$metadata_binary")"
[[ "$go_metadata" == *"GOOS=$target_os"* && "$go_metadata" == *"GOARCH=$target_arch"* ]] || {
  echo "release binary architecture metadata does not match $target: $go_metadata" >&2
  exit 1
}
core_module_version="$(awk '$1 == "github.com/ayeshLK/komposyn" { sub(/^v/, "", $2); print $2; exit }' "$root_dir/go.mod")"
candidate_version_output="$("$binary_path" version)"
[[ "$candidate_version_output" == "komposyn-lg $version (core $core_module_version)" ]] || {
  echo "release CLI/core version metadata mismatch: $candidate_version_output" >&2
  exit 1
}

# An optional released binary creates the database and settings for a real
# old-to-new migration. Protocol v1 is stable, so the fixture does not depend
# on private provider data or credentials.
if [[ -n "$reference_binary" ]]; then
  [[ -x "$reference_binary" ]] || { echo "reference binary is not executable: $reference_binary" >&2; exit 1; }
  bash "$root_dir/scripts/copy-release-executable.sh" "$reference_binary" "$binary_path"
  cat > "$source_dir/codex.jsonl" <<'EOF'
{"type":"event_msg","id":"issue-46-cursor-record","timestamp":"2026-10-11T12:00:00Z","payload":{"type":"token_count","session_id":"cursor-session","project_id":"cursor-project","model":"gpt-test","info":{"total_token_usage":{"input_tokens":100,"output_tokens":8}}}}
EOF
  run_cli settings --collector off --auto-start off >/dev/null
  run_cli watch add "$source_dir" >/dev/null
  run_cli import "$source_dir/codex.jsonl" --format json >/dev/null
  run_cli watch start --interval 1s >/dev/null
  run_cli watch stop --timeout 10s >/dev/null
  run_cli harness <<'EOF'
{"protocol":"komposyn.token-logger","version":1,"id":"init","method":"initialize","params":{}}
{"protocol":"komposyn.token-logger","version":1,"id":"record","method":"record","params":{"event":{"event_id":"issue-46-upgrade-fixture","schema_version":1,"event_type":"model.request","occurred_at":"2026-10-11T12:00:00Z","project_id":"issue-46-project","session_id":"issue-46-session","client":{"name":"issue-46-fixture"},"provider":"openai","requested_model":"gpt-test","usage":{"input_tokens":10,"output_tokens":5},"provenance":{"adapter":"fixture","source_id":"issue-46","source_record":"1"},"ingestion_key":"issue-46:upgrade-fixture"},"project":{"id":"issue-46-project","root":"/issue-46-fixture","display_name":"Issue 46 fixture"},"session":{"id":"issue-46-session","project_id":"issue-46-project","client":{"name":"issue-46-fixture"}}}}
{"protocol":"komposyn.token-logger","version":1,"id":"stop","method":"shutdown"}
EOF
  [[ -e "$data_dir/settings.json" && -e "$data_dir/token-logger.db" ]] || {
    echo "reference binary did not create settings and database" >&2
    exit 1
  }
  reference_schema="$(database_schema_version "$data_dir/token-logger.db")"
  # Preserve a user-reviewed diagnostic from the historical schema so the
  # upgrade fixture verifies that review decisions survive the real upgrade.
  python3 - "$data_dir/token-logger.db" <<'PY'
import hashlib
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
try:
    fingerprint = hashlib.sha256(b"issue-46-historical-reviewed-diagnostic").hexdigest()
    connection.execute(
        "INSERT INTO diagnostics(fingerprint, message, first_seen_at, last_seen_at, status) VALUES (?, ?, ?, ?, 'reviewed')",
        (fingerprint, "legacy sanitized diagnostic", "2026-10-11T12:00:00.000000000Z", "2026-10-11T12:00:00.000000000Z"),
    )
    connection.commit()
finally:
    connection.close()
PY
else
  run_cli settings --collector off --auto-start off >/dev/null
  run_cli usage --format json >/dev/null
  run_cli watch add "$source_dir" >/dev/null
  run_cli watch start --interval 1s >/dev/null
  run_cli watch status --format json | grep -Eq '"state":"(running|degraded)"' || {
    echo "background collector did not start during release verification" >&2
    exit 1
  }
fi
[[ -e "$data_dir/settings.json" ]] || { echo "settings were not created" >&2; exit 1; }
[[ -e "$data_dir/token-logger.db" ]] || { echo "database was not created" >&2; exit 1; }
settings_digest=""
if [[ -n "$reference_binary" ]]; then
  settings_digest="$(file_sha256 "$data_dir/settings.json")"
fi

# Replacing the executable exercises a real released-binary upgrade when the
# optional fixture is supplied, and remains a basic lifecycle smoke test
# otherwise.
if [[ -z "$reference_binary" ]]; then
  run_cli watch stop --timeout 10s >/dev/null
fi
bash "$root_dir/scripts/copy-release-executable.sh" "$tmp_dir/$binary_name" "$binary_path"
if [[ -n "$reference_binary" ]]; then
  upgraded_report="$(run_cli usage --format json)"
  grep -Eq '"total_tokens"[[:space:]]*:[[:space:]]*123' <<<"$upgraded_report" || {
    echo "historical usage total was not preserved by upgrade: $upgraded_report" >&2
    exit 1
  }
  upgraded_schema="$(database_schema_version "$data_dir/token-logger.db")"
  if ((upgraded_schema < reference_schema)); then
    echo "upgrade lowered database schema from $reference_schema to $upgraded_schema" >&2
    exit 1
  fi
  migration_required=false
  if ((upgraded_schema > reference_schema)); then
    migration_required=true
    compgen -G "$data_dir/.komposyn-pre-migration-*.sqlite" >/dev/null || {
      echo "schema-advancing upgrade did not retain a verified migration backup" >&2
      exit 1
    }
  elif compgen -G "$data_dir/.komposyn-pre-migration-*.sqlite" >/dev/null; then
    echo "same-schema upgrade unexpectedly created a migration backup" >&2
    exit 1
  fi
  [[ "$(file_sha256 "$data_dir/settings.json")" == "$settings_digest" ]] || {
    echo "settings or consent values changed during upgrade" >&2
    exit 1
  }
  upgraded_sources="$(run_cli watch list --format json)"
  source_path_match="$source_dir"
  if [[ "$windows_host" == true ]]; then
    source_path_match="$(cygpath -w "$source_dir")"
    source_path_match="${source_path_match//\\/\\\\}"
  fi
  [[ "$upgraded_sources" == *"$source_path_match"* ]] || {
    echo "registered collector source was not preserved by upgrade" >&2
    exit 1
  }
  [[ "$upgraded_report" == *'"calculation_basis": "unavailable"'* ]] || {
    echo "unknown pricing evidence changed during upgrade" >&2
    exit 1
  }
  reimport_result="$(run_cli import "$source_dir/codex.jsonl" --format json)"
  [[ "$reimport_result" == *'"imported_events":0'* && "$reimport_result" == *'"duplicate_events":0'* ]] || {
    echo "source cursor was not preserved for the unchanged fixture: $reimport_result" >&2
    exit 1
  }
  upgraded_projects="$(run_cli projects list --format json)"
  [[ "$upgraded_projects" == *'issue-46-project'* ]] || {
    echo "project identity was not preserved by upgrade" >&2
    exit 1
  }
  upgraded_sessions="$(run_cli sessions list --project issue-46-project --format json)"
  [[ "$upgraded_sessions" == *'issue-46-session'* ]] || {
    echo "session identity was not preserved by upgrade" >&2
    exit 1
  }
  upgraded_collector="$(run_cli watch status --format json)"
  [[ "$upgraded_collector" != *'"state":"running"'* && "$upgraded_collector" != *'"state":"degraded"'* ]] || {
    echo "collector unexpectedly restarted after a stopped historical run" >&2
    exit 1
  }
  python3 - "$data_dir/token-logger.db" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
try:
    row = connection.execute(
        "SELECT terminal_state FROM collector_runs ORDER BY started_at DESC LIMIT 1"
    ).fetchone()
    diagnostic = connection.execute(
        "SELECT status FROM diagnostics WHERE message = 'legacy sanitized diagnostic'"
    ).fetchone()
finally:
    connection.close()
if row is None or row[0] != "stopped":
    raise SystemExit(f"collector run history was not preserved: {row!r}")
if diagnostic is None or diagnostic[0] != "reviewed":
    raise SystemExit(f"historical diagnostic review state was not preserved: {diagnostic!r}")
PY
  run_cli watch start --interval 1s >/dev/null
  restarted_collector="$(run_cli watch status --format json)"
  [[ "$restarted_collector" == *'"state":"running"'* || "$restarted_collector" == *'"state":"degraded"'* ]] || {
    echo "candidate could not restart a collector after historical active run: $restarted_collector" >&2
    exit 1
  }
  run_cli watch stop --timeout 10s >/dev/null
  python3 - "$data_dir/token-logger.db" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
try:
    rows = connection.execute(
        "SELECT terminal_state FROM collector_runs ORDER BY started_at"
    ).fetchall()
finally:
    connection.close()
if len(rows) < 2 or any(row[0] != "stopped" for row in rows):
    raise SystemExit(f"historical collector restart was not recorded as stopped runs: {rows!r}")
PY
  if [[ "$migration_required" == true ]]; then
    migration_backup="$(compgen -G "$data_dir/.komposyn-pre-migration-*.sqlite" | head -1)"
    recovery_dir="$tmp_dir/restored-data"
    mkdir -p "$recovery_dir"
    cp "$migration_backup" "$recovery_dir/token-logger.db"
    backup_digest="$(file_sha256 "$migration_backup")"
    recovery_data_dir="$recovery_dir"
    if [[ "$windows_host" == true ]]; then
      recovery_data_dir="$(cygpath -w "$recovery_dir")"
    fi
    restored_report="$(TOKEN_LOGGER_DATA_DIR="$recovery_data_dir" "$reference_binary" usage --format json)"
    grep -Eq '"total_tokens"[[:space:]]*:[[:space:]]*123' <<<"$restored_report" || {
      echo "compatible reference binary could not read the restored pre-migration backup" >&2
      exit 1
    }
  fi
else
  run_cli watch start >/dev/null
fi
[[ -e "$data_dir/settings.json" && -e "$data_dir/token-logger.db" ]] || {
  echo "upgrade did not preserve local data" >&2
  exit 1
}

# Exercise the standalone CLI against a synthetic newer schema in an isolated
# directory. Core must refuse it before mutation or SQLite sidecar creation.
refusal_dir="$tmp_dir/newer-schema-data"
mkdir -p "$refusal_dir"
refusal_env_dir="$refusal_dir"
if [[ "$windows_host" == true ]]; then
  refusal_env_dir="$(cygpath -w "$refusal_dir")"
fi
TOKEN_LOGGER_DATA_DIR="$refusal_env_dir" "$binary_path" settings --collector off --auto-start off >/dev/null
TOKEN_LOGGER_DATA_DIR="$refusal_env_dir" "$binary_path" usage --format json >/dev/null
protocol_output="$(printf '%s\n' '{"protocol":"komposyn.token-logger","version":1,"id":"init","method":"initialize","params":{}}' '{"protocol":"komposyn.token-logger","version":1,"id":"stop","method":"shutdown"}' | TOKEN_LOGGER_DATA_DIR="$refusal_env_dir" "$binary_path" harness)"
[[ "$protocol_output" == *'"version":1'* ]] || {
  echo "standalone runtime protocol v1 smoke failed: $protocol_output" >&2
  exit 1
}
python3 - "$refusal_dir/token-logger.db" <<'PY'
import sqlite3
import sys

connection = sqlite3.connect(sys.argv[1])
try:
    connection.execute("UPDATE schema_migrations SET version = 16 WHERE version = (SELECT MAX(version) FROM schema_migrations)")
    connection.commit()
finally:
    connection.close()
PY
refusal_digest="$(file_sha256 "$refusal_dir/token-logger.db")"
if TOKEN_LOGGER_DATA_DIR="$refusal_env_dir" "$binary_path" usage --format json >"$tmp_dir/newer-schema-output.txt" 2>&1; then
  echo "standalone CLI unexpectedly opened a newer unsupported database schema" >&2
  exit 1
fi
[[ "$(file_sha256 "$refusal_dir/token-logger.db")" == "$refusal_digest" ]] || {
  echo "newer-schema refusal changed the database bytes" >&2
  exit 1
}
for sidecar in token-logger.db-wal token-logger.db-shm; do
  [[ ! -e "$refusal_dir/$sidecar" ]] || {
    echo "newer-schema refusal created SQLite sidecar $sidecar" >&2
    exit 1
  }
done
compgen -G "$refusal_dir/.komposyn-pre-migration-*.sqlite" >/dev/null && {
  echo "newer-schema refusal created a migration backup" >&2
  exit 1
}
if [[ "$windows_host" == true ]]; then
  win_install_dir="$(cygpath -w "$install_dir")"
  win_data_dir="$(cygpath -w "$data_dir")"
  win_uninstaller="$(cygpath -w "$root_dir/scripts/uninstall.ps1")"
  TOKEN_LOGGER_DATA_DIR="$win_data_dir" KOMPOSYN_INSTALL_DIR="$win_install_dir" powershell.exe -NoProfile -File "$win_uninstaller" >/dev/null
else
  TOKEN_LOGGER_DATA_DIR="$data_dir" KOMPOSYN_INSTALL_DIR="$install_dir" bash "$root_dir/scripts/uninstall.sh" >/dev/null
fi
[[ ! -e "$binary_path" ]] || { echo "uninstall left executable behind" >&2; exit 1; }
[[ -e "$data_dir/settings.json" && -e "$data_dir/token-logger.db" ]] || {
  echo "uninstall removed local data" >&2
  exit 1
}
if [[ -n "$reference_binary" && "$migration_required" == true ]]; then
  [[ -f "$migration_backup" ]] || {
    echo "uninstall removed the migration backup" >&2
    exit 1
  }
  [[ "$(file_sha256 "$migration_backup")" == "$backup_digest" ]] || {
    echo "uninstall changed the migration backup" >&2
    exit 1
  }
fi
echo "release install, upgrade, and data-preserving uninstall checks passed"
