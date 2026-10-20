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

if ! rg -q '"id":"shutdown".*"stopped":true' "$work_dir/harness-responses.jsonl"; then
  echo "harness did not complete shutdown; inspect $work_dir/harness-responses.jsonl" >&2
  exit 1
fi
if rg -q '"ok":false' "$work_dir/harness-responses.jsonl"; then
  echo "harness rejected the synthetic fixture; inspect $work_dir/harness-responses.jsonl" >&2
  exit 1
fi
printf 'Synthetic demo fixture loaded.\nData: %s\nResponses: %s\n' "$data_dir" "$work_dir/harness-responses.jsonl"
