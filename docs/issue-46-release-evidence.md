# Issue #46 release and cutover evidence

This ledger records verified gates and separates completed publication evidence
from the remaining core ownership cleanup. A passing smoke test is not full
lifecycle coverage.

## Accepted and released baseline

| Item | Verified value | Evidence |
| --- | --- | --- |
| Final combined/core baseline | `v0.7.0`; final combined CLI version `0.7.0` | Core release and tag history |
| Extraction commit | `d66b4117d8b7771f9c3c22b3b251e19054ba9e88` | [Extraction provenance](EXTRACTION.md) |
| First standalone CLI | `v0.8.0`, requiring core `v0.7.0` | `go.mod`, release metadata |
| Standalone release source | `9369bc327e70928c69b332a4aa54ad38de5a226c` | [Publication run 39102174717](https://github.com/ayeshLK/komposyn-lg/actions/runs/39102174717) |
| Harness protocol / SQLite schema | `1` / `15` | [Protocol contract](HARNESS_PROTOCOL.md), core migration ledger |
| Published `SHA256SUMS` | SHA-256 `70e7d41f001f167f625d8f8cf4f31f7cbc241c2c6b1e05329310adc2aba0b527` | Published release assets |

## Completed publication evidence

| Gate | Targets and evidence | Result |
| --- | --- | --- |
| Signed archive and checksum verification | All six targets; run 39102174717 and independent asset verification | Passed: Linux/macOS amd64 and arm64, Windows amd64 and arm64. Exact standalone signer identity and OIDC issuer verified; checksums, archive contents, licenses, target architecture, and version metadata matched. |
| Signed runtime, protocol, and installer lifecycle | All six native targets in run 39102174717 | Passed: signed fresh install, replacement, runtime metadata, protocol-v1 smoke, and data-preserving uninstall. |
| Historical active-collector upgrade | All six native targets in run 39102174717 | Passed from signed historical v0.6.0: cooperative stop, executable replacement, automatic restart, new process/lease identity, one healthy owner, preservation of usage, settings, consent, identities, cursors, diagnostics/review state, collector history and backup restoration. |
| Post-publication verification | Run 39102174717 and independent verification | Passed: published assets byte-compared with tested artifacts, signatures/checksums reverified, and Linux v0.7.0-to-v0.8.0 install/upgrade/uninstall lifecycle passed. |
| Migration failure preservation | Core PR #132, merged at `b870368091fe80ba325514de7ba22c8496f4aec4` | Passed: `TestMigrationFailureLeavesOriginalAndVerifiedBackupUsable` verifies failed migration rollback and usable original and backup snapshots. This test change is not in standalone core v0.7.0. |
| macOS registry verifier path correction | Standalone PR #11, merged in release source commit `9369bc327e70928c69b332a4aa54ad38de5a226c` | Passed in the six-target signed active-collector verification above. |

The standalone workflow identity is exactly
`https://github.com/ayeshLK/komposyn-lg/.github/workflows/release.yml@refs/heads/master`
with issuer `https://token.actions.githubusercontent.com`. Historical combined
artifacts retain their original core identity:
`https://github.com/ayeshLK/komposyn/.github/workflows/release.yml@refs/heads/master`.
Installers must never accept both identities for new artifacts.

## Compatibility and security procedures

- Minimum supported core and current CLI dependency pin are both `v0.7.0`.
  Normal CLI CI tests that pin once; an extra identical minimum-version job
  would be redundant. See [core compatibility and coordinated security](core-compatibility-security.md).
- Core PR [#133](https://github.com/ayeshLK/komposyn/pull/133) proposes a CI
  job for the public standalone CLI suite using a temporary dependency override
  in disposable storage, with read-only permissions and no cross-repository
  credentials. The same candidate-core test passed locally at core commit
  `9301710a9efe8b1e2447040ebe281d383e12a0d1`; the GitHub job remains unverified
  until the PR runs. No permanent `replace` directive is allowed.
- Dependency-update review covers public APIs, migrations and data recovery,
  protocol v1, exact JSON/CSV output, security/privacy notes, CLI/core/schema
  metadata, vulnerability scan, and release validation.
- The standalone CLI support policy covers only the latest published CLI
  release (`v0.8.0` at adoption); older releases are unsupported, and `master`
  is development source rather than a supported release line.
- Each repository owns advisories for code it owns. Cross-repository security
  fixes are prepared privately and coordinated so the fixed core tag and
  compatible signed CLI release are ready together; advisories map affected
  and fixed versions in both repositories and include explicit upgrade steps.

## Repository controls rechecked

The standalone `master` protection currently requires PRs, one approval,
latest-push approval, resolved conversations, strict required checks, and
blocks force pushes/deletion. Admin enforcement remains disabled under the
maintainer's accepted sole-admin bypass policy. The `release` environment
requires approval from `ayeshLK` and only allows deployments from protected
branches. API read-back still reports `can_admins_bypass: true`; disabling that
setting requires maintainer UI access at
[Settings → Environments → release](https://github.com/ayeshLK/komposyn-lg/settings/environments)
by clearing **Allow administrators to bypass configured protection rules**.
This is separate from the accepted branch-admin bypass policy.

## Remaining cutover work

The standalone repository is authoritative for the executable and its release
lifecycle after verified v0.8.0 publication. Core cleanup and candidate
compatibility are proposed in [core PR #133](https://github.com/ayeshLK/komposyn/pull/133).
CLI dependency/security procedure updates are proposed in [standalone PR #12](https://github.com/ayeshLK/komposyn-lg/pull/12).
Both PRs are open; their changes remain incomplete until they merge and the
merged-master checks pass:

- Remove CLI implementation, CLI-only policy, installers, binary packaging,
  and CLI publication code from core.
- Publish the core as a reusable module with source-only Release Please tags and
  release notes; retain the old combined releases and verification guidance.
- Run core candidate CI against public standalone CLI tests with a disposable
  replacement and no credentials/write access.
- Merge dependency-update and coordinated security-release guidance into both
  repositories and verify links/checks in both.

The release environment still reports `can_admins_bypass: true`. The maintainer
must clear **Allow administrators to bypass configured protection rules** in
[Settings → Environments → release](https://github.com/ayeshLK/komposyn-lg/settings/environments)
and verify the API read-back. This is separate from the intentionally retained
branch-administrator bypass policy.

Do not close issue #46 until these cleanup changes merge, all checks pass, and
the release-environment administrator-bypass follow-up is explicitly resolved
or accepted by the maintainer.
