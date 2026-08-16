# Engineering Log

## 2026-08-16 - Refined portal control ledger

- Reworked the embedded local-owner portal into the SteadyState control ledger
  while preserving the released typed API, broker, Kubernetes, telemetry,
  policy, and backup boundaries.
- Verified commit `221fab1e2a40d31b2ec762f66455b6adbd4139b5` in
  [Phase 9 acceptance run 31943997118](https://github.com/saadabdullaah/steadystate/actions/runs/31943997118).
  All five acceptance assertions passed, including the real GitHub App smoke
  proposal and cleanup; artifact `9263060350` has digest
  `sha256:bd333c3b6c0786ba071cd8197125e6498281e20e6b3e831b26c2baa47ec9068a`.
- The refreshed hosted GIF, WebM, and evidence JSON SHA-256 values are
  `b4b9b3f6e81f87588b58cb7465afde69287c96e4301f9e06b0ee3a099203e0e8`,
  `fd173d83484edfa64d5cc011e2746d2d2a36a62cdbdf68a40b83d7c47f453de4`,
  and `c2eb0ca81353a2247fa352606accd88801ac05f1bc4d62fc19475f342900e1c8`.
- Hosted measurements recorded a 484 ms initial render, 33,628,160-byte
  portal working set, and zero serious or critical accessibility violations.

## 2026-08-15 - v1.0.1 release closeout

- Squash-merged fresh-install hardening PR #111 as
  `cf3d8d53d89c296a0311c04b8819327f52772c59`. Its branch Phase 9 run
  `31888881140` retained artifact `9248286391`; the hosted GIF SHA-256 is
  `d7d47a04d24181f0ad76e5489b08ca39eef84eb553593e5c646338dc11aa7797`.
- Main-branch release runs `31891413953` and `31891413957` published signed,
  SPDX-attested `xyz v0.1.2` and demo `v0.7.1` images. App-authored delivery
  PR #118 merged as `ea6b41ed803372962abf8c0eb6d6b7bff9811a7d`; PR #119 merged as final
  delivery commit `5651068c83328fac22f4e4c12d6ee05b7ed44328`.
- Exact-main CI `31894424108`, parallel CodeQL `31894424105`, Nightly
  `31894456291`, Phase 4 `31894459297`, Phase 5 `31894457290`, Phase 6
  `31894457665`, Phase 7 `31894459604`, Phase 8 `31894459267`, Phase 9
  `31894459919`, and platformctl smoke `31894457361` all passed.
- Retained exact-main artifacts: Phase 1 `9249504549`, Phase 2 `9249518003`,
  Phase 3 `9249593065`, Phase 4 `9249722844`, Phase 5 `9249570963`, Phase 6
  `9249561710`, Phase 7 `9249802709`, Phase 8 `9249645903`, Phase 9
  `9249663392`, and release snapshot `9249469874`.
- The `v1.0.1` closeout updates installer defaults, portal version metadata,
  consumer verification examples, and cross-platform released-installer
  smoke. Publication remains gated on the closeout PR and exact release-commit
  validation.
- Fresh Windows lifecycle commands now prefer PowerShell 7 but fall back to
  the built-in Windows PowerShell 5.1 executable. The fallback applies a
  process-scoped execution-policy bypass only to the fixed repository-owned
  lifecycle script, removing an undocumented `pwsh` prerequisite without
  changing machine policy.
- The checksum-verifying Windows installer now makes its resolved install
  directory available in the current process and persistent user PATH, with
  `-NoPathUpdate` available for isolated or portable installations.
- GitHub operations prefer the user-owned `~/.local/bin/gh` binary before the
  ambient PATH, preventing an older administrator-owned installation from
  silently overriding the checksum-verified `2.97.0` security baseline.
- Fresh full-profile diagnostics treat not-yet-installed repository-local
  `sops` and `age` binaries as bootstrap warnings while continuing to require
  the owner-held age identity before any environment mutation.
- The retained SeaweedFS lifecycle parses Docker inspect JSON instead of using
  quoted dynamic Go templates, keeping subnet, network attachment, volume,
  memory, and environment verification identical under Windows PowerShell 5.1
  and PowerShell 7.

## 2026-08-15 — GA fresh-install hardening

- Reproduced the first external Windows installation path and corrected the
  installer architecture fallback, standard-profile database neutrality, and
  lifecycle behavior in the GA hotfix line.
- Made platform image builds source-addressed and retry-safe, moved cold image
  compilation before kind startup, streamed bounded stage progress, and added
  true Windows/Unix process-tree termination for deadlines.
- Added a fail-fast profile preflight: `standard` supports a 7 GiB Docker
  allocation, while the database/recovery `full` profile requires 9 GiB and no
  longer proceeds toward a predictable OOM failure below that boundary.
- Rebased the Go toolchain and builder to the Go 1.25.13 security patch and
  removed the duplicate blocking `govulncheck` execution while preserving its
  SARIF gate. The hosted security scan also advanced the generated web
  lockfile from `nanoid` 3.3.17 to the fixed 3.3.18 release and declared the
  demo builder change as patch version `v0.7.1`.
- Made pinned Go detection immune to `GOTOOLCHAIN=auto` masquerading, accepted
  environment-custodied SOPS identities without reading them, and kept
  uninspectable repository secret-name metadata as an explicit warning rather
  than a false missing-secret result under the scoped Actions token.
- Reworked the embedded portal from generic cards/raw JSON into a compact
  SteadyState operations console with native rollout, SLO, log, trace, policy,
  diagnosis, and backup views. The hotfix PR and final workflow evidence are
  recorded after hosted validation.

## 2026-08-13 — Phase 9 hosted acceptance verified

- Verified the complete full-profile portal golden path in [Phase 9 acceptance run 31689255906](https://github.com/saadabdullaah/steadystate/actions/runs/31689255906) against commit `7036477bfdca26b1557bbd2db4228dfbdd7b4de5`.
- Passed real portal navigation, accessibility, telemetry/policy/database views, typed planning, GitHub App broker dispatch, exact three-file PR validation, smoke PR/branch cleanup, curated snapshots, and disposable platform teardown.
- Retained artifact `9177368072` (`phase9-acceptance-7036477bfdca26b1557bbd2db4228dfbdd7b4de5`) with SHA-256 `05fc3a78031a28ce3b92f0fd6df7043cb9f2299c2aba589b60c2de2a2557f4c1`.
- Verified evidence JSON SHA-256 `1a3d392f08c0e69c0176f69fcdff730c2edb5ae296d5a6293512f473b1d3e3bf`, hosted GIF SHA-256 `c58b8df32c97c3c38eb4c3a337467ec9bb179c96212a17ace2f77ed494690c75`, and WebM SHA-256 `98ace3249e81f90e2c63e6df8abaca5cb94aa2435dfdc6e05908c93884e841b2`.
- Verified exact-commit CI run `31689255890`, platformctl smoke run `31689255934`, and parallel Go/JavaScript CodeQL run `31689279749`.
- Committed the real hosted GIF to `docs/demonstrations/phase9-portal-golden-path.gif`; final PR reruns, exact-main regressions, tag, and GA release remain publication gates.

## 2026-08-11 — Phase 9 portal and GA implementation

### Integrated on `phase-9/portal-ga`

- Added the embedded React portal, typed loopback API, single-use launch/session
  exchange, CSRF/Host/Origin enforcement, strict headers, bounded SSE replay,
  deterministic plans, real broker dispatch, and guarded Rollout break glass.
- Added refined overview, catalog, Team, service, Application, Database,
  request, readiness, observability, policy, diagnosis, planning, and audit
  experiences in light/dark responsive layouts with accessibility tests.
- Added `platformctl platform up|status|verify|down`, expanded first-run config,
  portal version/asset provenance, deterministic embedded builds, six-target
  release wiring, parallel Go/JavaScript CodeQL, and Playwright coverage.
- Added a real full-profile Phase 9 workflow that drives the browser, submits
  an unmerged App-authored smoke proposal, records GIF/WebM/evidence, removes
  temporary GitHub resources, captures diagnostics, and safely tears down.
- Added ADR-0012, portal/GA architecture, user and troubleshooting guides,
  evidence/security/budget contracts, and private master-plan version 2.0.

### Publication gates still pending

- PR #95 now contains the integrated implementation and hosted evidence. Exact-main
  regression links, release archives, annotated tag, and the `v1.0.0` release will
  be recorded only after those remaining external gates succeed.

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
- Exact-main CI `29685492924`, CodeQL `29685492928`, and Phase 4 acceptance `29685512926` passed on merge `1ca1564`. Nightly `29685511924` exposed a standalone-operator regression before publication: unconditional optional-CRD watches prevented controller cache startup when Rollouts and monitoring CRDs were absent. The follow-up makes watch registration capability-aware, keeps rolling reconciliation independent of Phase 4 add-ons, and rejects unavailable canary capability before child mutation; the release remains gated on its follow-up PR and a clean exact-main Nightly rerun.

### Release outcome

- Merged the standalone rolling-controller correction in PR #28 at `f9ba51a`, revalidated the release gates, and retained it as the exact `v0.4.0` release commit.
- Published [SteadyState v0.4.0 - Progressive Delivery and Automatic Rollback](https://github.com/saadabdullaah/steadystate/releases/tag/v0.4.0) on 2026-07-19.

## 2026-07-19 - Phase 5 observability

### Kubernetes 1.35 compatibility gate

- Rebased every kind profile and kubectl expectation on Kubernetes `1.35.5` using `kindest/node:v1.35.5@sha256:ce977ae6d65918d0b58a5f8b5e940429c2ce42fa3a5619ec2bbc60b949c0ac95`.
- Converted kubeadm patches to `v1beta3` map-form kubelet arguments while retaining Go `1.25.12`, kind `0.32.0`, and the existing Kubernetes Go modules.
- Recorded the compatibility boundary in ADR-0008. Local commit `764fcaa` is the Phase 5 rebaseline checkpoint; hosted Phase 0-4 regression run identifiers will be recorded after the consolidated PR starts Actions.

### GitOps observability foundation

- Added checksum-pinned Loki `18.5.1`/`3.7.3`, Tempo `1.24.4`/`2.9.0`, OTel Collector `0.165.0`/`0.156.0`, and Alloy `1.10.1`/`1.17.1` Argo children at waves `-16` through `-13`.
- Extended the existing Prometheus/Grafana plane rather than installing duplicates. Added explicit correlated datasources, a loopback-only Grafana HTTPRoute, 24-hour capped Loki/Tempo storage, label-filtered Alloy discovery, a bounded OTLP pipeline, project restrictions, checksum verification, and deterministic render tests.

### Telemetry, service health, SLOs, and dashboards

- Activated `observability.logs` and `observability.traces`; added deterministic Pod labels, OTLP environment, a collector-only egress NetworkPolicy, rolling metrics resources, opt-out deletion, and watch-derived `ServiceHealth`.
- Added structured JSON access logs, secure request IDs, W3C Trace Context propagation, OTLP export, normalized routes, health/metrics exclusions, and redaction tests to the demo. Declared `VERSION=v0.5.0` while retaining the released `v0.4.0` GitOps image until the post-merge delivery bot PR.
- Added request/error/availability/P95/burn recording rules, `14.4` fast and `6` slow multi-window alerts, fail-safe empty-vector behavior, and deterministic Application/platform Grafana dashboards.

### Consolidated hosted closeout

- Added the separate 60-minute Phase 5 workflow, real VHS tape, schema-versioned evidence, correlated Prometheus/Loki/Tempo queries, opt-out proof, ten-percent-error burn alert proof, Grafana/Alertmanager visibility, memory budgets, progressive-delivery regression, success/failure logs, rendered state, diagnostics, and bounded cleanup.
- Opened consolidated draft PR [#29](https://github.com/saadabdullaah/steadystate/pull/29), `feat: complete Phase 5 observability`, from `phase-5/observability`. At head `8ed367f`, [CI run 29843478654](https://github.com/saadabdullaah/steadystate/actions/runs/29843478654) passed quality, Windows, envtest, security, and kind-smoke; [Phase 4 run 29843478854](https://github.com/saadabdullaah/steadystate/actions/runs/29843478854) passed the progressive-delivery regression.
- [Phase 5 acceptance run 29843478650](https://github.com/saadabdullaah/steadystate/actions/runs/29843478650) passed all eight checks against PR merge revision `1ba5bfa17c2116388efeb0b88b9db2e75076185b`. The evidence correlates request `phase5-29843478650-1` and trace `11111111111111111111111111111111`, measures a deterministic 10% error rate, records 844,398,592 observability bytes and 5,272,350,720 total bytes, and confirms every opt-out, alert, datasource, and progressive regression assertion.
- [Artifact 8500873862](https://github.com/saadabdullaah/steadystate/actions/runs/29843478650/artifacts/8500873862), `phase5-acceptance-1ba5bfa17c2116388efeb0b88b9db2e75076185b`, is 1,852,250 compressed bytes with GitHub SHA-256 `f67b3e7090ee00ed60853dcfdbdffdc209a55c4bd7d56a4b7f56119852827cd9`. Its committed 197,514-byte GIF has SHA-256 `3791b01210e9842a0a32ae6ce6170a78a31fa8c8134fc61919a1c0398d0665d7`.
- Exact-main gates, the generated demo delivery PR, immutable `v0.5.0`/`v0.5.0-bad` publication, annotated tag, and release remain pending; Phase 5 is not yet published.

### Delivery and release stabilization

- Merged Phase 5 PR #29 at `75502a9`. Demo release run `29858334684` published public `v0.5.0` (`sha256:759de26a141a83f4d13807fefa3b9473f99ad9dbec869c28f315670c307bc76e`) and `v0.5.0-bad` (`sha256:eb0d52641b95794567c0e15ba4ba4a69a26d3686a6a9c61e110f418de9125043`). Generated delivery PR #32 passed its required checks and merged at `f4ea119`.
- Exact-main CI run `29863510072`, CodeQL run `29863510061`, Phase 4 acceptance run `29863934178`, and manual Nightly run `29863941431` passed. Phase 5 acceptance run `29863937918` passed every functional assertion but rejected a single post-load 989,569,024-byte observability sample above the unchanged 900 MiB limit.
- Scheduled Nightly run `29871039071` exposed a Gateway recovery edge: Deployment and Service self-healed, but the unchanged HTTPRoute retained Envoy Gateway's stale `BackendNotFound` status. The release-stabilization correction fingerprints backend Service UIDs on the route, producing one deterministic re-evaluation update after recreation and no steady-state writes.
- Phase 5 memory acceptance retains the 900 MiB and 6.5 GiB limits and now requires three consecutive 15-second in-budget samples within five minutes after the burn test. It retains the full sample series and a per-container breakdown so transient load and stabilized working set remain auditable. Publication remains blocked until these corrections pass all branch and exact-main release gates.
- PR #34 passed CI `29877128248`, CodeQL `29877152123`, Nightly `29877149981`, Phase 4 `29877128181`, and Phase 5 `29877128187`, then merged at `10a9706`. Its Phase 5 artifact `8513712319` contains 200 files and 174 diagnostics, with GitHub SHA-256 `02f1235aa6e243af3aca970565b8fc28dd9cf42d1076f9d4895ee5b84d653d0b`; the stabilized measurements were 861,413,376 observability bytes and 5,337,673,728 total bytes.
- Exact-main Phase 5 runs `29895953721` and `29897961290` were cancelled by the 60-minute job timeout inside the VHS recording step. Because only the job had a timeout, cancellation also skipped failure evidence, artifact upload, and cleanup. The acceptance runtime correction bounds the normally 3.5-minute functional recording at 14 minutes, reduces the job envelope to 45 minutes, bounds setup stages, records the active stage, and preserves time for failure evidence and cleanup instead of increasing the timeout.
- Exact-main CI `29906768275`, CodeQL `29906768584`, and Nightly `29906790664` passed at `0d851ee`. Phase 4 run `29906793346` reached its 75-minute ceiling after the background outage probe generated roughly 2.7 million requests in 18 minutes and saturated the single-node control plane; its retained artifact `8526269832` has GitHub SHA-256 `077cb9db61fdc59a20323f78f5b4249e8a9d9f7515f3ca7f352782b795ee2ca9`. The correction rate-limits only that continuity probe to about ten requests per second while preserving every explicit 500-request weight measurement and every promotion/rollback threshold. Phase 4 now records named stages and uses nested recording bounds inside a 60-minute job envelope.
- Phase 5 run `29906796438` failed fast during the observability-foundation gate because Grafana remained unready while downloading and initializing unused suggested plugins under a 200m CPU limit. Artifact `8524578367` has GitHub SHA-256 `2a213171e30ce8c66624e82429e466ace9c1da22d30c66e49b9b4cc9c10cc16f`. Grafana now disables default plugin preinstallation, receives a 500m CPU startup ceiling without changing its memory budget, and contributes its own log to acceptance evidence. With the functional test already bounded, the Phase 5 job envelope is reduced from 45 to 40 minutes.
- Dependabot alert `1` identifies a high-severity gRPC-Go xDS RBAC/HTTP/2 advisory in `google.golang.org/grpc v1.81.1`, fixed by `v1.82.1`. Because the demo links gRPC through its OTLP exporter, the exact update from PR #36 is incorporated into release stabilization and declares immutable demo patch `v0.5.1`; `v0.5.0` remains the already-published baseline until the main-branch release workflow publishes the fixed good/bad images and opens its single-file GitOps PR.
- PR #37 passed all seven checks, including Phase 4 run `29925221835` and Phase 5 run `29925221774`, then merged at `2847722`. Demo release run `29927603989` published `v0.5.1` and generated single-file delivery PR #38; all seven of its checks passed and it merged at exact main `4d964e1`. Exact-main Phase 5 run `29936633052` completed all eight functional checks in roughly 4.3 minutes but VHS then spent over nine minutes rendering the default high-frame-count GIF and hit the eight-minute process termination path with exit 137. Failure artifact `8537105171` retained the eight passed checks, zero-byte incomplete GIF, diagnostics, and schema-versioned failed evidence with GitHub SHA-256 `843882147f415e4def76972cb5bfe5585d7714caaec82a74671eac2336b00920`. The tape now matches the established Phase 3/4 recording contract at 2 FPS and 8x playback, with bounded 7/8/9-minute screen/process/step limits; no functional assertion changes.
- PR #40 Phase 5 run `29939861649` passed the corrected functional proof and GIF rendering in 15m36s. Its Phase 4 run `29939861587` completed promotion but the rejection commit reached the root before the health-gated platform revision reached the tenant Application; the five-minute Git-detection wait expired before any bad-candidate traffic or rollback timer began. Artifact `8538589251` retained failure evidence and diagnostics with GitHub SHA-256 `2e939a5096b2b5acdcf290ac3ba96d8d3473da3744c8426cba3a4f3a3292ea93`. Promotion, rejection, and recovery now use the same 15-minute Git propagation observation bound as final migration, while the frozen five-minute workload migration, 12-minute promotion, and 180-second post-10% abort thresholds remain unchanged.

### Release outcome

- Exact-main CI `29952193455`, CodeQL `29952193476`, Nightly `29952221895`, Phase 4 `29952225492`, and Phase 5 `29952228833` passed against `d258e169974a8a8ce1bc67e97eb966fe7c5113ed`.
- Published annotated tag and GitHub release `v0.5.0` from that exact validated commit.

## 2026-07-23 - Phase 6 supply-chain security

### Consolidated implementation

- Established checksum-pinned Kyverno `1.18.2` at Argo waves `-12/-11`, using only stable CEL `ValidatingPolicy` and `ImageValidatingPolicy` APIs.
- Switched the validated universal safety, strict Application, signature, and SPDX-attestation policies to fail-closed enforcement. Added deterministic Application security labels, same-Team non-isolated networking, admission-failure status, PolicyReport/workload watches, and digest provenance for Kyverno-mutated image references.
- Extended the demo release with CRITICAL vulnerability gates, distinct SPDX JSON SBOMs, GitHub OIDC signatures and attestations, exact identity/issuer verification, immutable rerun behavior, and one-file GitOps security activation.
- Added SOPS `3.13.2`/age `1.3.1` custody, an encrypted Grafana administrator Secret, short-lived decryption, mirrored commands, gosec/govulncheck, Kyverno CLI fixtures, a deliberate vulnerable fixture, CNPG-label bypass regression, ADR-0009, and the STRIDE-lite threat model.
- Added the bounded Phase 6 hosted workflow, real VHS tape, schema-versioned evidence, wrong-workflow identity proof, signed/digest-pinned admission, truthful rejection status, active-tuple preservation, resource budgets, redacted diagnostics, and exact fixture cleanup.
- Consolidated implementation PR #41 merged as `fd65dc4928062d3c4019dab86c7aadbac248f998`. Release-gate correction PR #43 merged as `03db5852b43d008990f7e5a7ff587f674d20e969`, and final acceptance-stability PR #45 merged as exact release commit `381e7dbd4c718609a35c70a38d08ac48485eece9`.
- Demo release run `30117183630` published signed and attested `v0.6.0` (`sha256:0e5d164992424a98a4d70dd12ef322536d081705d4f886bd276f3b0dec681717`) and `v0.6.0-bad` (`sha256:41b275717e1c02963cebdde1b9e885e2e16cc123021a84c924dd75380436aa21`). Generated single-file GitOps PR #44 merged as `acdbcf76f3f623b27c7396c541e43e23ee92df4a`.

### Acceptance and publication

- Exact-main CI `30182652568`, CodeQL `30182652570`, Nightly Integration `30182668936`, Phase 4 acceptance `30190797246`, Phase 5 acceptance `30190914963`, and Phase 6 acceptance `30192768251` passed against `381e7dbd4c718609a35c70a38d08ac48485eece9`.
- Phase 6 artifact `8629239017`, `phase6-acceptance-381e7dbd4c718609a35c70a38d08ac48485eece9`, is 1,666,282 compressed bytes with GitHub SHA-256 `f954c9934965fc082940761d28a6555306d7fbb6c834d1a6261a1a7be7f74597`.
- All seven security checks passed. The admitted workload was rewritten to the verified immutable digest, unsigned and wrong-identity images were denied, unsafe Pod fixtures and CNPG-label spoofing were denied, `SecurityPolicyReady` preserved the healthy tuple, and SOPS decryption left no tracked plaintext.
- Evidence retained 178 diagnostic files. Kyverno used 220,540,928 bytes and the standard profile used 6,059,945,984 bytes, within the unchanged 500 MiB and 7 GiB budgets. Evidence JSON SHA-256 is `b98d06b3035af17e3e4bc3a589cac06c7c5bf7c852b539e2ad1688cbba199fec`; the 31,604-byte GIF SHA-256 is `1d6fc52a43ccf3815e3042bb06e28aebc987566c0f0a8fb427f71a431013b246`.
- Published annotated tag and GitHub release [`v0.6.0`](https://github.com/saadabdullaah/steadystate/releases/tag/v0.6.0) from the exact validated commit with the GIF, evidence JSON, and both SPDX SBOMs attached. No remote Phase 6 branch or PR remains; Dependabot PR #42 stays separate.

## 2026-07-26 - Phase 7 data and recovery release candidate

- Added exact full-profile pins for local-path-provisioner, cert-manager,
  CloudNativePG, Barman Cloud Plugin, PostgreSQL, and SeaweedFS, including
  chart/manifest/CRD integrity and a mandatory hosted compatibility gate.
- Added SOPS-encrypted backup credentials and an exact-name, loopback-only
  SeaweedFS lifecycle whose named volume is preserved unless purge is
  explicit.
- Added the `Database` API, deterministic CNPG/Barman/monitoring/network
  resources, UID-derived archives, Argo health/waves/project boundaries,
  controller watches/status/drift repair, backup-first finalization, ordered
  Team deletion, and the emergency force-delete contract.
- Activated `Application.spec.databaseRef`, `DatabaseReady`, explicit
  SecretKeyRefs, database NetworkPolicies, and the demo orders API with
  database-aware readiness and value-free spans.
- Added the full disaster-recovery workflow: signed image, 100 acknowledged
  orders, WAL/base backup, complete kind destruction, Git-only recovery,
  exact checksum, RTO/RPO, backup alert/recovery, final backup, retained
  objects, budgets, redacted diagnostics, and VHS evidence.
- Initial foundation commit is `7d5b6b7`. PR, hosted artifact, signed image,
  generated delivery PR, exact-main gates, tag, and release identifiers will
  be recorded after validation. Phase 7 is not yet published.

### Draft validation

- Opened consolidated draft PR [#47](https://github.com/saadabdullaah/steadystate/pull/47),
  `feat: complete Phase 7 data and recovery`, from
  `phase-7/storage-foundation`. The first head combined foundation commit
  `7d5b6b7` and implementation commit `afdfb4d`.
- Initial CI run
  [30205678023](https://github.com/saadabdullaah/steadystate/actions/runs/30205678023)
  passed quality, Windows, envtest, and kind-smoke. Security correctly failed
  on two high-entropy test SHA fixtures; the unpublished implementation
  commit is being rewritten with deterministic low-entropy fixtures rather
  than adding a scanner exception.
- Phase 4 run
  [30205678008](https://github.com/saadabdullaah/steadystate/actions/runs/30205678008)
  and Phase 5 run
  [30205678009](https://github.com/saadabdullaah/steadystate/actions/runs/30205678009)
  retained diagnostics showing that the operator Argo child could not render:
  its JSON patch appended to an absent container `env` list. The correction
  creates that list atomically and adds a render-contract regression test.
- Phase 7 foundation run
  [30205678027](https://github.com/saadabdullaah/steadystate/actions/runs/30205678027)
  proved the exact SeaweedFS image started and wrote its named volume, but the
  health check incorrectly probed the authenticated S3 root. It now uses the
  image's internal master `/cluster/status` readiness endpoint while keeping
  the S3 port loopback-only. Phase 6 compatibility run
  [30205678031](https://github.com/saadabdullaah/steadystate/actions/runs/30205678031)
  passed unchanged.
- Corrected run, artifact, and final commit identifiers remain pending; this
  section will be completed rather than replaced after hosted validation.
- Corrected head `70dbb3b` passed quality, Windows, envtest, kind-smoke, and
  Phase 6 compatibility in runs
  [30208021265](https://github.com/saadabdullaah/steadystate/actions/runs/30208021265)
  and
  [30208021240](https://github.com/saadabdullaah/steadystate/actions/runs/30208021240).
  The operator render and SeaweedFS readiness corrections both worked.
- Phase 4 run
  [30208021246](https://github.com/saadabdullaah/steadystate/actions/runs/30208021246)
  and Phase 5 run
  [30208021340](https://github.com/saadabdullaah/steadystate/actions/runs/30208021340)
  then exposed a profile-boundary error: standard-profile tenant rendering
  included the Phase 7 Database and `databaseRef`, correctly degrading without
  the full data stack. Standard rendering now contains only Team/Application
  intent and removes the binding; the full profile retains all three sources.
- Phase 7 foundation run
  [30208021244](https://github.com/saadabdullaah/steadystate/actions/runs/30208021244)
  reached the healthy external store and full GitOps graph, then cert-manager's
  default `kube-system` leader-election resources were rejected by the
  restricted platform project. Exact chart values now keep leader election in
  `cert-manager`; duplicate local-path Namespace ownership is also removed.
- CI security retained report artifact `8633670383`. Trivy identified the
  Database controller's required Secret-copy permission and two findings in
  the byte-exact upstream local-path manifest. Time-bounded, path-specific
  exceptions document those required trust boundaries without weakening
  application policy or changing the checksum-pinned manifest.
- Corrected head `f28b59a` passed Windows, envtest, kind-smoke, Phase 5
  acceptance, and Phase 6 compatibility in runs
  [30213150530](https://github.com/saadabdullaah/steadystate/actions/runs/30213150530),
  [30213150482](https://github.com/saadabdullaah/steadystate/actions/runs/30213150482),
  and
  [30213150474](https://github.com/saadabdullaah/steadystate/actions/runs/30213150474).
- Quality failed before repository checks because the Kubernetes checksum
  download received a connection reset. All PowerShell checksum and binary
  downloads now share the existing bounded five-attempt retry contract.
- Security artifact `8635080653` showed Trivy, Gosec, Govulncheck, Gitleaks,
  and Kyverno gates passing. Checkov's 19 findings were all against the
  byte-exact upstream local-path manifest. Checkov now excludes only that
  immutable source fixture; checksum, Kustomize rendering, namespace
  ownership, and platform-boundary tests remain blocking.
- Phase 4 artifact `8635247414` from run
  [30213150538](https://github.com/saadabdullaah/steadystate/actions/runs/30213150538)
  proved every platform child Healthy but captured a deterministic Kustomize
  comparison error: the standard-profile remove patch targeted a
  `databaseRef` already removed by the acceptance branch. The reusable demo
  leaf now omits the binding, and only the full profile adds it.
- Phase 7 artifact `8635239188` from run
  [30213150486](https://github.com/saadabdullaah/steadystate/actions/runs/30213150486)
  proved bootstrap, the exact backup store, branch images, and full GitOps
  deployment. The compatibility script then queried the later-wave
  `cloudnative-pg` Application before Argo created it. Its bounded wait now
  tolerates absence while waves advance and requires eventual Synced/Healthy
  state rather than failing immediately.
- Corrected head `cb10021` passed all five CI jobs, Phase 4 acceptance, Phase 5
  acceptance, and Phase 6 compatibility in runs
  [30214986778](https://github.com/saadabdullaah/steadystate/actions/runs/30214986778),
  [30214986783](https://github.com/saadabdullaah/steadystate/actions/runs/30214986783),
  [30214986781](https://github.com/saadabdullaah/steadystate/actions/runs/30214986781),
  and
  [30214986732](https://github.com/saadabdullaah/steadystate/actions/runs/30214986732).
- Phase 7 run
  [30214986776](https://github.com/saadabdullaah/steadystate/actions/runs/30214986776)
  and artifact `8635863984` showed SeaweedFS remained healthy while every
  Kubernetes diagnostic failed with API-server EOF/reset errors before the
  Database proof began. This was full-graph runner pressure, not a backup or
  restore assertion. The focused foundation job now omits the separately
  validated Loki/Tempo/Alloy/OTel pipeline while retaining Prometheus,
  Kyverno, CNPG, Barman, PostgreSQL, and SeaweedFS. Failure evidence now also
  records exact kind container state/OOM flags, resource use, and Docker disk
  consumption.
- Corrected head `d02f8b1` passed all five CI jobs, Phase 4 acceptance,
  Phase 5 acceptance, and Phase 6 compatibility. Phase 7 run
  [30217802874](https://github.com/saadabdullaah/steadystate/actions/runs/30217802874)
  and artifact `8636565821` proved that the focused full-profile graph kept
  every kind node running without OOM, installed the complete data stack, and
  reached the real Database reconciliation path.
- That proof exposed a controller defect rather than an infrastructure
  failure: create-on-not-found reconciliation initialized external resources
  without the desired name and namespace, producing
  `resource name may not be empty`. The correction preserves external-child
  identity, keeps unstructured metadata JSON-compatible, and adds a direct
  absent-child regression test. A corrected compatibility artifact remains
  required before Phase 7 can close.
- Head `3b69183` passed all five CI jobs, Phase 5 acceptance, and Phase 6
  compatibility. Phase 7 run
  [30220026714](https://github.com/saadabdullaah/steadystate/actions/runs/30220026714)
  and artifact `8637197321` proved the identity correction and reached CNPG
  admission. CNPG then correctly rejected the digest-only PostgreSQL operand
  because it could not detect image upgrades. The frozen tag and digest are
  now combined as `18.4-system-trixie@sha256:...` everywhere: generated
  Cluster, version contract, Kyverno allowlist, acceptance assertion, and
  tests.
- Phase 4 run
  [30220026723](https://github.com/saadabdullaah/steadystate/actions/runs/30220026723)
  and artifact `8637268709` passed the complete product proof through automatic
  promotion, exact 10/25/50/100 traffic measurements, provenance, and
  zero-outage assertions. Only VHS missed the two-second colored completion
  marker and reached its tape timeout. Both Phase 4 tapes now wait on a plain
  last-line marker that remains visible for ten seconds using syntax supported
  by the pinned VHS `0.11.0`.
- Head `20c49e2` passed all five CI jobs, Phase 5 acceptance, and Phase 6
  compatibility. Phase 4 run
  [30231244539](https://github.com/saadabdullaah/steadystate/actions/runs/30231244539)
  again passed the complete promotion proof; its artifact `8640504482`
  confirmed the child marker was present only in redirected output while the
  VHS terminal remained at a prompt. The tapes now emit the authoritative
  pass/fail marker from their own shell after the wrapper exits.
- Phase 7 run
  [30231244531](https://github.com/saadabdullaah/steadystate/actions/runs/30231244531)
  and artifact `8640491291` proved the tagged immutable PostgreSQL operand was
  admitted and CNPG initialized both real Cluster resources. Kyverno then
  denied their Job-owned initdb Pods because the Phase 6 image exception only
  recognized direct Cluster ownership. The corrected boundary admits only the
  exact CNPG `1.30.0` bootstrap/recovery roles, labels, Cluster ServiceAccount,
  owner naming, and frozen PostgreSQL, bootstrap-controller, and Barman
  images; universal Team safety and the forged-label regression remain
  enforced.
- Head `c97e7a5` passed all five CI jobs, Phase 5 acceptance, and Phase 6
  compatibility. Phase 7 run
  [30256491321](https://github.com/saadabdullaah/steadystate/actions/runs/30256491321)
  and artifact `8649843581` showed CNPG accepted the exact identity and
  bootstrapped both Clusters, but Kyverno's ImageValidatingPolicy required its
  internal verification-outcomes annotation before applying the non-demo
  exception. CNPG and Barman chart values now inject exact tag-plus-digest
  images; their Pods bypass the demo-image mutator/verifier and are instead
  admitted only by the exact CNPG clause in universal Team safety.
- Phase 4 run
  [30256491215](https://github.com/saadabdullaah/steadystate/actions/runs/30256491215)
  reached only the VHS wait timeout. The tapes now run the real acceptance
  stage directly under a bounded Unix timeout and emit their result from the
  controlling shell, eliminating the nested redirected-process boundary.
- Head `b34e0bf` passed all five CI jobs, Phase 5 acceptance, and Phase 6
  compatibility. Phase 4 run
  [30261883299](https://github.com/saadabdullaah/steadystate/actions/runs/30261883299)
  proved the complete promotion path in 8.5 minutes, including exact
  10/25/50/100 traffic shares and provenance. Pinned VHS still ignored the
  terminal marker, so recordings now use bounded empirical sleeps while the
  immediately following state gate remains authoritative.
- Phase 7 run
  [30261883285](https://github.com/saadabdullaah/steadystate/actions/runs/30261883285)
  and artifact `8651903691` proved admission, immutable image pulls, and CNPG
  initdb execution. The init Pods then repeatedly errored because their egress
  policy permitted only SeaweedFS and blocked the Kubernetes API required by
  CNPG's in-Pod manager. The corrected policy adds only API-service,
  kube-apiserver, and same-Cluster PostgreSQL/manager paths; failure evidence
  now retains every CNPG job Pod object and container log.
- Head `d9835c6` again passed all five CI jobs, Phase 5 acceptance, and Phase 6
  compatibility. Phase 7 run
  [30273988013](https://github.com/saadabdullaah/steadystate/actions/runs/30273988013)
  and artifact `8657101498` supplied the definitive initdb log:
  `dial tcp 10.96.0.1:443: i/o timeout`. Calico evaluates the kind Service
  path after DNAT, where the endpoint is a private node address on 6443 and
  cannot be selected as a namespaced kube-apiserver Pod. The policy now
  permits only RFC1918 destinations on 6443 in addition to the exact Service
  IP, and failure capture applies ten-second API request bounds.
- Phase 4 run
  [30273987989](https://github.com/saadabdullaah/steadystate/actions/runs/30273987989)
  reached GIF encoding after the fixed 13-minute recording; artifact
  `8656731427` retained its failure evidence. The process was killed by its
  14-minute outer bound. The tape now clears to an explicit result marker and
  uses VHS whole-screen matching, which ends recording when the real command
  exits while retaining independent inner, VHS, and job timeouts.
- Head `c18a9ce` passed all five CI jobs, Phase 5 acceptance, and Phase 6
  compatibility. Phase 4 run
  [30285247205](https://github.com/saadabdullaah/steadystate/actions/runs/30285247205)
  and artifact `8660789470` proved the whole-screen wait matched the literal
  terminal markers while VHS was still typing its command, before the
  acceptance stage ran. The tapes now construct one result marker from a
  shell variable only after the bounded stage exits, and a render-contract
  test rejects terminal marker literals in every `Type` line.
- Phase 7 run
  [30285247223](https://github.com/saadabdullaah/steadystate/actions/runs/30285247223)
  and artifact `8661331384` proved the corrected API egress path: the GitOps
  `orders` Cluster completed initdb, became Ready, archived WAL, and completed
  its scheduled backup. The temporary `compat` initdb Pod alone was rejected
  because the 2 GiB Team quota had only 720 MiB of limit headroom remaining
  and CNPG requested 1 GiB. The sample ceiling is now 4 GiB, with a
  deterministic rendered-value regression; this is quota capacity, not
  allocated memory.
- Head `114722f` passed all five CI jobs plus Phase 4, Phase 5, and Phase 6.
  Phase 7 run
  [30288745403](https://github.com/saadabdullaah/steadystate/actions/runs/30288745403)
  and artifact `8662750840` proved both CNPG initdb jobs completed, both
  PostgreSQL Pods were Running, and both archives recorded successful backups.
  CloudNativePG nevertheless kept each Cluster in `Instance Status Extraction
  Error` because Team default-deny blocked the operator's HTTPS status request
  to the instance-manager Pod on TCP 8000. Each Database now admits that port
  only from the `cloudnative-pg` Pod in `cnpg-system`, with a least-privilege
  builder regression.
- Head `ef811de` again passed all five CI jobs plus Phase 4, Phase 5, and
  Phase 6. Phase 7 run
  [30299878348](https://github.com/saadabdullaah/steadystate/actions/runs/30299878348)
  and artifact `8666864746` confirmed the operator NetworkPolicy: both
  Databases became `Healthy`, both CNPG Clusters reported Ready, and both
  archives had successful backups. The foundation verifier still timed out
  because the controller emitted Application-only `DatabaseReady=True`
  instead of the Database API's declared `Ready=True`. Database status now
  uses the public Ready condition, removes the obsolete pre-release condition,
  and Application binding reads that same current-generation contract.
- Head `1302d47` again passed all five CI jobs plus Phase 4, Phase 5, and
  Phase 6. Phase 7 run
  [30308130782](https://github.com/saadabdullaah/steadystate/actions/runs/30308130782)
  advanced through Healthy Database status and failed immediately at the
  first real SQL command: Unix-socket peer authentication correctly rejected
  database role `app` because the CNPG container runs as operating-system user
  `postgres`. Compatibility automation now authenticates locally as
  `postgres`, executes workload statements under `SET ROLE app`, and keeps the
  privileged WAL switch in a separate administrative call without exposing a
  password. The Phase 7 disaster-recovery workflow uses the same explicit
  local administrative boundary.
- Head `ad0d146` passed all five CI jobs and Phase 4. Phase 7 run
  [30316337859](https://github.com/saadabdullaah/steadystate/actions/runs/30316337859)
  and artifact `8672817016` proved the SQL role boundary, both successful
  archives, and the complete compatibility path through Database deletion.
  Finalization then exposed that the ownerless final Backup initialized its
  GVK but not its name/namespace before `CreateOrUpdate`, producing
  `resource name may not be empty`. Final Backup reconciliation now seeds the
  complete desired identity and has a dedicated absent-child/no-owner
  regression.
- Phase 5 run
  [30316337779](https://github.com/saadabdullaah/steadystate/actions/runs/30316337779)
  reached its real recording but one Grafana datasource health request
  exhausted a single 15-second HTTP timeout. Grafana route, datasource, and
  alert requests now use a bounded 60-second retry envelope while retaining
  15-second request bounds. Phase 6 run
  [30316337803](https://github.com/saadabdullaah/steadystate/actions/runs/30316337803)
  failed before cluster work when GitHub's Loki chart asset connection reset.
  Checksum-verified manifest/chart downloads now retry three times, quarantine
  any corrupt cached chart, and still fail closed on integrity mismatch.
- Head `635729e` passed all five CI jobs plus Phase 4, Phase 5, and Phase 6.
  Phase 7 run
  [30354614185](https://github.com/saadabdullaah/steadystate/actions/runs/30354614185)
  and artifact `8686673631` proved final Backup creation, completion, and
  finalizer release. Its next assertion incorrectly searched SeaweedFS's
  physical LevelDB/volume files for Barman S3 key names. Evidence now obtains
  a recursive logical object inventory from mini's internal filer API, scopes
  base-backup and WAL assertions to the Database lifetime's UID-derived server
  name, and keeps credentials out of output.
- Head `247c464` again passed all five CI jobs plus Phase 4, Phase 5, and
  Phase 6. Phase 7 run
  [30357626649](https://github.com/saadabdullaah/steadystate/actions/runs/30357626649)
  produced passed evidence before disposable cleanup: 100 canonical rows,
  12 source-lifetime objects including 8 WAL objects, distinct source/recovery
  server names, and identical source/restored checksum
  `1fd183f94ddcb9114f1282249bba2299479d16499e4113abb0168292f19c6070`.
  The job failed only because PowerShell cannot add a new dotted property to
  an existing annotations `PSCustomObject` through assignment. Cleanup now
  uses explicit `kubectl annotate --overwrite`, leaving the successful path
  and evidence contract unchanged.

### Hosted full-profile resource-pressure correction

- Head `5494807` passed all five CI jobs plus Phase 4, Phase 5, and Phase 6.
  Phase 7 acceptance run
  [30854678892](https://github.com/saadabdullaah/steadystate/actions/runs/30854678892)
  failed before data assertions when the live VHS/Chromium process competed
  with the full profile: etcd and the API server missed probes, many add-ons
  restarted, Kubernetes requests returned `EOF`, and `local-path-storage`
  could no longer report Argo Healthy/Synced. Artifact `8872925925` has GitHub
  SHA-256 `7231303e2dd99e44974073c8d883047c00a363d54cc7b4efda730008e823ea85`
  and retains the failed stage, real GIF, component logs, and partial
  diagnostics.
- Foundation run
  [30854669462](https://github.com/saadabdullaah/steadystate/actions/runs/30854669462)
  restored PostgreSQL, reached `Database Healthy`, completed base/final
  backups, and retained WAL objects, but its 25-minute outer step expired
  during the final checksum/snapshot boundary while the API was recovering.
  Artifact `8872932550` has GitHub SHA-256
  `0373ed20a827656abd0c7393e39379960f3bd66c7fd62afe2a0cc8daecda6b63`.
- The correction runs the real drill without Chromium and renders its
  timestamped transcript afterward. The focused foundation gate disables the
  telemetry pipeline, tenant workloads, and kube-state-metrics while retaining
  the Grafana route, Prometheus, Alertmanager, and Kyverno; bounded Kubernetes
  request/retry envelopes protect the two backup lifetimes and restore.
- Phase 5 run
  [30885150066](https://github.com/saadabdullaah/steadystate/actions/runs/30885150066)
  passed all six functional checks through the fast-burn proof, then VHS
  terminated the still-running five-minute memory-stabilization stage at its
  seven-minute screen limit. Artifact `8883275157` retained the correlated
  Prometheus/Loki/Tempo evidence, alert proof, state, and diagnostics. Phase 5
  now follows the same isolation contract as Phase 7: the real test runs
  directly, writes a timestamped transcript, and VHS records only after every
  functional and resource assertion passes.
- At head `540b978`, CI, Phase 4, and Phase 6 passed. Phase 7 foundation run
  [30885149877](https://github.com/saadabdullaah/steadystate/actions/runs/30885149877)
  exposed a configuration error in the new lean gate: disabling Grafana left
  its managed HTTPRoute without a backend, so `monitoring` became Degraded and
  the parent sync never created `data-namespaces`; artifact `8883203415`
  retained Argo state, exact kind container state/resources, controller logs,
  and diagnostics. The gate now disables only kube-state-metrics and keeps the
  Grafana route healthy. Phase 7 run
  [30885160497](https://github.com/saadabdullaah/steadystate/actions/runs/30885160497)
  stopped at the initial `local-path-storage` wave after the parent exhausted
  its short bootstrap retry window; artifact `8883603914` retained the named
  stage and original error. The root Application now uses an explicit bounded
  retry policy, and failure capture tolerates an empty pre-backup inventory so
  diagnostics cannot replace the primary failure.
- At head `d12e8ad`, CI, Phase 4 acceptance, Phase 5 acceptance, Phase 6
  foundation, and Phase 7 foundation all passed. Phase 7 acceptance run
  [30893474923](https://github.com/saadabdullaah/steadystate/actions/runs/30893474923)
  failed before the recovery drill because the full combined stack
  destabilized the Kubernetes API while Kyverno was initializing. The
  retained snapshot showed `steadystate-root` waiting for `kyverno`, no data
  Applications yet, API/controller/scheduler and several add-on restarts, all
  three kind containers alive with `oomKilled=false`, and about 6.5 GiB across
  the nodes—below the 8 GiB contract. Artifact `8886715016` has GitHub digest
  `sha256:b11d9eb37478362ce18b6b14987878c11530a15cf1e91bc767442dc9e9886e8d`.
  The correction starts fail-closed security before telemetry, bounds Argo
  processors, adds reconciliation jitter, deterministically restarts the
  configured application controller before root sync, and captures
  control-plane/Kyverno current and previous state before slower diagnostics.
- Head `acf366e` passed the complete whole-cluster recovery path in Phase 7
  acceptance run
  [30898749390](https://github.com/saadabdullaah/steadystate/actions/runs/30898749390):
  100 orders restored with the exact checksum, RTO 14.162 minutes, RPO 0,
  backup outage alerting recovered, and the full profile measured 6656.418
  MiB. The deterministic final backup also completed, but the tenant Argo
  Application briefly remained on recovery commit `553bb5a` and recreated the
  deleted Database before receiving retirement commit `f82ae1e`. Artifact
  `8889359549` (GitHub SHA-256
  `ee24b6842f5efbee5899e33f168879b2916e9f494db4a908fcda0e73f4bc9da1`)
  proves the recreated UID and completed `orders-final` event. The acceptance
  runner now waits for every tenant target, compared revision, and successful
  operation revision to equal the retirement commit before requesting
  finalizer-driven deletion; the expected no-prune OutOfSync state is not
  treated as a blocker.
- Phase 7 acceptance run
  [30903568810](https://github.com/saadabdullaah/steadystate/actions/runs/30903568810)
  passed all ten checks on head `6d71076`. It restored the exact 100-order
  checksum after complete kind destruction, measured RTO 11.173 minutes and
  RPO boundary 0 minutes, retained eight WAL and 31 total objects, fired and
  cleared the backup-age alert, completed the final backup/deletion in
  104.959 seconds, and stayed within every resource budget. Artifact
  `8890983887` has GitHub SHA-256
  `815f86dba99c641612c9e9c3ada4a6882bde16ba91b75a60e31f8903100624fa`;
  the committed successful GIF has SHA-256
  `a92d1ba110973bdf72af29456176e7a0d407765f2f1b73312b1d9525a1963898`.

## 2026-08-04 - Phase 7 data and recovery released

- Closeout PR [#51](https://github.com/saadabdullaah/steadystate/pull/51)
  squash-merged as exact release commit
  `47c41d5cabc531afd6bcd46f2108ba9065e7a76a` after all PR checks and
  branch CodeQL passed. Bot delivery PR
  [#50](https://github.com/saadabdullaah/steadystate/pull/50) had already
  deployed signed and SPDX-attested `v0.7.0` good/bad images.
- Exact-main publication gates passed: CI
  [30910634104](https://github.com/saadabdullaah/steadystate/actions/runs/30910634104),
  CodeQL
  [30910634012](https://github.com/saadabdullaah/steadystate/actions/runs/30910634012),
  Nightly
  [30910712941](https://github.com/saadabdullaah/steadystate/actions/runs/30910712941),
  Phase 4
  [30910716390](https://github.com/saadabdullaah/steadystate/actions/runs/30910716390),
  Phase 5
  [30910719654](https://github.com/saadabdullaah/steadystate/actions/runs/30910719654),
  Phase 6
  [30910723943](https://github.com/saadabdullaah/steadystate/actions/runs/30910723943),
  and Phase 7
  [30910727236](https://github.com/saadabdullaah/steadystate/actions/runs/30910727236).
- Exact-main artifact `8894059242` has GitHub SHA-256
  `bbc33e16369684fa38fce3fa1641099546184c7b79b51556da1c0e5ecf72d703`.
  Its ten checks measured 12.58-minute RTO, zero-minute archive boundary,
  ten WAL/33 retained objects, 274.766 MiB data add-ons, 199.6 MiB
  SeaweedFS, and 6151.422 MiB total in-cluster working set.
- Annotated tag `v0.7.0` points to `47c41d5`; the
  [GitHub release](https://github.com/saadabdullaah/steadystate/releases/tag/v0.7.0)
  contains the exact-main GIF, evidence JSON, RTO/RPO report, transcript, and
  SHA-256 manifest. No Phase 7 branch remains. Dependabot PRs #42 and #49 are
  deliberately deferred because their current validation is failing.

## 2026-08-07 - Phase 8 developer golden path closeout candidate

- CLI foundation PR #53 (`e721d9e`) established `platformctl`, stable
  configuration/output/exit contracts, catalog-driven root rendering, and
  cluster/telemetry/data read paths. Broker PRs #54/#55 (`0b066a3`, `86a2690`)
  added the typed four-input workflow, GitHub App trust boundary, protected
  prune, and two-PR lifecycle renderer. Broker smoke run `30996324120` passed
  and its harmless temporary PR/ref were removed.
- Golden-template PR #57 (`e956723`) added Go API, embedded React static, and
  full-stack React/Go/PostgreSQL templates plus signed generic service
  delivery. Operations/release PR #59 (`921b638`) added ordered application
  diagnosis, confirmed break glass, six-target archives, SBOM/checksum
  signature/provenance contracts, installers, reference, man pages, and
  completions. Focused correctness PRs #61/#62/#64 (`cefea9b`, `b094d87`,
  `f3c85ae`) fixed catalog-aware scaffolding, embedded-asset ownership, and
  generated Go/fixture determinism.
- App-authored scaffold PR #65 merged as
  `b6d9e6bb5df2f289e36d8d48c6be83443645b8eb`. It added the `xyz` full-stack
  source, Team, and Database desired state without active Applications. Service
  release recovery run `31114158974` then built, scanned, signed, SPDX-attested,
  and anonymously verified the public `xyz-web-v0.1.0` and `xyz-api-v0.1.0`
  images. Activation PR #67 passed all five checks and merged as `ded9b43`,
  adding only the generated Application/catalog activation state.
- Release recovery PR #66 (`b992c67`) made absent immutable tags an expected
  preflight result and separated historical image source from the current
  activation base. Cleanup PR #68 (`a3cc284`) corrected the ephemeral runner's
  checked-out branch deletion order. GitHub-hosted runner allocation degraded
  during #68; after service recovery, CI run `31159912958` passed all five jobs
  and the PR merged.
- The closeout branch adds the dedicated 90-minute staged Phase 8 acceptance,
  schema-versioned evidence, real transcript/GIF contract, exact-revision CLI
  interactions, App-authored acceptance-only retirement PRs, final-backup and
  retained-archive proof, redacted diagnostics, ADR-0011, and synchronized
  product/runbook/evidence documentation. Final run, artifact ID/checksum,
  exact-main regression links, GIF checksum, tag, and release remain to be
  recorded after hosted acceptance succeeds.
- Closeout CI run
  [31169160234](https://github.com/saadabdullaah/steadystate/actions/runs/31169160234)
  passed all five jobs. Phase 8 acceptance run
  [31169160225](https://github.com/saadabdullaah/steadystate/actions/runs/31169160225)
  passed package verification, exact App-authored PR evidence, full-profile
  bootstrap, image load, and exact-branch GitOps deployment before exposing
  two product integration defects. A same-named `Database` and `Application`
  both requested `xyz-monitor`, while Kyverno `1.18.2` rejected the policy's
  two keyless identities inside one Cosign attestor. Artifact `8990939264`
  retained the CR conditions, exact controller errors, Kyverno verification
  errors, cluster snapshot, and cleanup evidence. Database monitoring now has
  type-qualified names with ownership-safe legacy cleanup, and the two exact
  release workflows are modeled as separate Kyverno attestors.
- The next exact-branch acceptance run
  [31172442924](https://github.com/saadabdullaah/steadystate/actions/runs/31172442924)
  confirmed those corrections: Team, Database, both Applications, Rollouts,
  provenance, backups, and the CLI health gate all passed. Its artifact
  `8991992337` then isolated a 504 in the same-origin order request. The
  generated web server addressed API container port `8080` directly even
  though the Kubernetes Service exposes port `80`. The template and checked-in
  `xyz` fixture now target the Service port, regression coverage rejects the
  invalid target, and acceptance diagnostics capture web and API logs
  separately. Focused PR #70 merged as
  `68cf1ebaa2a38074b812cad11662cd31cf619210` after CI and all Platformctl
  smoke targets passed. Generated service release run
  [31175994689](https://github.com/saadabdullaah/steadystate/actions/runs/31175994689)
  published, scanned, signed, SPDX-attested, and anonymously verified
  `xyz-web-v0.1.1` (`sha256:0e1056f988ce212e4296749f10a3dea03a0392f9e30bd9eb04188b108536858f`)
  and `xyz-api-v0.1.1`
  (`sha256:9a202073551edd3236b318823a2c6471e5fcc335338be0730625a5bab8fc7714`).
  App-authored activation PR #71 changed only the two image tags and catalog
  version, passed all five checks, and merged as
  `df97288f9d0d7d088be5ee02feca35c9900bc62e`.
- Closeout CI run
  [31181163010](https://github.com/saadabdullaah/steadystate/actions/runs/31181163010)
  passed all five jobs. Acceptance run
  [31181164407](https://github.com/saadabdullaah/steadystate/actions/runs/31181164407)
  verified the corrected packages and reached full-profile GitOps, but its
  retained artifact `8995699995` showed the 4-vCPU runner deploying both the
  historical payments data plane and the Phase 8 `xyz` data plane. etcd became
  starved, the API server restarted, and authorization failed closed while its
  storage caches reinitialized. Phase 8 acceptance now keeps every full-profile
  platform component but filters the catalog render to its subject tenant,
  `xyz`, and requires three consecutive API-storage/RBAC stability checks
  before product assertions. Historical tenant coverage remains in Nightly and
  the Phase 7 acceptance suite.
- Closeout CI run
  [31184905034](https://github.com/saadabdullaah/steadystate/actions/runs/31184905034)
  passed all five jobs at `72e6fc8`. Phase 8 acceptance run
  [31184909671](https://github.com/saadabdullaah/steadystate/actions/runs/31184909671)
  confirmed the filtered `xyz` render but artifact `8997945445` showed the
  kind API again returning EOF while the Database provisioned. The hosted
  harness now reserves Docker scheduling priority and soft memory for the
  exact control-plane container before add-ons start, fails early after six
  consecutive API outages, and captures Docker stats/inspect/kind logs before
  bounded raw-API snapshots. This changes no local cluster or product
  contract; it removes host overcommit from the acceptance runner and makes a
  future infrastructure failure diagnosable.
- Closeout CI run
  [31203978644](https://github.com/saadabdullaah/steadystate/actions/runs/31203978644)
  passed all five jobs. Acceptance run
  [31203978827](https://github.com/saadabdullaah/steadystate/actions/runs/31203978827)
  failed fast as designed and artifact `9004380242` retained complete host
  evidence: the live control plane consumed 225% CPU while the workers used a
  combined 305% during unfinished Argo sync waves. The harness now caps the
  three exact kind containers to 3.5 aggregate CPUs, retains control-plane
  scheduling priority, and waits for all fifteen required full-profile child
  Applications plus stable API/RBAC probes before starting the Database and
  golden-path clock. Raw failure snapshots also interpolate their API paths
  correctly.
- Closeout CI run
  [31208867226](https://github.com/saadabdullaah/steadystate/actions/runs/31208867226)
  passed all five jobs. Acceptance run
  [31208866752](https://github.com/saadabdullaah/steadystate/actions/runs/31208866752)
  and artifact `9006433856` verified the resource cap and sequencing fix: all
  platform children settled, the Database and both Applications became
  healthy, and frontend/API/PostgreSQL passed. It then exposed a telemetry
  assertion race because a successful but empty Loki response ended the retry
  immediately. Phase 8 now waits up to four minutes for the actual request ID
  in Loki and then for the correlated trace ID in Tempo; transport success
  alone no longer satisfies either evidence gate.
- Closeout CI run
  [31274745239](https://github.com/saadabdullaah/steadystate/actions/runs/31274745239)
  passed all five jobs. Acceptance run
  [31274745274](https://github.com/saadabdullaah/steadystate/actions/runs/31274745274)
  and artifact `9026965291` proved Loki correlation and retained the requested
  Tempo trace, but exposed an encoding mismatch in the assertion: application
  logs and the Tempo query path use 32-character hexadecimal IDs, while OTLP
  protobuf JSON represents the same 16-byte `traceId` as Base64. Acceptance
  now validates the canonical hex ID, converts it byte-for-byte to Base64, and
  requires that exact value in Tempo evidence.
- Acceptance run
  [31277260107](https://github.com/saadabdullaah/steadystate/actions/runs/31277260107)
  (artifact `9027649253`, GitHub SHA-256
  `5b279d60159852f56dc404c3a2432dd39965e1a78f8c5e30169e0652716b7e95`)
  failed during full-profile deployment before telemetry was exercised. Its
  complete diagnostics exposed the underlying isolation defect: the local
  preflight render received `tenantFilter=xyz`, but the bootstrap root
  Application did not inherit that Helm parameter, so live Argo still created
  both payments and xyz. Under the acceptance-only 1.5-CPU control-plane quota,
  etcd and kube-apiserver then missed liveness deadlines while Argo waited for
  `data-namespaces`. The root now propagates the exact tenant filter, acceptance
  proves the excluded Argo child, Team, and namespace are absent, and baseline,
  retiring, and finalized catalog/renders are mandatory evidence. Hosted
  workers remain capped, while the control plane keeps scheduling priority and
  may use the runner's remaining CPU. Early deployment failures are recorded
  truthfully as `result=failed`, and verified DSSE envelopes are decoded into
  retained SPDX JSON SBOMs rather than serving as a substitute for them.
- At `7022d2a`, CI run
  [31279448745](https://github.com/saadabdullaah/steadystate/actions/runs/31279448745)
  passed all five jobs and Platformctl smoke run
  [31279448762](https://github.com/saadabdullaah/steadystate/actions/runs/31279448762)
  passed all six target/packaging contracts. Phase 8 run
  [31279448756](https://github.com/saadabdullaah/steadystate/actions/runs/31279448756)
  and artifact `9028361989` (GitHub SHA-256
  `02a8988b38a3a3a258ca61139e105aedae6fcada74719b097ff42ce0649ff255`)
  confirmed the tenant/SBOM/failure-state corrections, then exposed scheduler
  imbalance: one hard-capped worker was saturated while its peer used about
  one fifth of a CPU, leaving six controllers unsettled. The harness now uses
  quota-free relative weights: the control plane receives twice each worker's
  share, and a busy worker can borrow idle capacity. Foundation waits retain
  exact per-Application state and fail fast on API loss. Failure diagnostics
  retain node journals/CRI state, and cleanup no longer runs long API teardown
  or backup-network detachment in the wrong order when the API is unavailable.
- At `3aba636`, CI run
  [31282232391](https://github.com/saadabdullaah/steadystate/actions/runs/31282232391)
  passed all five jobs and Platformctl smoke run
  [31282232388](https://github.com/saadabdullaah/steadystate/actions/runs/31282232388)
  passed all six contracts. Phase 8 run
  [31282232398](https://github.com/saadabdullaah/steadystate/actions/runs/31282232398)
  passed the complete live CLI, full-stack, PostgreSQL, canary, provenance,
  telemetry, policy, diagnosis, and resource-budget path. Artifact `9029132915`
  (GitHub SHA-256
  `88c3290a531bd5f9c5ab17a35922b42a46a3529e2cfc9a0dd6d6dff1bb3bdc36`)
  exposed one final lifecycle defect: the retiring render contained the tenant
  Argo cascade finalizer, but the bootstrap root ignored child finalizers, so
  deletion removed the Argo child without pruning its four managed CRs. The
  root now reconciles child finalizers while still ignoring status, and hosted
  acceptance requires the live cascade finalizer before merging finalization.
- At `91fa533`, CI run
  [31316241573](https://github.com/saadabdullaah/steadystate/actions/runs/31316241573)
  passed all five jobs and Platformctl smoke run
  [31316241554](https://github.com/saadabdullaah/steadystate/actions/runs/31316241554)
  passed all six contracts. Phase 8 run
  [31316241574](https://github.com/saadabdullaah/steadystate/actions/runs/31316241574)
  proved the cascade finalizer was live and removed both Application CRs.
  Artifact `9039245988` (GitHub SHA-256
  `7329918f70c5c92eb44bd020953bd30d4c0afa33ea114aa772aaf8fe395a9991`)
  then showed foreground propagation delete the CNPG Cluster seven seconds
  after `xyz-final` began, correctly leaving the Database blocked with
  `BackupHealthy=False`. The tenant child now uses Argo's pinned background
  cascade so Database and Team finalizers retain their dependencies while
  completing the final backup and ordered namespace cleanup.
- At `10a6de2`, CI run
  [31322270318](https://github.com/saadabdullaah/steadystate/actions/runs/31322270318)
  passed all five jobs, Platformctl smoke run
  [31322270333](https://github.com/saadabdullaah/steadystate/actions/runs/31322270333)
  passed all six release contracts, and Phase 8 acceptance run
  [31322270309](https://github.com/saadabdullaah/steadystate/actions/runs/31322270309)
  passed all seven golden-path checks in 25m53s. Artifact `9040790659`
  (GitHub SHA-256
  `0cf90f4e884c06fca495fea76d9d93118b7ecc0c556c36bca429ed9589835935`)
  retained complete evidence and diagnostics. Evidence SHA-256 is
  `19815d2fc16226a8124bde20f599d22d3c3f113e936fb5a590445632246cdfad`;
  the tracked hosted GIF SHA-256 is
  `1107d264753429ef6bfabbbcd9816b9accc54156b3310a7e067ab01ecf06e194`.
  The two reviewed retirement PRs completed in 193.787 seconds, retained 12
  external backup objects, and left no Team, namespace, CR, Argo child,
  generated workload, acceptance PR, or request branch behind.
- Phase 8 closeout PR #69 squash-merged to exact `main` commit
  `fc8e49fc52d55394c8b3730ae9e3c6ebe46a3903`. Exact-main CI run
  [31327852872](https://github.com/saadabdullaah/steadystate/actions/runs/31327852872),
  CodeQL run
  [31327852877](https://github.com/saadabdullaah/steadystate/actions/runs/31327852877),
  Platformctl smoke run
  [31327912602](https://github.com/saadabdullaah/steadystate/actions/runs/31327912602),
  Phase 7 run
  [31327915695](https://github.com/saadabdullaah/steadystate/actions/runs/31327915695),
  and Phase 8 run
  [31327915321](https://github.com/saadabdullaah/steadystate/actions/runs/31327915321)
  passed. The exact-main regression sweep also found two cross-phase defects:
  Nightly run
  [31327915917](https://github.com/saadabdullaah/steadystate/actions/runs/31327915917)
  reproduced an HTTPRoute resource-version race after Service self-healing,
  while Phase 4/5/6 runs
  [31327914014](https://github.com/saadabdullaah/steadystate/actions/runs/31327914014),
  [31327915341](https://github.com/saadabdullaah/steadystate/actions/runs/31327915341),
  and [31327915599](https://github.com/saadabdullaah/steadystate/actions/runs/31327915599)
  unintentionally rendered the Phase 8 `xyz` catalog tenant into their
  payments-only historical suites. The operator now retries HTTPRoute
  conflicts from a fresh object, and the Phase 3–6 historical suites pass an
  explicit `payments` tenant filter. Focused tests lock both contracts.
- Regression-fix commit `6539d58` passed all five jobs in CI run
  [31338950523](https://github.com/saadabdullaah/steadystate/actions/runs/31338950523).
  Nightly run
  [31338965298](https://github.com/saadabdullaah/steadystate/actions/runs/31338965298)
  passed the corrected Service/HTTPRoute self-heal and complete Phase 1–3
  sequence; its Phase 1/2/3 artifacts are `9045335118`, `9045350218`, and
  `9045446257`, with GitHub SHA-256 digests
  `50707b97979c456303fbf8197274cbd4e2771faeebe4e61789ed7e6a5b44fd80`,
  `38d4eee01da07354bee4192218a3d314965accb8fa94faf221b5c3e659d6e353`,
  and `1e24a7856200017dfc1c9461ab266a4160d20348c86f01dc2eba74f7fff5862e`.
  Payments-isolated Phase 4/5/6 runs
  [31338962854](https://github.com/saadabdullaah/steadystate/actions/runs/31338962854),
  [31338964831](https://github.com/saadabdullaah/steadystate/actions/runs/31338964831),
  and [31338965000](https://github.com/saadabdullaah/steadystate/actions/runs/31338965000)
  passed with artifacts `9045517644`, `9045380578`, and `9045367194` and
  GitHub SHA-256 digests
  `719d40a7899a88e398a62ba2066f2e6147e3ed22b55b46bd09e9885afbd234b0`,
  `ef7ea89ef110362b775a08890113d88cc57bc97ccae87673eea47639dbdeae2f`,
  and `69ac92b3f7861e2a68306344b8c7a44609dd8206b98f71641454f4d1c2b9df6d`.
- Exact-main commit `7c5a15013b36a2da73079ea03c00ac52437864e3`
  passed CI, CodeQL, Platformctl smoke, Nightly Integration, and Phase 4–8
  acceptance. The first `v0.8.0` packaging run
  [31384745322](https://github.com/saadabdullaah/steadystate/actions/runs/31384745322)
  correctly stopped before producing release assets because the tracked CLI
  reference contained a useful Tempo trace-ID encoding note that was not
  emitted by the documentation generator. The command help now owns that
  contract, generation tests require it, and failed pre-build runs no longer
  report a second misleading missing-`dist` artifact error.
- Corrected tag run
  [31421862732](https://github.com/saadabdullaah/steadystate/actions/runs/31421862732)
  passed tests and generated-document verification, then stopped before
  building any archive because GoReleaser's publishing step had not received
  `GITHUB_TOKEN`. The tag step now maps the job-scoped token explicitly, and
  the GitOps contract suite prevents removing that tag-only requirement.

## v1.0.1 fresh-install hardening

- Phase 9 closeout PR #137 squash-merged at exact `main` commit
  `7531e097f21e6b434d00728449cba67e6a6ed012`. CI run
  [31967885468](https://github.com/saadabdullaah/steadystate/actions/runs/31967885468),
  CodeQL run
  [31967885482](https://github.com/saadabdullaah/steadystate/actions/runs/31967885482),
  Nightly run
  [31967932899](https://github.com/saadabdullaah/steadystate/actions/runs/31967932899),
  Phase 4/5/6/8 runs
  [31967935202](https://github.com/saadabdullaah/steadystate/actions/runs/31967935202),
  [31967936869](https://github.com/saadabdullaah/steadystate/actions/runs/31967936869),
  [31967939384](https://github.com/saadabdullaah/steadystate/actions/runs/31967939384), and
  [31967943635](https://github.com/saadabdullaah/steadystate/actions/runs/31967943635),
  Platformctl smoke run
  [31967947950](https://github.com/saadabdullaah/steadystate/actions/runs/31967947950),
  and release-snapshot run
  [31967949945](https://github.com/saadabdullaah/steadystate/actions/runs/31967949945)
  passed against that exact revision.
- The same sweep exposed full-profile cold-start pressure rather than a portal
  defect. Phase 7 run
  [31967941659](https://github.com/saadabdullaah/steadystate/actions/runs/31967941659)
  captured cert-manager cainjector exceeding its 128 MiB limit, while Phase 9
  run [31967945539](https://github.com/saadabdullaah/steadystate/actions/runs/31967945539)
  captured kube-apiserver/etcd restarts and an incomplete later Argo wave.
  Hosted Phase 7 now applies the same bounded kind control-plane priority before
  both initial and recovery GitOps, the control plane receives an 8:1 scheduling
  weight over each worker with a 3 GiB soft reservation, cainjector receives a
  measured 256 MiB limit, and only the full profile receives a 15-minute Argo
  readiness window within the existing 20-minute lifecycle stage.

- A clean Windows minimal-profile run on 2026-08-15 proved the prerequisite,
  pinned-tool, image-build, and Kubernetes 1.35.5/Calico path, then exposed an
  Envoy Gateway cold-start failure: the kind node's direct Docker Hub pull
  remained in `ContainerCreating` beyond Helm's five-minute hook timeout even
  though the host Docker engine fetched the exact image in 25 seconds.
- Bootstrap now pulls Envoy Gateway `v1.8.0` through the host engine, verifies
  digest `sha256:a9b4c4d8a402e8c007b74f2796587a6fb33d8baba46094f17c8a6f233e46d609`,
  and imports only the Linux AMD64 image into each exact kind node before Helm.
  This also avoids kind 0.32's `--all-platforms` import failure for the image's
  OCI attestation index. Helm retains a bounded ten-minute fallback.
- Backup-store node discovery now treats an already-absent kind cluster as an
  idempotent stop condition under Windows PowerShell 5.1, so `platform down`
  still removes the exact SeaweedFS resources while retaining its named volume.
- The same fresh-clone exercise found that `minimal` previously changed only
  kind topology while still rendering every standard-profile Argo child. The
  profile now has a truthful core-only contract: Argo configuration, operator,
  Gateway, and CRDs remain; tenant workloads, Rollouts/Prometheus, telemetry,
  Kyverno, and data add-ons are omitted. Standard and full renders are
  unchanged.
- Minimal downgrade now prepares every optional Argo child with the background
  resource finalizer before root reconciliation. The live readiness gate
  rejects both lingering child Applications and lingering optional Pods, which
  prevents an apparently green minimal profile from retaining the resource
  footprint of an earlier standard/full run.
- A clean fresh-clone run at `7070c22` completed `platformctl platform up`
  from an absent cluster in 10m40s. The nine-check minimal baseline reported
  only `argocd-configuration`, `steadystate-operator`, and `steadystate-root`,
  all Synced/Healthy; every optional add-on namespace contained zero Pods.
  `platform status` returned Ready, `platform verify` passed, and the single
  kind node used 3.441 GiB of its 9.713 GiB limit at inspection time. A real
  loopback portal launch/session exchange returned HTTP 200 from
  `portal.steadystate.dev/v1alpha1`, reported portal `v1.0.1` and profile
  `minimal`, and exposed the expected SHA-256 asset digest without leaving the
  portal process running.
