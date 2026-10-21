# Backups and recovery

KompoSyn Token Logger keeps its database, settings, consent choices, and local
diagnostics in the selected data directory. Stop all collectors before copying
or restoring these files. Copy the complete data directory, including SQLite
WAL and SHM sidecars when present; never copy a live database file by itself.

The CLI creates and verifies a private SQLite backup before an upgrade that
advances the schema. Same-schema executable upgrades do not create a migration
backup. Keep any `.komposyn-pre-migration-*.sqlite` files with the protected
data directory until the upgrade has been validated.

The next CLI release candidate updates core from v0.7.1 to v0.9.0. Core v0.8.0
introduced the schema 15 to 16 migration, which creates a verified backup that
records the source and target schema and core version. Keep a separate full data
directory backup before upgrading. Stop collectors before copying or restoring
data. After a successful migration, CLI v0.8.1 and other older binaries refuse
the schema-16 database without changing it. To recover, stop collectors and
restore the verified schema-15 migration backup into a separate data directory,
then use a CLI/core version that supports schema 15. Keep the original backup;
opening a recovery copy may migrate it forward and create another backup.

For recovery, stop the collector and restore a complete saved data directory.
For a migration rollback, restore the verified pre-migration database into a
separate working data directory and run a CLI version that supports its schema.
Do not point an older CLI at the upgraded database.

The core library documents its migration backup format and APIs in the
[v0.8.0 core backup guide](https://github.com/ayeshLK/komposyn/blob/v0.8.0/docs/migration-backups.md).
