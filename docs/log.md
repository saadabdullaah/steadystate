# Engineering Log

## 2026-07-13 — Phase 0 foundation

### Done

- Established Windows Git and PowerShell as the primary local workflow.
- Protected private planning material from Git tracking.
- Selected a maintained Gateway API implementation after validating dependency lifecycle.
- Defined pinned tools, cluster profiles, CI gates, and failure diagnostics.
- Enabled protected-branch governance, security scanning, dependency updates, and immutable required checks.
- Proved the minimal profile and all required quality, security, Windows, CodeQL, and cluster checks in GitHub Actions.

### Next

- Begin Phase 1 API and controller design on top of the validated foundation.

## 2026-07-13 — Standard-profile topology correction

### Done

- Pinned Envoy Gateway data-plane pods to the kind node that owns the host-port mappings.
- Preserved cross-node service routing with `externalTrafficPolicy: Cluster`.
- Revalidated the complete required-check suite in GitHub Actions.
- Proved a standard-profile bootstrap, destroy, clean re-bootstrap, and final idempotent cleanup.

## 2026-07-13 — Windows acceptance

### Done

- Validated Docker Engine 29.6.1 with Linux containers and cgroup v2 through `doctor`.
- Completed a cold standard-profile bootstrap locally in 8.9 minutes.
- Reproved Gateway API ingress and Calico NetworkPolicy enforcement on Windows.
- Hardened the positive-connectivity proof against transient Service endpoint programming.
- Converged the existing cluster in 1.2 minutes, then proved destroy is safe both when present and absent.

## 2026-07-13 — Phase 1 Application API and reconciliation

### Done

