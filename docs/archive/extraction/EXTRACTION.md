# Extraction provenance

> **Historical record.** Historical extraction provenance for the first standalone CLI, based on the v0.7.0 combined-repository baseline. See maintained [release verification](../../release-verification.md) for current publication requirements.


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

The first standalone release, CLI v0.8.0 pinned to core v0.7.0, was published
from `9369bc327e70928c69b332a4aa54ad38de5a226c` and verified in workflow run
[39102174717](https://github.com/ayeshLK/komposyn-lg/actions/runs/39102174717).
All six native signed runtime/installer and historical active-collector
upgrade jobs passed, followed by post-publication asset comparison and Linux
lifecycle verification. The standalone repository is now authoritative for
the executable and its release lifecycle. Core cleanup is being prepared as a
separate reviewable change; historical combined releases and their signer
identity remain available for verification.
