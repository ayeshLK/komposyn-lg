# Operations

Komposyn is a local application. Its operational state is the data directory;
provider transcript directories remain outside Komposyn ownership.

## Locate the data directory

Use `komposyn-lg settings` to inspect the selected directory. Selection
precedence, legacy-directory discovery, settings, and collector startup policy
are documented in [configuration](configuration.md). The operational database
files are `token-logger.db` and `settings.json`; SQLite may also create WAL and
SHM sidecars while the database is active.

## Health and collection

Use a one-shot import when you need an immediate result. Use `watch` for
continuous polling; it performs an initial poll and then continues at the
configured interval. The collector reads source files and never modifies them.

```sh
komposyn-lg usage --format table
komposyn-lg watch add /path/to/codex /path/to/claude --data-dir /path/to/data
komposyn-lg watch start --data-dir /path/to/data
komposyn-lg watch status --data-dir /path/to/data
komposyn-lg settings --collector on --auto-start off
```

For temporary collection without persistence, use `watch foreground` with one
or more explicit paths. The
`TOKEN_LOGGER_CODEX_DIR` and `TOKEN_LOGGER_CLAUDE_DIR` variables configure only
the interactive auto-start collector; see [configuration](configuration.md).

The background runtime has no network listener. Coordination uses one SQLite
lease per data directory, a random instance identity, and a heartbeat. Stop is
cooperative and waits for an atomic import to commit or roll back. It never
signals a PID read from storage. A stale heartbeat remains visible and the
next start may replace it transactionally. Separate data directories have
independent collectors.

## Multiple REPL windows

You can open more than one REPL against the same local data directory. Each
process keeps its own terminal input/history, navigation, selected project and
session, range, and last-report snapshot. The processes share committed usage,
the saved settings file, registered background sources, and collector runtime
state. A settings change is persisted for the next launch; an already-open REPL
continues to use its startup settings snapshot.

Only one managed collector lease is active for a data directory. If a REPL
finds another healthy collector, it continues browsing without creating a
second worker or changing the existing worker's roots. A REPL that borrowed an
embedded collector does not stop it when the borrower closes. The REPL that
started an embedded collector owns its lifetime, and that collector stops when
the owning REPL exits. Other REPLs remain open and can query committed data;
they do not silently take over when the owner exits. `/status` shows the current
collector lifecycle and whether the active embedded collector belongs to this
REPL or another process. `/refresh` re-queries committed data and never starts
collection.

For a collector that should outlive every REPL, register persistent roots and
start the detached background worker with `watch add` and `watch start`. That
worker uses the shared registered-source list and continues after REPL windows
close. Embedded REPL collection instead uses the interactive default roots
and configured `TOKEN_LOGGER_CODEX_DIR` / `TOKEN_LOGGER_CLAUDE_DIR` roots; those
environment roots do not register or replace background roots. See
[configuration](configuration.md) for root selection and collector startup
policy. Different data directories have independent settings, reports, source
registries, and collector leases.

## Backups and recovery

Run `komposyn-lg watch stop` and stop foreground/interactive instances before
taking a file copy.
Back up the complete data directory, including `settings.json`, the database,
and any SQLite sidecars. See [Backups and recovery](backups.md) for a repeatable
procedure.

To recover, stop the application, replace the data directory with the backup,
and start the same or a compatible Komposyn version. Do not edit the database
or copy only the main `.db` file while a WAL database is active.

## Upgrades and uninstall

The initial upgrade operation is to rerun the latest standalone installer. It
stops a managed background collector before replacement and restarts it after
success only when it was previously active. Uninstall stops the managed
collector before removing the executable. Both preserve the database,
registered sources, runtime history, settings, diagnostics, and consent
choices. See [upgrades](upgrades.md) before changing versions.
