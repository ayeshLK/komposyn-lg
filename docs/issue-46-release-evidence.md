# Issue #46 standalone release evidence

This ledger separates repository changes from evidence that only a real signed
candidate or maintainer-controlled GitHub settings can provide. A green smoke
test is not full installer or lifecycle evidence.

## Accepted baseline

| Item | Value | Evidence |
| --- | --- | --- |
| Core release / required module | `v0.7.0` | `go.mod`, `.release-please-manifest.json` |
| Extraction source commit | `d66b4117d8b7771f9c3c22b3b251e19054ba9e88` | `docs/EXTRACTION.md` |
| Protocol / schema | 1 / 15 | `docs/HARNESS_PROTOCOL.md`, core migration ledger |
| Proposed first standalone CLI | `v0.7.1` | `CHANGELOG.md`; Release Please has not yet generated its release PR, so the proposal remains unconfirmed |
| Extraction merge | PR #1, merge commit `1bd04fa` | https://github.com/ayeshLK/komposyn-lg/pull/1 |
| Core verification fixes | PR #131, merge commit `c95be94` | https://github.com/ayeshLK/komposyn/pull/131 |
| Core release signing identity | `https://github.com/ayeshLK/komposyn/.github/workflows/release.yml@refs/heads/master` | Historical release verification retains this identity. |
| Core v0.7.0 checksum manifest | SHA-256 `82057898bf5906585087108ed0a29db32fe99226a23b913d671e300c0df482e2` | Downloaded from the published `ayeshLK/komposyn` v0.7.0 release; native fixture CI verifies this manifest signature and the selected target checksum before use. |

The six historical `v0.7.0` archive checksums in that manifest are:

```text
b0af004ab8028d8ce1980801bf2ecde47e6c56acfaba4e14026abee9f11fa30b  komposyn-lg_0.7.0_darwin_amd64.tar.gz
99c563e939328adf6a8e4d5cc5d22edfaba9e751036c1d1e360a27447e77a76b  komposyn-lg_0.7.0_darwin_arm64.tar.gz
a1e1930d131a5f235beda80bbbb19ca956485ebaac819cbdeb1082bf665e79f9  komposyn-lg_0.7.0_linux_amd64.tar.gz
0470314209d677ab6c5074507f3b199f58b88bef69dec70c4568e516759f1760  komposyn-lg_0.7.0_linux_arm64.tar.gz
06edc6ea9756221791d95442cc5d5567c2a29439f34f26cf84edb53b7725cf38  komposyn-lg_0.7.0_windows_amd64.zip
c630e827c6adbd169f787f8783122050bf899e33c53bf6600fa4cbe120f8662b  komposyn-lg_0.7.0_windows_arm64.zip
```

## Evidence matrix

| Gate / scenario | Target(s) | Command or workflow | Evidence / result |
| --- | --- | --- | --- |
| Historical published `v0.6.0` migration, restore, and rollback fixture | Linux, macOS, Windows; amd64 and arm64 | [CI `published-upgrades`](https://github.com/ayeshLK/komposyn-lg/blob/master/.github/workflows/ci.yml); `scripts/verify-published-upgrades.sh` | Five targets passed in [workflow run 39055074199](https://github.com/ayeshLK/komposyn-lg/actions/runs/39055074199), commit `06e103d`. Windows ARM64 exposed that Git Bash reports the emulated amd64 process architecture; commit `34794fc` now queries native .NET `OSArchitecture`. Six-target rerun pending. |
| Historical `v0.7.0` same-schema replacement | All six targets | Same CI job | Same five targets passed in run `39055074199`; Windows ARM64 rerun pending the native architecture correction in `34794fc`. |
| Signed candidate archive, manifest, architecture, provenance, metadata, protocol v1 smoke | All six targets | [Release workflow](https://github.com/ayeshLK/komposyn-lg/blob/master/.github/workflows/release.yml); `scripts/verify-signed-runtime.sh` | Smoke only; pending first real signed candidate. Full install/uninstall is a separate step. |
| Signature-verifying installer install, replacement, uninstall, and data preservation | All six targets | [Release workflow](https://github.com/ayeshLK/komposyn-lg/blob/master/.github/workflows/release.yml) | Pending first real signed candidate. |
| Active historical background collector: cooperative stop, executable replacement, new process/lease identity, automatic restart, one healthy owner, data/history preservation, restoration and uninstall | All six targets | [Release workflow](https://github.com/ayeshLK/komposyn-lg/blob/master/.github/workflows/release.yml); `scripts/verify-active-signed-candidate-upgrade.{sh,ps1}` | Pending first real signed candidate. Candidate signatures must match the exact standalone identity; v0.6.0 signatures retain the core identity. |
| Verify all six signed archives and `SHA256SUMS` before publication | Linux verifier | Release `verify-signed-artifacts` | Added as a required publication dependency; pending first real signed candidate. |
| Exact trust identity | Standalone | `scripts/test-release-provenance.sh`; installer tests and signed candidate verifiers | The standalone identity is `https://github.com/ayeshLK/komposyn-lg/.github/workflows/release.yml@refs/heads/master`; issuer is `https://token.actions.githubusercontent.com`. Historical fixture trust remains scoped to the core identity. |
| Native run evidence for six published targets | All six targets | Release workflow jobs above | No standalone release has been published; runtime and lifecycle results are pending. |
| Release Please version proposal | Standalone | Manually dispatched “Prepare release” workflow | Not yet generated. `CHANGELOG.md` proposes `v0.7.1`; confirm the actual Release Please PR before publication. |
| Master branch protection / ruleset | Standalone repository | GitHub repository settings audit, 2026-10-17 | Unmet: master protection endpoint returned 404 and repository rulesets were empty. Maintainer must configure required CI/review protections. |
| Protected release environment | Standalone repository | GitHub repository settings audit, 2026-10-17 | Unmet: `release` environment did not exist. Configure required reviewer protection before publication. |
| Actions policy | Standalone repository | GitHub repository settings audit, 2026-10-17 | Unmet: Actions allowed all actions and SHA pinning was disabled. Workflows currently pin third-party actions; maintainer must decide and configure repository policy. Workflow token defaults are read-only. |
| CodeQL and dependency updates | Standalone repository | `.github/workflows/codeql.yml`, `.github/dependabot.yml`, merged PR CI | Configured. Revalidate on this PR and confirm alerts/settings in GitHub before publication. |
| Cross-repository compatibility guardrails | Core and standalone | Core PR #131 and standalone PR #1 | Merged. Revalidate workflow checks on this PR; keep combined core CLI/release authoritative through signed standalone publication and post-publication verification. |
| Migration backup failure preservation acceptance item | Core | Core migration test suite | Still requires issue #46 evidence review; no new test is claimed by this standalone PR. |

## Publication gate

The release workflow must be dispatched only after the maintainer has reviewed
the Release Please PR, configured repository controls, and accepted native
historical checks. Its publication job now depends on all six-target signed
runtime/install checks, six-target active historical-collector upgrades, and
verification of the exact standalone identity on all six archives and the
checksum manifest. This repository change does not itself provide signed
candidate evidence. After publication, verify the release assets and rerun the
installed upgrade/restoration/uninstall checks before treating standalone as
authoritative or planning core cleanup.
