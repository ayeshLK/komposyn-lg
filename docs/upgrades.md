# Upgrades and data preservation

Upgrade by rerunning the latest signed standalone installer. It replaces the
executable while reusing the selected data directory. See [installation](installation.md)
for verified installation commands.

The following local state must survive an upgrade:

- `token-logger.db`, including its migration history and usage data
- `settings.json`, onboarding state, and collector preferences
- local diagnostics and their review state
- consent choices represented by local settings
- registered collector sources and prior runtime history

When the selected core needs a schema migration, it creates and verifies a
private SQLite online backup before applying numbered migrations. A backup
failure stops the migration. A newer unsupported schema is refused without
changing database contents or existing WAL sidecars. Older published binaries
may not have this protection; never open an upgraded database with an older
executable. See [database migrations](migrations.md) and [backups and recovery](backups.md)
for schema compatibility and restoration steps.

The installer requests cooperative stop from an active managed collector
before replacement. It restarts only a previously active detached background
collector. Foreground and embedded REPL collection remain stopped with explicit
manual-resume guidance, so an upgrade cannot silently change collection mode
or roots. The installer never terminates a persisted PID. Set
`TOKEN_LOGGER_DATA_DIR` during upgrade when using a non-default data directory;
see [configuration](configuration.md) for path selection.

Uninstall follows the same stop-before-remove rule and leaves the database,
settings, diagnostics, and consent state intact. Provider-owned transcripts are
not removed.

In-CLI self-updating is not provided. Release installers verify checksums and
Cosign signatures; do not bypass those checks by downloading an unverified
binary.
