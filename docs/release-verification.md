# Release verification

Release Please prepares the standalone CLI version and changelog in a reviewed
pull request. CLI and core versions advance independently: the release builder
uses the requested CLI tag for `cli_version` and the exact pinned core module
version for core `plugin_version`. The proposed first standalone CLI version is
v0.7.1 and requires `github.com/ayeshLK/komposyn v0.7.0`; no permanent
`replace` directive is permitted.

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
make release VERSION=v0.7.1 OUTPUT_DIR=/tmp/komposyn-lg-release-test
bash scripts/verify-release.sh /tmp/komposyn-lg-release-test
```

The lifecycle verifier checks manifest checksums, archive contents, initial
settings/database creation, executable replacement, collector restart, and
data-preserving uninstall. The publication workflow also tests native
installers. Cross-compilation is not runtime evidence for any target.
