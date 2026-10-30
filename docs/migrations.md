# Database migrations

The CLI opens its SQLite database through the separately versioned KompoSyn
core library. Numbered migrations are forward-only and are applied on open
when the selected core supports them. A newer unsupported schema is rejected
without mutation. Do not manually edit the migration ledger or reuse a migrated
database with an older executable.

The latest published mapping remains CLI v0.10.0/core v0.9.0 with schema 17.
The current development source pins published core v0.10.0 for the next CLI
release and supports schema 18. Core v0.8.0 advanced schema 15 to 16 with
private append checkpoints; core v0.9.0 advanced schema 16 to 17 with recorded
project-name origin; core v0.10.0 advances schema 17 to 18 with bounded
persisted diagnostic details and a per-database diagnostic namespace key.
Existing project origins remain `unknown`; the migration does not infer
historical names.

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

Before migration 17→18, core creates and verifies a pre-migration backup using
the existing backup flow. Restore only with a CLI/core version that supports
the backup's recorded schema. Older binaries that support schema 17 refuse
schema 18 without mutation. Migrations are forward-only; no automatic
down-migration is available.

For core migration history and implementation details, see the
[core v0.10.0 migration guide](https://github.com/ayeshLK/komposyn/blob/v0.10.0/docs/migrations.md).
