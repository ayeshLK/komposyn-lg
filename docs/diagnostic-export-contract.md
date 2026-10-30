# Reviewed diagnostic export

Reviewed export is a separate action from listing, inspecting, or reviewing a
diagnostic. Review is a local state change and does not grant consent to
disclose evidence. Export requires explicit problem IDs, and every selected
detail is read again through core's `Store.InspectDiagnosticDetail` API at
export time. Only details currently marked `reviewed` are eligible. Security
sensitive classifications are not accepted by the public export flow; follow
the private-disclosure process in [SECURITY.md](../SECURITY.md).

The CLI constructs a versioned envelope from a fixed allowlist. It may include
controlled classification codes, the separate operational condition state,
latest-poll/latest-observed-run/lifetime exact counters, availability,
saturation and truncation markers, plus bounded capacity/sample flags. Counts
remain decimal strings. Unknown or unavailable values remain explicit. The
export omits problem and subject references, raw messages/errors, provider and
source identifiers, paths, process/run IDs, sample offsets, and exact
timestamps. These omissions reduce exposure; classification and usage counts
can still identify a rare product condition and are not anonymous.

Associated local log records require a separate explicit selection from
diagnostic details. The selection refers to reader-issued references for
records returned by the bounded managed-log reader; caller-entered event/code
tuples cannot create log evidence. The export accepts only event, severity,
controlled code, CLI/core version, an optional bounded count, and the fixed
validated core field catalogue. It does not copy local log timestamps,
process/run identifiers, arbitrary messages, file names, or other metadata.
Version metadata is included only for a selected log record and is shown in
the preview. The exact combined envelope is displayed before the consent
callback. Declined consent or a failed preview creates no destination file.

The caller selects the destination. Creation is exclusive and never replaces
an existing file or follows a destination symlink. Output is private (0600 on
Unix-like systems and a protected current-user ACL on Windows). A failed
write attempts to remove only the file created by that invocation after
checking that the destination still identifies that file. If the destination
was replaced concurrently, cleanup refuses to remove it. Export performs no
upload, browser launch, issue attachment, provider-file read, or network
request. It does not change diagnostic review state.

Diagnostic queries use an already-open Store. `Store.Open` may perform schema
migration and pricing-catalog synchronization; that existing behavior is
separate from inspection's no-provider-read guarantee. See [database
migrations](migrations.md) and [backups](backups.md) before opening data that
may require an upgrade.
