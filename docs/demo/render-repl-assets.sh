#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: render-repl-assets.sh /path/to/repl.cast /path/to/output-directory" >&2
  exit 1
fi

cast_file=$1
output_dir=$2
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

"$agg_bin" --quiet --cols 150 --rows 32 --font-size 11 --speed 1 \
  --idle-time-limit 1 --fps-cap 10 --last-frame-duration 2 --theme github-dark \
  "$cast_file" "$output_dir/komposyn-lg-repl-demo.gif"
python3 - "$output_dir/komposyn-lg-repl-demo.gif" "$output_dir/repl-model-view.png" <<'PY'
import sys
from PIL import Image, ImageChops

with Image.open(sys.argv[1]) as recording:
    recording.seek(recording.n_frames - 1)
    frame = recording.convert("RGB")
    background = Image.new("RGB", frame.size, frame.getpixel((0, 0)))
    difference = ImageChops.difference(frame, background).convert("L")
    bounds = difference.point(lambda value: 255 if value > 10 else 0).getbbox()
    if bounds is None:
        raise SystemExit("final REPL frame is unexpectedly blank")
    left, top, right, bottom = bounds
    margin = 12
    crop = (
        max(0, left - margin),
        max(0, top - margin),
        min(frame.width, right + margin),
        min(frame.height, bottom + margin),
    )
    frame.crop(crop).save(sys.argv[2], format="PNG", optimize=True)
PY
