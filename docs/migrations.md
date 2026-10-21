# Database migrations

The CLI opens the SQLite database through the KompoSyn core library. Core
v0.8.0 advanced schema 15 to 16 by adding private parser checkpoints for
validated append continuation. Core v0.9.0 advances schema 16 to 17 by adding
recorded project-name origin. Existing projects receive `unknown`; the
migration does not infer or repair historical provenance. Checkpoints contain
bounded normalized parser state and fingerprints, not provider source text.
Migrations preserve existing events, identities, settings, consent and data
paths, and create a verified pre-migration backup before changing the database.
The project-inspection CLI integration requires published core v0.9.0.

Before upgrading, keep a backup of the full data directory. If migration fails,
preserve the original files and use the recovery procedure in
[Backups and recovery](backups.md). Do not manually edit the migration ledger or
reuse an upgraded database with an older binary.

For migration history, implementation details, and supported rollback evidence,
see the [v0.8.0 core migration guide](https://github.com/ayeshLK/komposyn/blob/v0.8.0/docs/migrations.md).
