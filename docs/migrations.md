# Database migrations

The CLI opens its SQLite database through the separately versioned KompoSyn
core library. Numbered migrations are forward-only and are applied on open
when the selected core supports them. A newer unsupported schema is rejected
without mutation. Do not manually edit the migration ledger or reuse a migrated
database with an older executable.

Core migrations preserve existing events, identities, settings, consent, and
data paths unless a documented migration adds metadata. The project-name-origin
migration records `unknown` for existing projects; it does not infer or repair
historical provenance. Parser checkpoints contain bounded normalized state
and fingerprints, not provider source text.

Before an upgrade, keep a complete data-directory backup. When a migration is
required, the CLI verifies a pre-migration SQLite backup before changing the
database. If migration or recovery fails, preserve the original files. Follow
[backups and recovery](backups.md) to select a compatible binary from the
backup's recorded schema. Installer behavior is covered in [upgrades](upgrades.md).

For core migration history and implementation details, see the
[core migration guide](https://github.com/ayeshLK/komposyn/blob/v0.9.0/docs/migrations.md).
