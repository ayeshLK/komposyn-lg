# KompoSyn Token Logger CLI

`komposyn-lg` is a local-first command-line application for importing token
usage from AI coding clients, collecting usage in the background, and producing
reports. It uses the separately versioned
[`github.com/ayeshLK/komposyn`](https://github.com/ayeshLK/komposyn) core for
usage normalization, SQLite storage, provider adapters, pricing, diagnostics,
and reusable report behavior.

The standalone CLI is proposed to begin at v0.7.1, with the v0.7.0 core as its
minimum dependency. CLI and core versions advance independently. Protocol v1,
existing data paths, consent behavior, and JSON/CSV values remain stable.

See the [CLI documentation](docs/README.md) for installation, configuration,
commands, the harness protocol, data handling, and release verification.

## Install

Signed stable releases are published in this repository. Download the archive for your operating system and architecture from the
[KompoSyn Token Logger CLI releases](https://github.com/ayeshLK/komposyn-lg/releases),
verify its SHA-256 checksum and Cosign signature, then use the supplied
installer. The installers verify artifacts before replacing an executable.

The default local data directory is `$HOME/.komposyn/token-logger/data` (or
its platform equivalent). Existing legacy data directories remain supported.
Set `TOKEN_LOGGER_DATA_DIR` or use the command's `--data-dir` option to choose a
different location. The CLI does not transmit provider source content.

## Build from source

Requires Go 1.26 or newer:

```sh
go build -o bin/komposyn-lg ./plugins/cli/token-logger
```

Read [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) before
changing behavior. Domain behavior belongs in the core module; this repository
owns command-line policy, settings, onboarding, process lifecycle, terminal
interaction, and the versioned JSONL harness transport.
