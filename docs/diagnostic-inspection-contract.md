# Diagnostic inspection and reviewed export contract

Core #112 and #163 are complete and their public APIs are available in
published core v0.10.0. The CLI inspection/review increment is implemented by
`diagnostics list|show|review`; the separately reviewed export workflow is
tracked independently. The CLI must not use direct diagnostic SQL, copy core
summary logic, or commit a temporary module replacement.

## Core-owned inspection and review

The CLI provides list, show, and review operations through
`Store.ListDiagnosticDetails`, `Store.InspectDiagnosticDetail`, and
`Store.ReviewDiagnosticDetail`, delegating queries, aggregation, pagination,
and review compare-and-set to core. The contract must preserve:

- Detailed operations use the core problem ID. Legacy fingerprints remain
  separate; old diagnostics without a detail projection are not recast as
  detailed source identities. Unavailable legacy evidence stays explicit.
- Operational status separately from whether a person reviewed the item.
- Latest-poll, latest-run, and lifetime counts as separate core-provided
  observations. A skipped or unobserved source cannot be described as resolved.
- Stable category/reason codes, safe source identity, bounded impact and
  remediation, and the existing private-disclosure routing.
- Review as a local action only. Review does not transmit anything and does
  not imply that the underlying condition was corrected.

The CLI consumes the published v0.10.0 core contract. Core contract version 1
preserves exact uint64 counters as decimal strings, saturation and truncation,
and distinct latest-poll, latest-observed-run, and lifetime scopes. A bounded
64-ID replay window may count an evicted replay again and marks projected
counters truncated. Opening the normal store may perform migrations or
catalog synchronization; diagnostic commands must disclose those `Store.Open`
side effects and must not be described as strictly read-only. `diagnostics
list` returns one bounded core page; use `next_token` as `--continuation` with
the same page size and filters. Core binds continuation to that query. Human
output prints a continuation token; JSON preserves the versioned core page.
`show` exposes only core's bounded projection. `review` uses compare-and-set
with the current evidence revision and accepts only reviewed/skipped changes;
review state remains distinct from active/resolved/unknown condition.

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
