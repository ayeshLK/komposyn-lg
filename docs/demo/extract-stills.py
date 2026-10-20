#!/usr/bin/env python3
"""Extract named stills from frames of the actual rendered terminal capture."""

import argparse
from pathlib import Path

from PIL import Image, ImageChops


FRAMES = {
    "overall-report.png": 0,
    "project-session-navigation.png": 2,
    "pricing-explanation.png": 4,
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("gif", type=Path)
    parser.add_argument("output_dir", type=Path)
    args = parser.parse_args()

    with Image.open(args.gif) as recording:
        args.output_dir.mkdir(parents=True, exist_ok=True)
        for name, frame_index in FRAMES.items():
            if frame_index >= recording.n_frames:
                parser.error(f"recording has no frame {frame_index} for {name}")
            recording.seek(frame_index)
            frame = recording.convert("RGB")
            background = Image.new("RGB", frame.size, frame.getpixel((0, 0)))
            difference = ImageChops.difference(frame, background).convert("L")
            bounds = difference.point(lambda value: 255 if value > 10 else 0).getbbox()
            if bounds is None:
                parser.error(f"frame {frame_index} is unexpectedly blank")
            left, top, right, bottom = bounds
            margin = 12
            crop = (
                max(0, left - margin),
                max(0, top - margin),
                min(frame.width, right + margin),
                min(frame.height, bottom + margin),
            )
            frame.crop(crop).save(args.output_dir / name, format="PNG", optimize=True)


if __name__ == "__main__":
    main()
