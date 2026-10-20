#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: seed-demo.sh /path/to/komposyn-lg /absolute/empty-demo-directory" >&2
  exit 1
fi

cli=$1
work_dir=$2
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

[[ -x "$cli" ]] || { echo "CLI is not executable: $cli" >&2; exit 1; }
version="$("$cli" version)"
[[ "$version" == "komposyn-lg 0.9.0 (core 0.8.0)" ]] || {
  echo "expected signed CLI 0.9.0 with core 0.8.0; got: $version" >&2
  exit 1
}
[[ "$work_dir" == /* && "$work_dir" != / && "$work_dir" != /tmp && "$work_dir" != /home ]] || {
  echo "provide a dedicated absolute work directory, not a broad parent" >&2
  exit 1
}
if [[ -e "$work_dir" ]]; then
  [[ -d "$work_dir" && ! -L "$work_dir" ]] || { echo "work directory must be a real directory" >&2; exit 1; }
  [[ -z "$(find "$work_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]] || {
    echo "work directory must be empty; refusing to overwrite existing files" >&2
    exit 1
  }
else
  mkdir -m 700 -- "$work_dir"
fi

data_dir="$work_dir/data"
mkdir -m 700 -- "$data_dir"
python3 "$script_dir/generate-fixture.py" "$data_dir" | "$cli" harness > "$work_dir/harness-responses.jsonl"

python3 - "$work_dir/harness-responses.jsonl" <<'PY'
import json
import sys

path = sys.argv[1]
try:
    with open(path, encoding="utf-8") as stream:
        responses = [json.loads(line) for line in stream if line.strip()]
except (OSError, json.JSONDecodeError) as error:
    raise SystemExit(f"invalid harness response JSONL: {error}")

if not responses or any(response.get("ok") is not True for response in responses):
    raise SystemExit(f"harness rejected the synthetic fixture; inspect {path}")
shutdown = responses[-1]
if shutdown.get("id") != "shutdown" or shutdown.get("result", {}).get("stopped") is not True:
    raise SystemExit(f"harness did not complete shutdown; inspect {path}")
PY
printf 'Synthetic demo fixture loaded.\nData: %s\nResponses: %s\n' "$data_dir" "$work_dir/harness-responses.jsonl"
