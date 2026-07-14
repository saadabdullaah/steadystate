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

### Next

- Merge the fully validated Phase 2 acceptance PR, repeat hosted acceptance on the exact merge commit, and publish `v0.2.0`.
