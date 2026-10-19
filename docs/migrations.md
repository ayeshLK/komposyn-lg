# Database migrations

The CLI opens the SQLite database through the KompoSyn core library. The core
owns the numbered schema migrations and creates a verified backup before
advancing an older schema. The standalone CLI requires the published core
module version recorded in its release metadata.

Before upgrading, keep a backup of the full data directory. If migration fails,
preserve the original files and use the recovery procedure in
[Backups and recovery](backups.md). Do not manually edit the migration ledger or
reuse an upgraded database with an older binary.

For migration history, implementation details, and supported rollback evidence,
see the [v0.7.1 core migration guide](https://github.com/ayeshLK/komposyn/blob/v0.7.1/docs/migrations.md).
