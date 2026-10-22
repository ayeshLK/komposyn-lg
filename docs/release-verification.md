# Release verification

Release Please prepares the standalone CLI version and changelog in a reviewed
pull request. CLI and core versions advance independently: the release builder
uses the requested CLI tag for `cli_version` and the exact pinned core module
version for core `plugin_version`. The current published CLI is v0.10.0 and
pins the published core v0.9.0; that is also the minimum supported core
version. Core v0.8.0 migrated schema 15 to 16, and core v0.9.0 migrated 16 to
17 with recorded project-name origin. The version map and historical schema
compatibility are in [core compatibility](core-compatibility-security.md).

The release preparation job resolves the expected core version from the exact
validated release commit using `scripts/resolve-release-core-version.sh`. That
resolver rejects replacements, Go workspaces, and any core version outside the
reviewed release pin. The shared value is passed to native runtime, Unix and
Windows active-upgrade, and post-publication checks. Standalone active-upgrade
verifier calls keep their existing arguments and resolve the same expected
version from the checkout; callers may also pass the independently prepared
CLI/core values explicitly as the third/fourth shell arguments or
`-ExpectedCliVersion` and `-ExpectedCoreVersion` in PowerShell.

## Required release gates

A release is ready for publication only when the protected workflow passes on
the exact reviewed commit and tag. It verifies signed runtime metadata and
protocol smoke natively for all six targets: Linux, macOS, and Windows on
amd64 and arm64. Cross-compilation does not count as native runtime evidence.

The workflow also runs signed active-collector upgrade checks on every target
from the historical CLI v0.6.0 and v0.8.1 releases. These checks cover the
existing data and migration path, backup recovery, refusal of unsupported newer
schemas, collector restart, and data-preserving uninstall. Current schema-change
coverage and historical lifecycle evidence remain release gates; ordinary CI
or a candidate build does not replace them.

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
create a tag and cannot provide signed publication or native six-target
evidence:

```sh
go mod verify
go vet ./...
go test ./...
go test -race ./...
make release VERSION=v0.10.0 OUTPUT_DIR=/tmp/komposyn-lg-release-test
bash scripts/verify-release.sh /tmp/komposyn-lg-release-test
```

The lifecycle verifier checks manifest checksums, archive contents, initial
settings/database creation, executable replacement, collector restart, and
data-preserving uninstall. The protected publication workflow supplies the
signed native runtime and historical active-upgrade evidence described above.
