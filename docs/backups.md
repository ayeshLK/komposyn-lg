# Backups and recovery

KompoSyn stores its database, settings, consent choices, and local diagnostics
in the selected data directory. Stop all collectors before copying or restoring
files. Copy the complete data directory, including SQLite WAL and SHM sidecars
when present; never copy a live database file by itself. The selected location
and precedence rules are documented in [configuration](configuration.md).

## Before an upgrade

Keep a separate copy of the complete data directory before changing versions.
When an upgrade needs a schema migration, the CLI creates and verifies a
private SQLite backup before applying the migration. Same-schema executable
upgrades do not create a migration backup. Keep `.komposyn-pre-migration-*.sqlite`
files protected with the rest of the data until the upgrade is validated. The
backup records its source schema, target schema, and core version; its schema is
not necessarily the same as another historical backup.

## Restore or recover from a migration

Stop every collector and close the CLI before restoring data. For ordinary
recovery, restore the complete saved data directory. For migration recovery,
use the verified pre-migration backup that belongs to that upgrade, restore it
into a separate data directory, and open it only with a CLI/core version that
supports the backup's recorded schema. Check the backup metadata before
choosing the executable. Do not point an older binary at a migrated database.

Migrations are forward-only; the CLI does not automatically down-migrate. A
newer unsupported schema is refused without migration. Preserve the original
data and backup if opening or restoration fails. See [database migrations](migrations.md)
for schema behavior and [upgrades](upgrades.md) for installer and collector
lifecycle details.

The core library describes its migration backup format and APIs in the
[core migration backup guide](https://github.com/ayeshLK/komposyn/blob/v0.9.0/docs/migration-backups.md).
