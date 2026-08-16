# Hosted Evidence Contracts

Every hosted acceptance artifact is revision-bound and must contain schema-versioned JSON, generated/rendered state, success or failure diagnostics captured before cleanup, and the phase's real VHS recording where required. Missing required files fail the workflow.

Phase 5 uploads `phase5-acceptance-<commit>` with:

- `phase5-request-telemetry.gif`, its tracked tape, and the timestamped transcript rendered by VHS;
- `evidence.json` and the incremental `state.json`;
- Grafana Prometheus/Loki/Tempo datasource health responses;
- correlated Prometheus, Loki, and Tempo query results for one request/trace identity;
- Prometheus, Alertmanager, and Grafana fast-burn alert results;
- stabilized memory working-set measurements, the bounded sample series, and a per-container observability breakdown;
- rendered GitOps platform state and Kubernetes/Argo snapshots;
- operator, Grafana, Alloy, OTel Collector, Loki, and Tempo logs;
- cluster diagnostics from the common diagnostics contract.

The Phase 5 schema is version `1`. It records the exact source SHA, profile, result/failure, timestamps, the current named stage, named elapsed checks, request/trace identity, and memory values. Memory passes only after three consecutive samples, 15 seconds apart, remain within both budgets; the raw samples and final per-container breakdown are retained in `metrics/memory.json`. The functional proof runs directly under a 15-minute step bound and writes a timestamped stage/check transcript. Only after success does pinned VHS render that transcript at two frames per second inside a four-minute process bound. Browser and GIF encoding therefore cannot terminate a valid proof or consume its five-minute memory-stabilization window, while the 40-minute job envelope still retains failure evidence, diagnostics, upload, and cleanup time.

Phase 6 uploads `phase6-acceptance-<commit>` with:

- `phase6-admission-denial.gif` and its tracked tape;
- schema-versioned `evidence.json` and incremental `state.json`;
- sanitized unsigned, wrong-identity, unsafe-Pod, and label-spoofing admission responses;
- stable CEL policy and PolicyReport snapshots;
- public Cosign signature and SPDX-attestation verification output for both image variants;
- the wrong-identity fixture SPDX SBOM;
- Application, ReplicaSet, Pod, and NetworkPolicy snapshots proving security status and active-tuple preservation;
- Kyverno/operator logs, resource measurements, scanner reports where applicable, and redacted cluster diagnostics.

The Phase 6 schema is version `1`. Evidence never contains credentials, decrypted Secret values, private age material, GitHub tokens, or request authorization. Success requires every named check, a non-empty recording, Cosign/SBOM evidence, security snapshots, logs, and common diagnostics. Failure capture runs before bounded cleanup and remains uploadable.

Phase 7 uploads a compatibility artifact and
`phase7-acceptance-<commit>`. The latter contains the disaster-recovery GIF,
tape, and timestamped transcript from the real hosted drill, schema-versioned
evidence, RTO/RPO report, Git revisions, canonical
source/recovery checksums, backup/WAL metadata, sanitized object inventory,
CNPG/Barman/Argo/platform snapshots, anonymous signature/attestation proof,
resource measurements, component logs, and common diagnostics.

Phase 7 schema version `1` forbids Secret data, S3 credentials, PostgreSQL
passwords/URIs, GitHub tokens, SQL values, and authorization headers. Its ten
named checks require exact pinned data/security state, a value-free database
span in Tempo, checksum equality, RTO `<=30m`, confirmed archive RPO boundary
`<=5m`, a backup-freshness alert that fires and clears, final-backup deletion
with retained objects, all memory budgets, and a non-empty recording.
The full drill runs before VHS so a browser and GIF encoder cannot compete
with the disposable full-profile control plane. VHS records the concise real
stage/check transcript only after every functional assertion passes.

Phase 8 uploads `phase8-acceptance-<commit>` with:

- `phase8-zero-to-live.gif`, its tracked tape, and the timestamped transcript;
- schema-versioned state/evidence with exact source, disposable branch,
  proposal IDs/digests, PR URLs/authors/merge actors, revisions, checks, and
  finalizer timeline;
- the human scaffold/activation PR A/B metadata and the acceptance-only
  retirement PR C/D metadata;
- exact CLI version, cluster, Team, Application, Database, backup, provenance,
  rollout, SLO, policy, and doctor output;
- signed service-image manifests, Cosign verification, verified DSSE
  attestation envelopes, and decoded SPDX JSON SBOMs for the web and API
  components;
- frontend/same-origin API/PostgreSQL results, canonical order checksum, and
  Prometheus/Loki/Tempo evidence;
- healthy and failure-diagnosis fixtures, confirmed break-glass rejection
  output, rendered proposal results, and redacted audit-contract proof;
- baseline, retiring, and finalized catalog plus deterministic GitOps renders;
- the live tenant Argo background-cascade finalizer at the retiring revision,
  before the finalization proposal is allowed to proceed;
- Argo, Team, Application, Database, Rollout, AnalysisRun, route, Pod, Service,
  tenant-filter isolation, platform-foundation progress, backup-retention, and
  resource-usage snapshots;
- direct kind-container inspect, kubelet/containerd journal, and CRI state on
  pre-product failures where the Kubernetes API is unavailable;
- success/failure component logs and common diagnostics captured before
  cleanup.

Phase 8 schema version `1` requires seven unique elapsed checks: exact CLI/live
health, live tenant-filter isolation, frontend/API/PostgreSQL behavior,
canary/provenance/telemetry/policy and diagnosis, resource budget, two-PR
finalizer retirement, and absence of residual live/request resources.
Auto-merge is accepted only for PRs whose base
is the exact disposable `acceptance/phase8-<run>-<attempt>` ref. The artifact
must prove that no open acceptance PR or request branch remains, the Team
namespace and CR graph disappeared, and the final Database archive stayed in
the external store. Private keys, tokens, authorization, age identities,
PostgreSQL URIs/passwords, and Secret values are forbidden.

Phase 9 uploads `phase9-acceptance-<commit>` with real Playwright screenshots,
a WebM, a deterministic GIF composed from those frames, schema-versioned
evidence, accessibility results, asset hashes and sizes, sanitized API
summaries, SSE/performance measurements, broker run and App-authored smoke PR
metadata, curated platform snapshots, and pre-cleanup diagnostics.

Launch tokens, cookies, browser profiles, HAR files, kubeconfigs, GitHub/App
tokens, SOPS identities, Secret values, and unredacted logs are forbidden.
Launch material stays in a private ignored directory removed before upload.
Success requires the unmerged smoke PR to be closed and its exact branch
deleted.

The latest verified Phase 9 branch artifact is `9263060350` from run
`31943997118`, against portal control-ledger commit
`221fab1e2a40d31b2ec762f66455b6adbd4139b5`. Its artifact digest is
`sha256:bd333c3b6c0786ba071cd8197125e6498281e20e6b3e831b26c2baa47ec9068a`;
the evidence JSON, GIF, and WebM SHA-256 values are respectively
`c2eb0ca81353a2247fa352606accd88801ac05f1bc4d62fc19475f342900e1c8`,
`b4b9b3f6e81f87588b58cb7465afde69287c96e4301f9e06b0ee3a099203e0e8`, and
`fd173d83484edfa64d5cc011e2746d2d2a36a62cdbdf68a40b83d7c47f453de4`.
