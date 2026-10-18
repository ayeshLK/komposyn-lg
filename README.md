# KompoSyn Token Logger CLI

**See where your AI coding tokens go.**

[![Latest release](https://img.shields.io/github/v/release/ayeshLK/komposyn-lg)](https://github.com/ayeshLK/komposyn-lg/releases/latest)
[![CI](https://github.com/ayeshLK/komposyn-lg/actions/workflows/ci.yml/badge.svg?branch=master)](https://github.com/ayeshLK/komposyn-lg/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

KompoSyn imports token usage from **Codex and Claude Code** into a local SQLite
database. Explore usage by project, session, model, or provider, inspect reported
costs and available price estimates, and keep your history current with a
background collector—all from your terminal.

[Install](#install) · [Quick start](#quick-start) · [Uninstall](#uninstall) ·
[Documentation](docs/README.md) · [Contributing](CONTRIBUTING.md)

## What you can do

- **Bring usage together.** Import supported local transcript files or directories;
  repeated imports deduplicate events.
- **Understand your usage.** Filter and group token, cost, and session reports.
  Export exact values as JSON or CSV for your own tools.
- **Keep collecting.** Register source directories and run a managed background
  collector, or watch sources in the foreground.
- **Stay in the terminal.** Open the interactive interface for onboarding,
  reports, and collector settings, or use individual commands in scripts.
- **Keep data local.** Provider files are read-only. KompoSyn stores usage metadata
  without capturing prompts, responses, or credentials, and has no telemetry.

Cost estimates use a bundled pricing catalog. They are public list-price estimates,
not invoices; missing pricing evidence remains unknown. See
[usage and cost reporting](docs/usage.md#reports) for coverage and limitations.

## Install

Signed stable releases are published for **Linux, macOS, and Windows**, on both
**amd64 (x86-64) and arm64**. You do not need Go to install a release.

The installers resolve the **latest stable release**, select your platform, and
verify the archive's SHA-256 checksum and Cosign signature before installing it.

### macOS and Linux

Requires Bash, `curl`, `jq`, `tar`, `sha256sum` or `shasum`, and
[Cosign](https://docs.sigstore.dev/cosign/system_config/installation/), available
on your `PATH`.

```sh
curl -fsSL https://raw.githubusercontent.com/ayeshLK/komposyn-lg/master/scripts/install.sh | bash
```

The default installation directory is `~/.local/bin`. If it is not already on
your `PATH`, add this line to your shell configuration and open a new terminal:

```sh
export PATH="$HOME/.local/bin:$PATH"
```

### Windows (PowerShell)

Requires PowerShell and
[Cosign](https://docs.sigstore.dev/cosign/system_config/installation/) on your
`PATH`.

```powershell
$d = Join-Path $env:TEMP ([guid]::NewGuid()); New-Item -ItemType Directory -Path $d | Out-Null; try { Invoke-WebRequest -Uri https://raw.githubusercontent.com/ayeshLK/komposyn-lg/master/scripts/install.ps1 -OutFile (Join-Path $d 'install.ps1'); Invoke-WebRequest -Uri https://raw.githubusercontent.com/ayeshLK/komposyn-lg/master/scripts/windows-architecture.ps1 -OutFile (Join-Path $d 'windows-architecture.ps1'); & (Join-Path $d 'install.ps1') } finally { Remove-Item -LiteralPath $d -Recurse -Force }
```

The default installation directory is `$env:LOCALAPPDATA\Komposyn\bin`.
Add that directory to your user `PATH` and open a new terminal. To use it in the
current PowerShell session:

```powershell
$env:Path = "$env:LOCALAPPDATA\Komposyn\bin;$env:Path"
```

### Check your installation

```sh
komposyn-lg version
komposyn-lg --help
```

For a pinned version, a custom install directory, local release artifacts, or
manual verification, see the [installation guide](docs/installation.md) and
[release verification guide](docs/release-verification.md). You can also browse
all [release downloads and notes](https://github.com/ayeshLK/komposyn-lg/releases).

## Quick start

Open the interactive interface:

```sh
komposyn-lg
```

The first launch asks whether to enable local collection and saves your choice.
You can also import a transcript directory and inspect reports directly:

```sh
komposyn-lg import /path/to/transcripts
komposyn-lg usage
komposyn-lg cost
komposyn-lg sessions
```

To collect from the default Codex and Claude Code directories in the background:

```sh
komposyn-lg watch add --defaults
komposyn-lg watch start
komposyn-lg watch status
```

Stop collection with `komposyn-lg watch stop`. Register custom directories with
`komposyn-lg watch add /path/to/transcripts`.

Filter reports or export them for further analysis:

```sh
komposyn-lg usage --group-by project,model
komposyn-lg cost --provider openai --format json
komposyn-lg sessions --format csv > sessions.csv
```

Use `komposyn-lg help <command>` for command-specific help, or follow the
[getting started guide](docs/getting-started.md).

## Upgrade

Download and run the installer again to install the latest stable release.
Upgrades preserve your database, settings, consent choices, and registered sources.
An active managed collector is stopped before replacement; a previously running
background collector is restarted. Foreground and interactive collectors need
manual resumption, as explained by the installer.

If you use a custom installation directory or data directory, set
`KOMPOSYN_INSTALL_DIR` and `TOKEN_LOGGER_DATA_DIR` to those same locations when
running install or uninstall scripts. See [upgrades](docs/upgrades.md) for details.

## Uninstall

Use the uninstall script for your platform. It stops a managed collector before
removing the executable and **preserves your local usage history and settings**.

**macOS and Linux:**

```sh
curl -fsSL https://raw.githubusercontent.com/ayeshLK/komposyn-lg/master/scripts/uninstall.sh | bash
```

**Windows (PowerShell):**

```powershell
Invoke-RestMethod https://raw.githubusercontent.com/ayeshLK/komposyn-lg/master/scripts/uninstall.ps1 | Invoke-Expression
```

New installations store data in `$HOME/.komposyn/token-logger/data` (under the
user profile on Windows); existing legacy data directories are supported.
`TOKEN_LOGGER_DATA_DIR` or `--data-dir` selects a different location. If you also
want to remove your history, first review [backups](docs/backups.md) and
[data-directory configuration](docs/configuration.md). Provider-owned transcripts
are never removed by these scripts.

## Documentation

| Goal | Guide |
| --- | --- |
| Learn the commands and output formats | [CLI reference](docs/cli-reference.md) |
| Configure data paths, collection, and consent | [Configuration](docs/configuration.md) |
| Understand reports and pricing | [Usage guide](docs/usage.md) |
| Diagnose collection problems | [Troubleshooting](docs/troubleshooting.md) |
| Connect a host application | [JSONL harness protocol v1](docs/HARNESS_PROTOCOL.md) |
| Understand local data handling | [Privacy](docs/privacy.md) and [security model](docs/security-model.md) |

See the [documentation index](docs/README.md) for the full guide.

## Development and contributing

Bug reports, documentation improvements, and code contributions are welcome.
Start with [CONTRIBUTING.md](CONTRIBUTING.md) and the
[development guide](docs/contributing/development.md) for setup and checks.

To build from source, install Go **1.26.9 or newer**:

```sh
git clone https://github.com/ayeshLK/komposyn-lg.git
cd komposyn-lg
make build
./bin/komposyn-lg --help
```

This repository owns the CLI, interactive experience, settings, and process
lifecycle. The separately versioned [KompoSyn core](https://github.com/ayeshLK/komposyn)
owns the reusable model, SQLite storage, provider adapters, pricing, and reports.
`komposyn-lg version` displays both the CLI and core versions.

[Open an issue](https://github.com/ayeshLK/komposyn-lg/issues) for bugs or feature
requests. Report vulnerabilities privately using [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution.
