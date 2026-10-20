# Getting started

This guide takes you from installation to a useful local usage report. The
current standalone CLI supports usage records from Codex and Claude Code; it
does not require provider credentials for importing or reporting.

## Prerequisites and installation

Signed stable GitHub releases are available for macOS, Linux, and Windows on
amd64 and arm64. You do not need Go or provider credentials to install a
release.
Follow the platform prerequisites and PATH setup in
[Installation](installation.md); its installers verify both the SHA-256
checksum and Cosign signature before installing.

Building from source is optional and requires Go 1.26.9 or newer:

```text
git clone https://github.com/ayeshLK/komposyn-lg.git
cd komposyn-lg
make build
```

Confirm the binary and version:

```text
./bin/komposyn-lg version
```

The version command above checks the source-built binary. For an installed
release, check it with `komposyn-lg version`. The examples below use the
installed command; from the repository root, use `./bin/komposyn-lg` in its
place when following the source-build path.

## Get your first report

Run the installed CLI, or use `./bin/komposyn-lg` from the repository root if
you built it from source:

```sh
komposyn-lg
```

On first use, onboarding asks whether to start the local collector now and
whether to start it automatically on future launches of the interactive
interface. If you opt in, the collector reads configured Codex and Claude Code
directories; it does not modify provider files. The first scan is asynchronous.
The collector notice appears at prompt boundaries, so it may not appear while
the prompt is idle. Run `/status` and check `last_completed` and source counts.
If `last_completed` is not available yet, run `/status` again later, or press
Enter to return through a prompt boundary and then inspect status. Once a scan
has completed, run this inside the interactive view to query committed data:

```text
/total-usage
```

The default report covers the last seven UTC calendar days. To inspect the
breakdown, navigate through project and session rows printed by the report:

```text
/projects
/project 1
/sessions
/session 1
/models
```

Choose row numbers from the displayed tables; `1` above is an example. Use
`/back` or `/home` to return to a broader view.

If you declined collection or prefer a one-time import, run this from a shell.
Replace `<SOURCE>` with a supported Codex or Claude Code transcript file or
directory before running the command:

```sh
komposyn-lg import <SOURCE>
komposyn-lg usage --format table
```

Export exact token counts for other tools with JSON or CSV:

```sh
komposyn-lg usage --group-by project,model --format json > usage.json
komposyn-lg sessions --format csv > sessions.csv
```

Reported costs remain separate from public list-price estimates. Estimates are
not invoices, and KompoSyn keeps missing pricing evidence visible as unknown.
Read [usage and pricing](usage.md#reports) for details.

## Data location

New installations store the database and settings under
`$HOME/.komposyn/token-logger/data` (under the Windows user profile on Windows).
Set `TOKEN_LOGGER_DATA_DIR` to choose another directory for both interactive
and command-line use. Existing legacy data is discovered in place; see
[Configuration](configuration.md) for path precedence and conflict behavior.

## Next steps

- Learn filters, costs, and optional collection in the [Usage guide](usage.md).
- Review the [CLI reference](cli-reference.md).
- Understand the [privacy and data contract](privacy.md).
