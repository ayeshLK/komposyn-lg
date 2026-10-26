# Privacy and data handling

Komposyn is designed for local usage accounting. It does not require provider
credentials to import local transcripts or generate reports.

## What Komposyn reads

The Codex and Claude Code adapters read provider-owned local JSONL files or
directories selected by the user. The collector can use configured roots and
platform default roots when enabled. Source files are read-only.

## What is stored

The local SQLite database stores normalized usage events, timestamps, token
counts, provider/model/client identifiers, project and session references,
provenance, ingestion keys, source cursors, registered collector paths, local
collector runtime/health, pricing rules, and sanitized local diagnostics.
Settings are stored separately in the configured data directory. Registered
paths are path-explicit local administrative data shown by `watch list`; they
are excluded from diagnostics and issue drafts.

The CLI also writes bounded JSONL troubleshooting logs under the user's home
directory by default. Logs contain fixed event names, command categories,
versions, process/run identifiers, and selected exit/error codes. They do not
accept raw arguments, raw errors, provider paths, project/session names, source
records, prompts, or responses. They are separate from SQLite and may be shared
by CLI processes that use different data directories. See
[local troubleshooting logs](local-logs.md) for exact paths, limits, rotation,
and safe inspection guidance. `komposyn-lg logs show` reads managed logs
without opening SQLite or initializing the writer. Its local output includes
exact timestamps and process/run identifiers; review it before sharing. The
reader omits unknown event text and fields, but local output is not a reviewed
external export.

## What is not stored by default

Komposyn does not intentionally persist prompts, model responses, full provider
transcripts, API keys, or provider credentials. Source record hashes and
metadata may be stored to support provenance, deduplication, and replacement
detection; they are not a transcript archive.

## External communication

Importing and reporting are local operations. Release installation downloads
artifacts and verifies their signatures. Diagnostic issue reporting is
interactive and produces a draft for review; it is not silently submitted.
The public pricing catalog is bundled with the application; routine catalog
loading, import, and reporting do not contact providers or send usage/model
data over the network.
Opening a GitHub issue-draft URL sends the sanitized diagnostic text to
GitHub, where it may also appear in browser history or other URL logs. The
sanitizer is best-effort, not a guarantee of secret removal. Review the URL
before opening it, and use private vulnerability reporting instead of an issue
draft for security vulnerabilities.

Review [SECURITY.md](../SECURITY.md) before reporting a vulnerability. Never
include credentials, transcripts, prompts, responses, or other sensitive source
content in public issues or diagnostic reports.

## Data preservation

Rerunning the latest standalone installer preserves the existing database,
settings, consent choices, diagnostics, and data directory. Uninstall removes
the executable but intentionally preserves local data.
