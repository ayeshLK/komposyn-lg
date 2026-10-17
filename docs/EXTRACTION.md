# Extraction provenance

This candidate preserves the CLI source and tests from the accepted combined
repository baseline commit:

`d66b4117d8b7771f9c3c22b3b251e19054ba9e88` (v0.7.0).

The source layout remains under `plugins/cli/token-logger/`; the package imports
core from `github.com/ayeshLK/komposyn/plugins/library/token-logger`, pinned to
v0.7.0 in this module without a `replace` directive. Domain behavior,
migrations, adapters, pricing, storage, and reusable reports remain in core.

Later verification fixes from PR #130 are applied to
`scripts/verify-release.sh`, `scripts/expand-release-archive.ps1`, and
`scripts/test-release-resolution.ps1` and `.sh`. The CLI remains the same
executable and preserves protocol v1, the `plugin_version` core field, and the
`cli_version` executable field.

The combined repository remains authoritative until a signed standalone
release is published and verified. No legacy CLI or release machinery should
be removed before that cutover is complete.
