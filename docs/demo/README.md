# KompoSyn CLI demonstration assets

These assets demonstrate komposyn-lg v0.9.0 with core v0.8.0 and entirely synthetic Codex/Claude Code usage. They are intended for review and for Agent 1 to embed selected images in the repository README. This change deliberately does not modify the README or publish social-media content.

## Reproduce the fixture and recording

The checked-in cast was captured from the signed v0.9.0 Linux amd64 release, not a development build. Its source release is [ayeshLK/komposyn-lg v0.9.0](https://github.com/ayeshLK/komposyn-lg/releases/tag/v0.9.0); it pins core v0.8.0. The release archive was verified against the signed SHA256SUMS manifest using Cosign's keyless GitHub Actions identity. The verified Linux amd64 archive SHA-256 was:

    1ef7cf6fa9fae6976dc44bbaf62fb81027c2c89a02ca6e129f32dffa01c79084

Requirements to regenerate:

- Linux/macOS shell, Python 3, and the signed v0.9.0 executable.
- For stills and GIF: asciinema 3.2.1, agg 1.9.0, Pillow 10.2.0.
- For a caption-burned MP4: ffmpeg built with libass/subtitles and an H.264 encoder. That encoder was unavailable in the capture environment, so no MP4 is claimed as delivered. The original 30-second asciicast and synchronized WebVTT captions are included; the GIF is a shortened looping visual preview, not a replacement for that narrated-duration recording.

After obtaining and verifying the release binary, run from a clean checkout:

    mkdir -m 700 /tmp/komposyn-demo-data
    export CLI_BIN=/path/to/komposyn-lg
    docs/demo/seed-demo.sh "$CLI_BIN" /tmp/komposyn-demo-data
    export TOKEN_LOGGER_DATA_DIR=/tmp/komposyn-demo-data/data
    export DEMO_EXPORT_PATH=/tmp/komposyn-demo-data/usage.json
    asciinema rec --title 'KompoSyn Token Logger — synthetic example data' --cols 110 --rows 45 /tmp/komposyn-demo-data/komposyn-lg-demo.cast --command 'bash docs/demo/record-demo-session.sh'
    mkdir -m 700 /tmp/komposyn-demo-render
    docs/demo/render-assets.sh /tmp/komposyn-demo-data/komposyn-lg-demo.cast /tmp/komposyn-demo-render

The fixture uses protocol-v1 synthetic records only, with fixed dates, three sessions across two named sample projects, two Codex/OpenAI requests and three Claude Code/Anthropic requests. It reads no provider directories and includes no prompts, responses, credentials, or real project paths. Claude records intentionally omit observed billing context, so their estimated cost remains unknown; the shown Codex amount is explicitly a public list-price estimate, not provider-reported cost. The checked-in JSON is the exact CLI JSON output for the fixed report range.

Temporary data, harness responses, and generated recording renders are kept outside the repository. To clean up, remove only the dedicated directories created above after checking their exact paths:

    rm -r -- /tmp/komposyn-demo-data /tmp/komposyn-demo-render

## Asset manifest

See [asset-manifest.md](asset-manifest.md) for individual sizes, captions, alternative text, privacy review, and README-use guidance.

## Validation and release note

The fixture was seeded and queried with the signed binary; its totals and cost coverage were checked against the generated JSON. The capture covers overall usage, project navigation, sessions, model grouping, pricing coverage, and exact JSON export. Full-width screenshots are readable at normal desktop README scale; at phone width terminal text becomes too small to read without zoom. Use the captions/alt text and retain the source cast for accessible text review rather than shrinking the stills further.

At capture time, the target repository's SECURITY.md still described v0.8.1 as its latest release; the GitHub release page and verified signed artifacts show v0.9.0. This documentation records the artifact actually used and does not change that unrelated repository file.
