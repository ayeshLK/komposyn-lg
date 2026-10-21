# Database migrations

The CLI opens the SQLite database through the KompoSyn core library. Core
v0.8.0 advances schema 15 to schema 16 by adding private parser checkpoints for
validated append continuation. Checkpoints contain bounded normalized parser
state and fingerprints, not provider source text. The migration preserves
existing events, identities, settings, consent and data paths, and creates a
verified pre-migration backup before changing the database. The project-inspection
CLI integration requires published core v0.9.0; its schema migration remains
the v0.8.0 migration described above.

Before upgrading, keep a backup of the full data directory. If migration fails,
preserve the original files and use the recovery procedure in
[Backups and recovery](backups.md). Do not manually edit the migration ledger or
reuse an upgraded database with an older binary.

For migration history, implementation details, and supported rollback evidence,
see the [v0.8.0 core migration guide](https://github.com/ayeshLK/komposyn/blob/v0.8.0/docs/migrations.md).
