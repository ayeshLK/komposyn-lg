# Diagnostic inspection and reviewed export contract

CLI issue #58 has two delivery increments. B remains outstanding until core
publishes the persisted diagnostic query/review APIs from core #112. Core #112
and structured-event issue #163 are open; the latest core release is `v0.9.0`
and does not provide these APIs. The CLI must not use direct diagnostic SQL,
copy core summary logic, or commit a temporary module replacement.

## Core-owned inspection and review

Once a released core API is available, the CLI will provide list, show, and
review operations by delegating queries and local state changes to core. The
contract must preserve:

- The existing diagnostic fingerprint and historical metadata. Generic
  legacy rows keep provenance and detail explicitly unavailable.
- Operational status separately from whether a person reviewed the item.
- Latest-poll, latest-run, and lifetime counts as separate core-provided
  observations. A skipped or unobserved source cannot be described as resolved.
- Stable category/reason codes, safe source identity, bounded impact and
  remediation, and the existing private-disclosure routing.
- Review as a local action only. Review does not transmit anything and does
  not imply that the underlying condition was corrected.

The CLI will consume only a tagged core release that exposes these semantics.
Exact Go symbols and field mappings will follow the released public API; this
document deliberately does not invent them. Opening the normal store may
perform the core's existing migrations or catalog synchronization. Diagnostic
commands will document those `Store.Open` side effects and will not be
described as strictly read-only.

## Reviewed export

Export is a separate action from viewing or reviewing. The workflow must:

1. Make the evidence selection explicit. Reviewed diagnostics are eligible only
   when individually selected; associated logs require a separate selection.
2. Show the exact bounded shareable projection and its remaining identifiers
   before final confirmation. A review mark is not export consent.
3. Re-project approved fields into a versioned shareable envelope. Exclude
   unknown fields, arbitrary error/message text, local PID/run/process IDs,
   source identifiers, and exact timestamps. Any deliberate retention needs
   an explicit privacy policy and a visible explanation.
4. Write only to a caller-selected destination, with private access and
   create-new/no-overwrite semantics. A failed write must not leave a file
   represented as a completed export.
5. Perform no upload, browser launch, issue-draft attachment, provider-file
   access, or external contact.

The export may still contain category, reason, version, broad time bucket,
counts, and safe remediation that can identify a product configuration or
rare failure. Sanitized does not mean anonymous. Export destination, file mode
and ACL behavior, failure cleanup, and the exact envelope version will be
specified against the released core model before B is implemented.

The bounded `logs show` command is independent of this workflow. Its local
output includes exact timestamps and run identifiers and must never be treated
as an implicitly reviewed or exportable attachment.
