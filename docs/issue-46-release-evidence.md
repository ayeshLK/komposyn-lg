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
| Proposed first standalone CLI | Previous Release Please attempt proposed `v0.8.0` | Workflow run `39085987339` created branch commit `dfde8e8` but could not open a PR. PR #8 fixed the component-prefixed tag configuration. The actual proposal after that fix remains unconfirmed; run Prepare release and review its generated PR. |
| Extraction merge | PR #1, merge commit `1bd04fa` | https://github.com/ayeshLK/komposyn-lg/pull/1 |
| Earlier verification preparation | PR #126, merge commit `43cb71014680abbe57011885a150604eef6b93a6` | https://github.com/ayeshLK/komposyn/pull/126 |
| Published-release verification fixes transferred to standalone | PR #130, merge commit `2c27610b0cfc3dc4d7ff55bfea0b405b810afbea`; applied to `verify-release.sh`, `expand-release-archive.ps1`, and release-resolution scripts | https://github.com/ayeshLK/komposyn/pull/130 and [standalone extraction record](EXTRACTION.md) |
| Core verification and historical upgrade gates | PR #131, merge commit `c95be94` | https://github.com/ayeshLK/komposyn/pull/131 |
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
| Historical published `v0.6.0` migration, restore, and rollback fixture | Linux, macOS, Windows; amd64 and arm64 | [CI `published-upgrades`](https://github.com/ayeshLK/komposyn-lg/blob/master/.github/workflows/ci.yml); `scripts/verify-published-upgrades.sh` | Passed on all six native targets in [workflow run 39056595195](https://github.com/ayeshLK/komposyn-lg/actions/runs/39056595195), head `423387fd1dbdc3b7ee4675564e0e76aeb9c94797`. Historical archives were signature- and checksum-verified with their core signer identity. |
| Historical `v0.7.0` same-schema replacement | All six targets | Same CI job | Passed on all six targets in [workflow run 39056595195](https://github.com/ayeshLK/komposyn-lg/actions/runs/39056595195), head `423387f`. |
| Native archive runtime smoke: target architecture, CLI/core metadata, protocol v1, archive contents/checksums, upgrade, restoration, uninstall/data preservation | All six targets | Same CI job; `scripts/verify-release.sh` | Passed on all six native targets in [workflow run 39056595195](https://github.com/ayeshLK/komposyn-lg/actions/runs/39056595195). This exercises unsigned PR candidate archives plus signed historical baselines; it is not signed standalone candidate evidence. |
| Local release build and full install/replacement/uninstall lifecycle | Local Linux amd64; archive set includes all six targets | `make release VERSION=v0.7.1 OUTPUT_DIR=/tmp/komposyn-lg-readiness-release-final`; `bash scripts/verify-release.sh /tmp/komposyn-lg-readiness-release-final` | Passed locally. This does not establish signed-candidate verification on all targets. |
| Module, vet, unit, race, trust, and provenance checks | Local Linux | `go mod verify`, `go vet ./...`, `go test ./...`, `go test -race ./...`, `make check`, `bash -n scripts/*.sh`, `scripts/test-installer-trust.sh`, `scripts/test-release-provenance.sh` | Passed locally; PR CI reruns formatting, modules, vulnerability scan, vet, tests, builds, race, and native Windows PowerShell checks. |
| Signed candidate archive, manifest, architecture, provenance, metadata, protocol v1 smoke | All six targets | [Release workflow](https://github.com/ayeshLK/komposyn-lg/blob/master/.github/workflows/release.yml); `scripts/verify-signed-runtime.sh` | Smoke only; pending first real signed candidate. Full install/uninstall is a separate step. |
| Signature-verifying installer install, replacement, uninstall, and data preservation | All six targets | [Release workflow](https://github.com/ayeshLK/komposyn-lg/blob/master/.github/workflows/release.yml) | Pending first real signed candidate. |
| Active historical background collector: cooperative stop, executable replacement, new process/lease identity, automatic restart, one healthy owner, data/history preservation, restoration and uninstall | All six targets | [Release workflow](https://github.com/ayeshLK/komposyn-lg/blob/master/.github/workflows/release.yml); `scripts/verify-active-signed-candidate-upgrade.{sh,ps1}` | Pending first real signed candidate. Candidate signatures must match the exact standalone identity; v0.6.0 signatures retain the core identity. |
| Verify all six signed archives and `SHA256SUMS` before publication | Linux verifier | Release `verify-signed-artifacts` | Added as a required publication dependency; pending first real signed candidate. |
| Exact trust identity | Standalone | `scripts/test-release-provenance.sh`; installer tests and signed candidate verifiers | The standalone identity is `https://github.com/ayeshLK/komposyn-lg/.github/workflows/release.yml@refs/heads/master`; issuer is `https://token.actions.githubusercontent.com`. Historical fixture trust remains scoped to the core identity. |
| Native smoke evidence for all six target builds | All six targets | CI workflow run [39056595195](https://github.com/ayeshLK/komposyn-lg/actions/runs/39056595195) | Passed for the v0.7.1 candidate archive build. |
| Published signed-release verification and full lifecycle after publication | All six targets | [Release workflow](https://github.com/ayeshLK/komposyn-lg/blob/master/.github/workflows/release.yml) | Pending. No standalone release/tag exists. The workflow will compare published assets byte-for-byte with tested signed artifacts and run a post-publication Linux lifecycle check; six-target install/lifecycle checks run on the signed candidate before publication. |
| Release Please version proposal | Standalone | [Prepare release run 39085987339](https://github.com/ayeshLK/komposyn-lg/actions/runs/39085987339) | That pre-PR-#8 attempt proposed `0.8.0` because the extraction commit is `feat`, generated branch commit `dfde8e829b4cad5defca4126d4bc4315750af2b8`, then failed before PR creation with “GitHub Actions is not permitted to create or approve pull requests.” It also exposed component-prefixed compare/tag output. PR #8 fixes `include-component-in-tag`; the actual post-fix proposal is unconfirmed until Prepare release is run again. |
| Master branch protection / ruleset | Standalone repository | [Branch settings](https://github.com/ayeshLK/komposyn-lg/settings/branches), GitHub API read-back, 2026-10-18 | Configured on `master`; the API confirms pull requests required, strict/up-to-date checks, administrators enforced, resolved conversations, and force-push and deletion blocks. Required GitHub Actions contexts are `test (ubuntu-latest)`, `test (macos-latest)`, `test (windows-latest)`, `test (windows-11-arm)`, `race`, `historical-fixture-artifacts`, `published-upgrades (ubuntu-24.04, linux_amd64)`, `published-upgrades (ubuntu-24.04-arm, linux_arm64)`, `published-upgrades (macos-15-intel, darwin_amd64)`, `published-upgrades (macos-latest, darwin_arm64)`, `published-upgrades (windows-latest, windows_amd64)`, `published-upgrades (windows-11-arm, windows_arm64)`, and `analyze` (CodeQL). These exact contexts completed successfully on PR #7 head `15e006aff87dcff2163ce88ea2dfbaa028608c50` in [run 39084391819](https://github.com/ayeshLK/komposyn-lg/actions/runs/39084391819). The only direct collaborator with write/admin access is `ayeshLK`; to avoid a self-approval deadlock, required approvals are 0, stale-review dismissal is enabled, and latest-push approval is disabled. Recommended policy when another eligible reviewer is added: require one approval, dismiss stale approvals, and require approval of the latest push. No repository ruleset was added; active branch protection supplies the controls. |
| Protected release environment | Standalone repository | [Environment settings](https://github.com/ayeshLK/komposyn-lg/settings/environments), GitHub API read-back, 2026-10-18 | `release` now requires approval from `ayeshLK` (the only maintainer), allows that person to approve a run they triggered, has no wait timer, and permits deployments only from protected branches (`master`). The environment API read-back reports `can_admins_bypass: true`; its create/update endpoint rejects that field, so administrator bypass still needs to be disabled in the GitHub settings UI before treating the environment gate as non-bypassable. |
| Actions policy and Release Please authentication | Standalone repository | [Actions settings](https://github.com/ayeshLK/komposyn-lg/settings/actions), workflow run [39085987339](https://github.com/ayeshLK/komposyn-lg/actions/runs/39085987339), API read-back, 2026-10-18 | Configured the existing fallback path: `default_workflow_permissions` remains `read`, while `can_approve_pull_request_reviews` is `true` so the job's explicitly scoped `contents: write` and `pull-requests: write` permissions can create/update the Release Please PR. The workflow uses `secrets.RELEASE_PLEASE_TOKEN || github.token`; no repository secret is configured, so the next run will use the short-lived, repository-scoped `GITHUB_TOKEN`. GitHub puts PR workflow runs created with this token into an approval-required state; after the PR opens, a maintainer with write access must select **Approve workflows to run** so required CI and CodeQL checks execute ([GitHub documentation](https://docs.github.com/en/actions/concepts/security/github_token)). This setting and path are configured, but Prepare release has not been rerun and no new release PR is confirmed. Third-party Actions remain SHA-pinned; this task did not change workflow files or application code. |
| CodeQL and dependency updates | Standalone repository | `.github/workflows/codeql.yml`, `.github/dependabot.yml`, merged PR CI | Configured. Revalidate on this PR and confirm alerts/settings in GitHub before publication. |
| Cross-repository compatibility guardrails | Core and standalone | Core PR #131 and standalone PR #1 | Merged. Revalidate workflow checks on this PR; keep combined core CLI/release authoritative through signed standalone publication and post-publication verification. |
| Migration backup failure preservation acceptance item | Core | Core migration test suite | Still requires issue #46 evidence review; no new test is claimed by this standalone PR. |

## Publication gate

The release workflow must be dispatched only after the maintainer has reviewed
the Release Please PR, completed the remaining environment administrator-bypass
setting, and accepted native historical checks. Its publication job now depends
on all six-target signed runtime/install checks, six-target active
historical-collector upgrades, and
verification of the exact standalone identity on all six archives and the
checksum manifest. This repository change does not itself provide signed
candidate evidence. After publication, verify the release assets and rerun the
installed upgrade/restoration/uninstall checks before treating standalone as
authoritative or planning core cleanup.
