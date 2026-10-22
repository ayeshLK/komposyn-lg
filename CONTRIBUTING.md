# Contributing

Contributions to KompoSyn Token Logger CLI are provided under Apache-2.0; see
`LICENSE` and `NOTICE`. Read `AGENTS.md` and `SECURITY.md` before changing the
CLI, installer, protocol, or release process.

The CLI depends on the separately versioned core module
`github.com/ayeshLK/komposyn`. Keep CLI and core versions independent, avoid a
permanent `replace`, and keep domain behavior in core. Preserve the library/CLI
boundary and the protocol-v1 contract. Do not capture prompts, responses,
credentials, or provider source content, and do not add telemetry.

CLI v0.8.0 historically pinned core v0.7.0, and CLI v0.8.1 pins core v0.7.1.
The next standalone CLI release candidate raises both the minimum supported
core version and current pin to v0.8.0, so normal CI covers that version once.
Core candidate CI also runs the public CLI suite against the proposed
core source without cross-repository credentials. See [core compatibility and
coordinated security releases](docs/core-compatibility-security.md) for
dependency-update review, version support, and private advisory coordination.

Use the [testing guide](docs/contributing/testing.md) for required checks and
change-specific validation. Release and installer changes must also satisfy
[release verification](docs/release-verification.md), including native
lifecycle, signature, checksum, upgrade-preservation, and data-preserving
uninstall gates. Report exact commands and results in pull requests. Never
create or move release tags manually; use the reviewed Release Please and
protected publication workflow.

## Reporting problems and contributing

For installation feedback or a reproducible CLI bug, open an issue in the
[komposyn-lg issue tracker](https://github.com/ayeshLK/komposyn-lg/issues).
Include the CLI version, operating system and architecture, command, sanitized
output, and steps to reproduce. Remove private paths and project names. Do not
attach provider transcripts, prompts, responses, credentials, or raw
diagnostics. Report vulnerabilities privately using the process in
[SECURITY.md](SECURITY.md); do not report them in public issues.

Documentation and code contributions are welcome. Before opening a pull
request, read the relevant ownership and privacy guidance in this file and
[AGENTS.md](AGENTS.md), and describe the user problem, changes, and checks you
ran. Library, parser, storage, pricing, and report behavior belongs in the
[KompoSyn core repository](https://github.com/ayeshLK/komposyn).
