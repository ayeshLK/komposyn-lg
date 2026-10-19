# Backups and recovery

KompoSyn Token Logger keeps its database, settings, consent choices, and local
diagnostics in the selected data directory. Stop all collectors before copying
or restoring these files. Copy the complete data directory, including SQLite
WAL and SHM sidecars when present; never copy a live database file by itself.

The CLI creates and verifies a private SQLite backup before an upgrade that
advances the schema. Same-schema executable upgrades do not create a migration
backup. Keep any `.komposyn-pre-migration-*.sqlite` files with the protected
data directory until the upgrade has been validated.

For recovery, stop the collector and restore a complete saved data directory.
For a migration rollback, restore the verified pre-migration database into a
separate working data directory and run a CLI version that supports its schema.
Do not point an older CLI at the upgraded database.

The core library documents its migration backup format and APIs in the
[v0.7.1 core backup guide](https://github.com/ayeshLK/komposyn/blob/v0.7.1/docs/backups.md).