- Added the namespaced `Application` API, defaults, CEL validation, printer columns, generated CRD, and least-privileged RBAC in PR #6.
- Added deterministic builders and watch-driven reconciliation for Deployment, Service, ConfigMap, and HTTPRoute in PR #8.
- Added controller owner references, finalizer handling, truthful observed status, conflict retry, unsupported-capability reporting, and zero-write idempotency coverage.
- Added the in-cluster operator runtime, hardened demo application, shared Gateway integration, and destructive self-heal harness in PR #9.
- Recovered and audited all GitHub PR refs after the local environment loss; no unmerged Phase 1 feature branch remained.
- Passed the hosted standard-profile round trip and destructive acceptance test in [Nightly Integration run 29260395935](https://github.com/saadabdullaah/steadystate/actions/runs/29260395935).
- Recreated Deployment, Service, ConfigMap, and HTTPRoute with new UIDs; Deployment recreation completed in 0.300 seconds and replica drift repair in 0.435 seconds.
- Proved HTTP 200 through Envoy Gateway after repair, truthful `Ready=True`, finalizer release, and garbage collection of every owned child.
- Generated the checksum-verified VHS recording and schema-versioned JSON evidence for the Phase 1 closeout.

### Release outcome

- Merged the diagnostics-inclusive Phase 1 closeout in PR #10 at `8229c7b`.
- Passed the required post-merge CI, Nightly Integration, and CodeQL gates on the exact release commit.
- Published the annotated `v0.1.0` tag and GitHub release with the hosted GIF and JSON acceptance evidence.

## 2026-07-14 — Phase 2 tenancy design

### Done

- Froze `v0.1.0` as the immutable Phase 1 baseline.
- Merged the fully green GitHub Actions dependency update in PR #3 at `2d7ebfd`, closed stale Go dependency PR #7, and froze the resulting dependency baseline for Phase 2.
- Approved the cluster-scoped `Team` API, managed Namespace lifecycle, label-and-UID ownership model, protected tenant RBAC, default-deny networking, Envoy Gateway ingress selector, repository glob contract, and Application tenancy guard.
- Split Phase 2 into bounded API, controller, Application integration, and hosted acceptance closeout pull requests.
- Merged the Team API and deterministic Namespace, quota, LimitRange, RBAC, and NetworkPolicy builders in PR #11 at `36eeebf`.
- Merged the watch-driven Team controller, exact-UID non-adoption boundary, drift repair, staged status, and safe Namespace finalization in PR #12 at `6dac808`.
- Passed quality, Windows, envtest, security, and kind-smoke for the Team runtime in CI run `29315523417`.
- Merged the Application tenancy guard, Team and Namespace dependency watches, repository authorization, Team-aware demo path, and install-time owner ClusterRole in PR #13 at `21ca1a5`.
- Passed all five CI jobs in run `29324628300` and the hosted standard-profile operator path in Nightly run `29322107063`.
- Added a hosted Phase 2 acceptance harness with explicit Calico, concurrent application, cross-team network and RBAC, Gateway, repository, unmanaged namespace, quota, and isolated Team-deletion proofs.
- Passed quality, Windows, envtest, security, and kind-smoke on the Phase 2 acceptance candidate `b3b56a9` in CI run `29337911712`.
- Passed all ten revision-bound Phase 2 checks in hosted standard-profile Nightly run `29337904627`; evidence artifact `phase2-acceptance-b3b56a95249fcdd7ac6d38be925d55f29bc169dd` (artifact `8313037526`, SHA-256 `b4b64434faa8ea6665e662b737d1518a353cb0cbaffa1c1a7c5b085a027bcf85`) contains the JSON result, rendered fixtures, and diagnostics.

### Release outcome

- Merged the Phase 2 acceptance closeout in PR #14 at `d67aa3b`.
- Passed all five post-merge CI jobs in run `29344460290`, CodeQL analysis in run `29344460213`, and all post-merge hosted acceptance steps in Nightly run `29344492583`.
- Retained merge-bound evidence as `phase2-acceptance-d67aa3b83ef7167d8dc1e3863c5598d60ed11533` (artifact `8315782226`, SHA-256 `f037f4b501b734be14d3f1336ef80a38deb7a2bebc5b2cac05731625d95d39c2`) and prepared permanent JSON evidence for the annotated `v0.2.0` release.

## 2026-07-17 - Phase 3 GitOps and automated delivery

### Runtime provenance checkpoint

- Merged PR #16 at `20d984c` with Pod watches, canonical CRI digest parsing, `resolvedImageDigest`, `resolvedGitRevision`, source-revision validation, active-release tuple preservation, status conflict retry, and revision-only zero-write behavior.
- Passed quality, Windows, envtest, security, and kind-smoke on checkpoint head `c4a24d9` in CI run `29374119238`.

### Argo CD foundation checkpoint

- Merged PR #17 at `4947a0b` with checksum-pinned Argo CD v3.4.2, Dex removal, shared-Gateway routing, exact revision inheritance, constrained AppProjects, sync waves, annotation tracking, Lua health, and deterministic Helm/Kustomize structural tests.
- Passed all five CI jobs in run `29410393004`.
- Passed the hosted standard-profile Argo foundation proof in Nightly run `29410404432`. Artifact `8341115414` (`phase3-gitops-foundation-4ba2b51...`) has SHA-256 `0ff29fedde0ee534e61608e670fb1b20f11944b6038ae1603e7c756f7323b231`.

### Demo delivery automation checkpoint

- Merged PR #18 at `8d1f1af` with authoritative demo `VERSION`, required version-bump validation, serialized immutable GHCR publication, tag-reuse digest checks, and repository-scoped delivery App pull requests.
- Passed all five CI jobs in run `29448337438`.
- Demo release run `29448884694` published public `v0.3.0` and full-source-SHA tags, then created delivery PR #19. The generated PR passed all five checks in run `29450652467` and merged at `7122c07`.
- Merged PR #20 at `ddf0019` to align hosted delivery validation with the automated manifest state; all five checks passed in run `29449593346`.

### Hosted acceptance closeout

- PR #21 candidate `7f29303` runs Phase 3 after the Phase 1 and Phase 2 proofs, creates and removes a repository-scoped ephemeral branch, records the real test through pinned VHS, and guarantees diagnostics plus bounded cleanup.
- Hosted Nightly run `29570255069` passed every step and all 14 Phase 3 assertions: pinned Argo and Dex absence, UI routing, root/platform/tenant health, baseline reachability, Git-only candidate delivery, digest and revision provenance, Degraded rejection, Healthy recovery, operator-outage UID preservation, zero-drift restart, and the Argo/operator ownership boundary.
- Artifact `8403532367` (`phase3-acceptance-7f293037...`) is 5,661,842 bytes with SHA-256 `0918fbb1c25b393291a6bd248d549f56027463d8befe3c495b7074b96b06f094`. It contains the schema-versioned JSON, real GIF, rendered states, registry metadata, Argo snapshots, controller logs, and cluster diagnostics.
- The hosted GIF is 3,320,344 bytes with SHA-256 `650b5794f9111ad0972f74ed3aa5812b97d47b0b5087d39bf84a982f89f9df6d`.

### Publication gates

- Merge PR #21 only after its five required checks pass on the evidence-and-documentation commit.
- Run CI, 40-minute CodeQL, and Nightly Integration on the exact merged `main` commit before publishing the annotated `v0.3.0` tag and GitHub release.

## 2026-07-19 - Phase 4 progressive delivery and automatic rollback

### Design gate and GitOps foundation

- Merged PR #22 at `0088394` with frozen Rollouts `2.41.0`/controller `v1.9.0`, Gateway plugin `v0.16.0`, kube-prometheus-stack `87.16.1`, k6 `v2.1.0`, checksum enforcement, constrained projects/RBAC, trimmed monitoring, and ADR-0007.
- Passed all five CI jobs in run `29587019868` and the hosted Envoy weighted-routing/monitoring compatibility proof in Phase 4 run `29587019867` on checkpoint head `2eda385`.
- Retained artifact `8409663696` (`phase4-foundation-65c27ea...`, 1,817,039 bytes) with GitHub SHA-256 `092d95270cca1ee825404a60867be984c2b2d25f3b9014ca0f43d04e68b1df6c`.

### Demo telemetry and immutable variants

- Merged PR #23 at `0dfa317` with deterministic error/latency/crash injection, RED metrics, health exclusions, race-tested configuration boundaries, `VERSION=v0.4.0`, and immutable good/bad publication contracts.
- Passed all five CI jobs in run `29588945870`; hosted foundation run `29588946834` retained artifact `8410432249` with GitHub SHA-256 `c6b4c51d858b1e7e4896ae440c89a75fdf0737c5161db63fa7b793e8619f8afd`.
- Demo release run `29590086934` published public `v0.4.0`, `v0.4.0-bad`, and both full-source-SHA variants from merge `0dfa317`, verified immutable digests, and opened delivery PR #24.
- Generated PR #24 passed all five CI jobs in run `29590193617` and merged at `89382cf`, changing only the normal demo GitOps image tag.

### Deterministic progressive-delivery resources

- Merged PR #25 at `32e74ac` with typed Rollout builders, stable/canary Services, weighted HTTPRoute integration, inline metric analysis, ServiceMonitor/PrometheusRule resources, suffix-safe names, vendored CRDs, scheme registration, least-privileged RBAC, and deterministic structural/golden coverage.
- Passed all five CI jobs in run `29597577828` and hosted foundation run `29597577840`; artifact `8413934573` has GitHub SHA-256 `b16e5bab1be75d7491cfe394cb0eece3fff9a1414a17e2f20737287a2ceef873`.

### Controller and reversible migration

- Merged PR #26 at `7d3d5bb` with active canary reconciliation, Rollout/AnalysisRun/monitoring watches, truthful rollout status, provider/no-traffic fail-safe behavior, field-owner-aware drift repair, deleted-Rollout recovery from the last healthy tuple, and restart-safe rolling↔canary migration.
- Passed all five CI jobs in run `29622523680` and the non-recorded hosted controller flow in Phase 4 run `29622523675`; artifact `8422801964` has GitHub SHA-256 `592dca3b34b0cd3d10798ecf328d2561af3133e103b80b29abf0e572cd8b1b59`.

### Hosted acceptance closeout

- PR #27 candidate `ff3e419` adds the separate bounded Phase 4 workflow, Git-only ephemeral delivery sequence, independent VHS execution/recording, success and failure diagnostics, strict artifact completeness, and unconditional branch/GitOps/cluster cleanup.
- CI run `29681093152` passed quality, Windows, envtest, security, and kind-smoke. Phase 4 run `29681093123` passed all workflow steps, including both recordings, evidence verification, artifact upload, ephemeral branch deletion, GitOps teardown, and cluster destruction.
- All 12 schema-versioned checks passed. Good traffic measured 11.8%, 26.2%, 52.0%, and 100.0% canary shares from 500 requests per window. The bad candidate measured 12.0% at its 10% step, fired its alert, aborted automatically, preserved the active tuple, and was followed by three 30-second stable-only windows containing 4,038, 4,262, and 4,364 successful requests.
- Both strategy migrations completed without a routing failure. Final active state was `v0.4.0`, digest `sha256:b362da3460289de02c0f0d9ade9120ca3549de329c180bda2cfe40ea3c63233e`, at the exact final Git revision. The monitoring working set was 383,983,616 bytes, below the approximately 1.2 GiB budget.
- Artifact `8440858967` (`phase4-acceptance-0e99a028...`, 6,379,903 compressed bytes) has GitHub SHA-256 `8ebecbfb3517f850e88eaac375fad2fe09efb5ab357a2564f7f54e1590337b95`. It retains both tapes/GIFs, JSON evidence, five traffic measurements, k6 scripts/output/summaries, AnalysisRuns, Alertmanager and registry metadata, rendered GitOps/resources, snapshots, controller logs, and 158 diagnostic files.
- The promotion GIF is 461,583 bytes with SHA-256 `9a29885ce75065721f16f13ab6472de9d680ebd8ae504546c4ff17d798409b42`; the rollback/recovery GIF is 1,930,829 bytes with SHA-256 `17bc9d8188f5692f1c04423d57926b3e35fa4d94aa47c014dc533150d37960fc`. Evidence JSON SHA-256 is `0bbe40d446ea7216af27a8fe7eaabeae18d6b06bf83dcfd1a1d508bafc02eac9`.

### Publication gates

- Commit the hosted GIFs and documentation, then require the rerun of all five CI jobs, CodeQL, and branch Phase 4 acceptance before merging PR #27.
- After the squash merge, run exact-`main` CI, 60-minute CodeQL, existing Nightly Integration, and Phase 4 acceptance; retain and verify the exact-main artifact before tagging and releasing `v0.4.0`.
