# Release verification

Release Please prepares the standalone CLI version and changelog in a reviewed
pull request. CLI and core versions advance independently: the release builder
uses the requested CLI tag for `cli_version` and the exact pinned core module
version for core `plugin_version`. The first standalone release was CLI
`v0.8.0`, pinned to core `v0.7.0`; CLI `v0.8.1` pins core `v0.7.1`. The next
release candidate pins the published `github.com/ayeshLK/komposyn v0.9.0` and
sets it as the minimum supported core version. Normal CLI CI tests that version
once instead of adding a redundant second job. Core v0.8.0 migrated schema 15
to 16, and core v0.9.0 migrates 16 to 17 with recorded project-name origin.
Native upgrade checks verify schema-15 backup recovery and refusal of schema 17
by the shipped older binaries. See
[core compatibility and coordinated security releases](core-compatibility-security.md).

The release preparation job resolves the core version from the exact validated
release commit using `scripts/resolve-release-core-version.sh`. That resolver
rejects replacements, Go workspaces, and any core version outside the reviewed
release pin. The shared value is passed to native runtime, Unix and Windows
active-upgrade, and post-publication checks. Standalone active-upgrade verifier
calls keep their existing arguments and resolve the same expected version from
the checkout; callers may also pass the independently prepared CLI/core values
explicitly as the third/fourth shell arguments or `-ExpectedCliVersion` and
`-ExpectedCoreVersion` in PowerShell.

## Publishing

The manually dispatched publication workflow resolves and validates the merged
Release Please pull request, tests the exact commit, builds six target
archives, signs them with Cosign, verifies installer lifecycle on Linux,
macOS, and Windows, and publishes only after those checks pass and the
protected `release` environment allows publication. Do not create or move tags
manually.

Standalone artifacts are trusted only when the certificate identity is exactly
`https://github.com/ayeshLK/komposyn-lg/.github/workflows/release.yml@refs/heads/master`
with issuer `https://token.actions.githubusercontent.com`. Installers reject
the former combined-repository identity and unrelated workflow identities.

## Artifacts

Each release includes Linux/macOS amd64 and arm64 archives and Windows amd64
and arm64 archives, the executable, Apache-2.0 license, notice, README,
third-party license attribution and license texts, and a SHA-256 manifest.
Archive and checksum manifest signatures accompany each release.

## Local verification

Use an empty output directory outside the repository. The local build does not
create a tag:

```sh
go mod verify
go vet ./...
go test ./...
go test -race ./...
make release VERSION=v0.8.1 OUTPUT_DIR=/tmp/komposyn-lg-release-test
bash scripts/verify-release.sh /tmp/komposyn-lg-release-test
```

The lifecycle verifier checks manifest checksums, archive contents, initial
settings/database creation, executable replacement, collector restart, and
data-preserving uninstall. The publication workflow also tests native
installers. Cross-compilation is not runtime evidence for any target.
