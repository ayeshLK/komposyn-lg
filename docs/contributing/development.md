# Development guide

## Local setup

Install Go 1.26.9 or newer and work from a feature branch. This module is
`github.com/ayeshLK/komposyn-lg`; the CLI source remains under
`plugins/cli/token-logger` to preserve the extracted layout. The module requires
`github.com/ayeshLK/komposyn v0.9.0` or newer and must not use a permanent
`replace` directive.

Keep local build output in the ignored `bin/` directory. Use a fresh temporary
directory for release output rather than the repository root.

## Useful commands

```sh
go test ./...
go test -race ./...
go vet ./...
go mod verify
go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
make check
make build
```

Focused CLI tests can be run while iterating:

```sh
go test ./plugins/cli/token-logger -run TestName
```

The CI vulnerability check uses govulncheck v1.1.4 and fails for reachable
findings. Review findings before release; do not silence them with exclusions
without a recorded security rationale. `make check` runs formatting, vet, all
tests, a CLI build, and installer trust tests. Formatting may modify Go files,
so inspect the diff. CI also checks module verification, builds/tests on Linux,
macOS, and Windows, runs Linux race tests, and runs CodeQL separately.

## Release validation

Before changing release scripts or installers, run syntax and lifecycle checks
with a fresh output directory. Local builds do not create tags:

```sh
bash -n scripts/*.sh
make release VERSION=v0.10.0 OUTPUT_DIR=/tmp/komposyn-lg-release-test
bash scripts/verify-release.sh /tmp/komposyn-lg-release-test
```

The verifier covers archive contents, install, executable replacement, and
data-preserving uninstall. Release workflows additionally sign with Cosign and
verify the exact standalone workflow identity. Do not weaken checksum or
signature verification.
