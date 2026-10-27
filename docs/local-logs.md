# Local troubleshooting logs

The CLI writes bounded, local JSON Lines troubleshooting records before it
opens SQLite. This includes short commands such as `version`, so startup and
database-open failures can still leave process evidence. Logging is best-effort
and does not determine a command's exit status or usage accounting.

## Location

The active file is:

```text
<user-home>/.komposyn/token-logger/klg.log
```

Set `TOKEN_LOGGER_LOG_DIR` to choose another directory. The active basename
remains `klg.log`. A relative override is resolved against the process's
working directory and converted to an absolute path. If no override is set and
the home directory cannot be resolved, file logging is disabled and the CLI
prints the fixed warning `warning: local troubleshooting log unavailable`; it
never falls back to the current directory.

Log placement is independent of SQLite. Changing `TOKEN_LOGGER_DATA_DIR` or
using a legacy/custom database location does not move logs. Processes that use
different database directories share the default log location unless each is
given a different `TOKEN_LOGGER_LOG_DIR`.

## Contents and privacy

Each physical line is one JSON object with `format_version: 1`, a UTC
timestamp, level, fixed event name, CLI and core versions, process ID, and a
random process-run ID. Where applicable, records also contain a command
category from a fixed allowlist, a fixed error code, exit code, or suppressed
and dropped event counts. No raw command arguments, error strings, environment
values, data-directory paths, provider paths, project/session names, source
records, prompts, or responses are accepted by the logging interface.

The CLI records process start/finish, data-directory resolution origin,
settings persistence, controlled command failures, and collector lifecycle,
health, and bounded outcome summaries. It does not record successful
per-record imports. Repeated events
are suppressed for a short window and summarized; a full queue drops events
without blocking command execution and reports counts when possible. If a
detached collector cannot initialize or write its log, it records the fixed
`reason=local_log_sink_unavailable` marker as a pending local CLI diagnostic
when its database is available. This does not send a report or grant consent.

Logs and their process/run identifiers are local troubleshooting data. Review
them before sharing; they are not automatically safe to publish or attach to
an issue.

The [event and code catalogue](issue-57-cli-logging-coverage.md) defines the
format-version-1 event names, controlled codes, and latest-poll versus
run-cumulative counter scopes. With the currently pinned core v0.9.0, source
and parser causes unavailable from the public API remain generic. Rich typed
core events depend on core #163 and a reviewed published core release; the CLI
does not infer causes from error strings. A log event is best-effort evidence,
not an atomic outbox, an exactly-once stream, or an accounting authority.

## Size, rotation, and recovery

The active file and each of three archives are capped at 5 MiB. Rotation keeps
`klg.log.1`, `klg.log.2`, and `klg.log.3`, for at most 20 MiB of managed log
content. A record is rotated before an append would exceed the per-file cap.
The queue holds at most 256 records; each encoded record is limited to 8 KiB.
The writer batches at most 64 records and waits at most 50 ms for the shared
process lock. Normal shutdown gives queued records up to 500 ms to flush.
These are best-effort limits, not a durability promise; the writer does not
`fsync` every record, and abrupt power loss can lose recent records.

Processes coordinate rotation and append using a stable lock file in the log
directory. The lock file is not rotated or removed. Files are opened only
inside the lock and closed before rotation or unlock. Unix directories/files
use mode 0700/0600. Windows applies a protected access-control list for the
current user. Unexpected symlinks, reparse points, directories, special files,
or occupied rotation targets disable that write rather than being followed or
overwritten.

If a process stops during a write, the next writer removes an incomplete final
line from the active file before appending. If rotation stopped after moving
the active file to `.1`, the next writer restores it as active. Older managed
numeric archives beyond `.3` are pruned after validating them as regular log
files. Pre-existing managed files over 5 MiB are reduced at a complete-line
boundary to the first 5 MiB; unrelated names in the log directory are left
alone. An unusable target or failed write is not repaired automatically.

## Inspecting and removing logs

On Unix-like systems, inspect recent events with:

```sh
tail -n 100 ~/.komposyn/token-logger/klg.log
```

On PowerShell:

```powershell
Get-Content "$HOME\.komposyn\token-logger\klg.log" -Tail 100
```

Close CLI and collector processes before manually removing `klg.log` and its
`.1` through `.3` archives. The stable `.lock` file can remain; deleting it
while another process may be using the log can split coordination across two
lock files.

For database and collector diagnosis, see
[troubleshooting](troubleshooting.md). For broader data handling, see
[privacy](privacy.md).
