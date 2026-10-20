# Demonstration asset manifest

All sizes are from the checked-in files. Captions describe the corresponding scene; the GIF's six-second loop is a compressed visual overview of the 30.17-second source capture. WebVTT timestamps align with the source cast.

| Asset | Size | Caption / alt text | README use |
| --- | ---: | --- | --- |
| [komposyn-lg-demo.cast](assets/komposyn-lg-demo.cast) | 5.2 KB | Terminal recording: synthetic Codex and Claude Code usage; public list-price estimates are not provider-reported costs. Includes six real CLI scenes and exact JSON export. | Keep as reproducible/accessibility source; do not embed raw cast. |
| [komposyn-lg-demo.gif](assets/komposyn-lg-demo.gif) | 121 KB | KompoSyn CLI synthetic usage demo: aggregate report, projects, sessions, model usage, pricing coverage, and exact JSON values. | Suitable animated overview; link to cast/notes. |
| [overall-report.png](assets/overall-report.png) | 58 KB | Overall synthetic report for five requests across Codex and Claude Code, with token totals and estimated-versus-unknown cost coverage. | Static report screenshot. |
| [project-session-navigation.png](assets/project-session-navigation.png) | 17.4 KB | Synthetic Ledger Studio project with separate Codex and Claude Code sessions. | Shows project-to-session navigation. |
| [pricing-explanation.png](assets/pricing-explanation.png) | 60.6 KB | Synthetic pricing breakdown: Codex public list-price estimate is shown separately from three Claude requests whose cost is unknown without billing context. | Explains estimate provenance and missing evidence. |
| [synthetic-usage-v0.9.0.json](assets/synthetic-usage-v0.9.0.json) | 2.9 KB | Exact JSON usage report for the fixed synthetic fixture and report dates. | Link as machine-readable example; not an image. |
| [captions.vtt](captions.vtt) | 1.2 KB | Six synchronized captions for the 30.17-second cast. | Pair with a future captioned video export. |

## Captions

1. Synthetic example data combines Codex and Claude Code usage. The displayed estimate is a public list-price calculation; provider-reported cost is not present.
2. Browse two clearly synthetic projects and their usage totals.
3. Select Ledger Studio to inspect the separate Codex and Claude Code sessions.
4. Open a Codex session and inspect its exact model usage and estimate.
5. Cost coverage keeps the Claude estimate unknown because this synthetic fixture omits billing context.
6. Export exact JSON. Summary values: input_tokens 335000, calculated_cost_micros 346250, unknown_cost_requests 3.

## Privacy and provenance

Reviewed the recordings and fixture for captured provider histories, prompt or response text, credentials, home-directory paths, and real project locations. None are present. The only paths displayed in the fixture are explicitly synthetic /synthetic/workspaces/... examples. Recording metadata uses a generic title and command; no username or host path is embedded. Data directory and harness output paths used during regeneration are temporary and are not included in the JSON artifact.

The release executable was v0.9.0 (core v0.8.0); release manifest and Linux amd64 archive signature checks passed. The reproducible recording script and exact release checksum are documented in [README.md](README.md).
