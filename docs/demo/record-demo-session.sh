#!/usr/bin/env bash
set -euo pipefail

cli_bin="${CLI_BIN:-komposyn-lg}"
data_dir="${TOKEN_LOGGER_DATA_DIR:-}"
export_path="${DEMO_EXPORT_PATH:-}"
from_date=2026-10-18
to_date=2026-10-21

if [[ -z "$data_dir" || ! -f "$data_dir/token-logger.db" ]]; then
  echo "TOKEN_LOGGER_DATA_DIR must point to the seeded synthetic demo database" >&2
  exit 1
fi
if [[ -z "$export_path" || "$export_path" != /* || -e "$export_path" ]]; then
  echo "DEMO_EXPORT_PATH must be a new absolute file path for the exact JSON export" >&2
  exit 1
fi
if [[ "$("$cli_bin" version)" != "komposyn-lg 0.9.0 (core 0.8.0)" ]]; then
  echo "this recording script is verified for signed CLI 0.9.0 with core 0.8.0" >&2
  exit 1
fi

scene() {
  local title="$1"
  local display_cli="komposyn-lg"
  shift
  printf '\033[2J\033[H'
  printf 'SYNTHETIC EXAMPLE DATA  ·  %s\n\n' "$title"
  if [[ "$1" == "$cli_bin" ]]; then
    shift
    printf '$ %s' "$display_cli"
    printf ' %q' "$@"
    printf '\n'
    "$cli_bin" "$@"
  else
    printf '$'
    printf ' %q' "$@"
    printf '\n'
    "$@"
  fi
}

scene "Overall usage" "$cli_bin" usage --from "$from_date" --to "$to_date" --format table
sleep 5
scene "Projects" "$cli_bin" projects list --from "$from_date" --to "$to_date" --format table
sleep 5
scene "Sessions in Ledger Studio" "$cli_bin" sessions list --project project-ledger --from "$from_date" --to "$to_date" --format table
sleep 5
scene "Models in one Codex session" "$cli_bin" sessions show session-ledger-codex --project project-ledger --from "$from_date" --to "$to_date" --group-by model --format table
sleep 5
scene "Cost and pricing coverage" "$cli_bin" cost --from "$from_date" --to "$to_date" --group-by client,provider,model --format table
sleep 5

printf '\033[2J\033[H'
printf 'SYNTHETIC EXAMPLE DATA  ·  Exact machine export\n\n'
printf '$ komposyn-lg usage --from %s --to %s --group-by client,provider,model --format json > usage.json\n' "$from_date" "$to_date"
"$cli_bin" usage --from "$from_date" --to "$to_date" --group-by client,provider,model --format json > "$export_path"
python3 - "$export_path" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as stream:
    summary = json.load(stream)["summary"]
print("Exact export saved: input_tokens={}; calculated_cost_micros={}; unknown_cost_requests={}".format(
    summary["input_tokens"],
    summary["calculated_cost_micros"],
    summary["unknown_cost_requests"],
))
PY
sleep 5
