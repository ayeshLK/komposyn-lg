# Release highlights

> **Historical record.** Historical highlights for CLI v0.9.0 with core v0.8.0, released October 19, 2026. This page describes that release and its demo, not the current CLI. See the maintained [usage guide](../../usage.md), [installation guide](../../installation.md), and current release list.


## CLI v0.9.0 with KompoSyn core v0.8.0

Released October 19, 2026. See the [CLI v0.9.0 release](https://github.com/ayeshLK/komposyn-lg/releases/tag/v0.9.0), built from source commit [`01ebb45b53a9a0f4b7be5f6ad2f0a40e6bd48694`](https://github.com/ayeshLK/komposyn-lg/tree/01ebb45b53a9a0f4b7be5f6ad2f0a40e6bd48694). This release uses [KompoSyn core v0.8.0](https://github.com/ayeshLK/komposyn/releases/tag/v0.8.0), source commit [`258dc3e3e13eee176786aa68a97ed5ee09063717`](https://github.com/ayeshLK/komposyn/tree/258dc3e3e13eee176786aa68a97ed5ee09063717). CLI and core versions are independent.

### A clearer interactive report

Starting `komposyn-lg` now opens with a short guide to `/help`, `/status`, and
`/quit`. The report keeps the active project/session context and time range
visible while leaving collector details available when you ask for them. A
brief collector notice appears when its meaning changes instead of repeating
poll timestamps and counters at each prompt.

```text
komposyn-lg
/status
/pricing
/range
```

`/status` shows the CLI and core versions, selected data directory, saved
collection preferences, collector health and counters, and the exact bounds
and query time of the last successful report. It is an inspection command:
it does not scan provider files or change the active range. `/refresh` redraws
committed database data; source scanning is separate.

### Follow-up details without losing report context

Use `/pricing` to inspect pricing coverage for the active report scope and
`/range` to inspect or set the UTC time range. The report distinguishes
provider-reported costs from catalog estimates; estimates are public list-price
calculations, not invoices. When price evidence is missing, the report keeps
cost unknown rather than treating it as zero. See the
[usage guide](../../usage.md#reports) for the meanings and limits of each cost basis.

The linked core v0.8.0 adds validated parser checkpoints for supported Claude
and Codex appends. Checkpoints are used only after source history is verified;
rewrites and unsupported cases retain the full-replay recovery path. This does
not mean the collector reads only newly appended bytes: it still verifies the
observed source history.

Install or upgrade with the [installation guide](../../installation.md) and
[upgrade guide](../../upgrades.md). The standalone CLI reads provider files without
modifying them and stores normalized usage data locally; see the
[privacy guide](../../privacy.md) for details.
