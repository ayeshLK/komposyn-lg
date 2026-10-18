#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
mkdir "$tmp_dir/bin"
cat > "$tmp_dir/bin/uname" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$TEST_COPY_HOST"
STUB
cat > "$tmp_dir/bin/cp" <<'STUB'
#!/usr/bin/env bash
count=0
[[ ! -f "$TEST_COPY_COUNT" ]] || read -r count < "$TEST_COPY_COUNT"
count=$((count + 1))
printf '%s\n' "$count" > "$TEST_COPY_COUNT"
if ((count <= TEST_COPY_FAILURES)); then
  echo 'cp: Device or resource busy' >&2
  exit 7
fi
exec "$TEST_REAL_CP" "$@"
STUB
cat > "$tmp_dir/bin/sleep" <<'STUB'
#!/usr/bin/env bash
[[ "$1" == 0.1 ]]
STUB
chmod +x "$tmp_dir/bin/"*
export TEST_REAL_CP="$(command -v cp)"
export TEST_COPY_COUNT="$tmp_dir/count"
export PATH="$tmp_dir/bin:$PATH"
printf 'verified fixture executable\n' > "$tmp_dir/source"

run_case() {
  local host="$1" failures="$2" expected_status="$3" expected_attempts="$4"
  rm -f "$TEST_COPY_COUNT" "$tmp_dir/destination"
  local status=0
  TEST_COPY_HOST="$host" TEST_COPY_FAILURES="$failures" \
    bash "$root_dir/scripts/copy-release-executable.sh" "$tmp_dir/source" "$tmp_dir/destination" \
    >"$tmp_dir/output" 2>&1 || status=$?
  [[ "$status" == "$expected_status" && "$(cat "$TEST_COPY_COUNT")" == "$expected_attempts" ]] || {
    cat "$tmp_dir/output" >&2
    echo "unexpected copy result for $host: status=$status attempts=$(cat "$TEST_COPY_COUNT")" >&2
    exit 1
  }
  if ((expected_status == 0)); then
    cmp "$tmp_dir/source" "$tmp_dir/destination"
  else
    [[ ! -e "$tmp_dir/destination" ]]
  fi
}

run_case Linux 0 0 1
run_case Linux 1 7 1
run_case Darwin 1 7 1
run_case MINGWARM64_NT-10.0 3 0 4
run_case MINGW64_NT-10.0 50 0 51
run_case MSYS_NT-10.0 51 7 51
run_case CYGWIN_NT-10.0 51 7 51
printf 'release executable copy retry checks passed\n'
