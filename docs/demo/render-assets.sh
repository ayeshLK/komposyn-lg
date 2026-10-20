#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: render-assets.sh /path/to/komposyn-lg-demo.cast /path/to/output-directory" >&2
  exit 1
fi

cast_file=$1
output_dir=$2
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
agg_bin="${AGG_BIN:-agg}"

[[ -f "$cast_file" ]] || { echo "cast file not found: $cast_file" >&2; exit 1; }
[[ "$output_dir" == /* && "$output_dir" != / && "$output_dir" != /tmp && "$output_dir" != /home ]] || {
  echo "output must be a dedicated absolute directory, not a broad parent" >&2
  exit 1
}
if [[ -e "$output_dir" ]]; then
  [[ -d "$output_dir" && ! -L "$output_dir" ]] || { echo "output must be a real directory" >&2; exit 1; }
  [[ -z "$(find "$output_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]] || {
    echo "output directory must be empty; refusing to overwrite existing files" >&2
    exit 1
  }
else
  mkdir -m 700 -- "$output_dir"
fi

"$agg_bin" --quiet --cols 110 --rows 45 --font-size 12 --speed 1 \
  --idle-time-limit 1 --fps-cap 12 --last-frame-duration 2 --theme github-dark \
  "$cast_file" "$output_dir/komposyn-lg-demo.gif"
python3 "$script_dir/extract-stills.py" "$output_dir/komposyn-lg-demo.gif" "$output_dir"
