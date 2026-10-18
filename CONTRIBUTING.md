# Contributing

Contributions to KompoSyn Token Logger CLI are provided under Apache-2.0; see
`LICENSE` and `NOTICE`. Read `AGENTS.md` and `SECURITY.md` before changing the
CLI, installer, protocol, or release process.

The CLI depends on the separately versioned core module
`github.com/ayeshLK/komposyn`. Keep CLI and core versions independent, avoid a
permanent `replace`, and keep domain behavior in core. Preserve the library/CLI
boundary and the protocol-v1 contract. Do not capture prompts, responses,
credentials, or provider source content, and do not add telemetry.

At CLI v0.8.0, both the minimum supported core version and the current pin are
v0.7.0, so normal CI covers that version once. Core candidate CI also runs the
public CLI suite against the proposed core source without cross-repository
credentials. See [core compatibility and coordinated security releases](docs/core-compatibility-security.md)
for dependency-update review, version support, and private advisory coordination.

Use focused tests while iterating, then run `go mod verify`, `go vet ./...`,
`go test ./...`, `go test -race ./...`, and `make check`. For installer or
release changes, also run the platform lifecycle checks and verify signatures,
checksums, upgrade preservation, and data-preserving uninstall. Report exact
commands and results in pull requests. Never create or move release tags
manually; use the reviewed Release Please and protected publication workflow.
