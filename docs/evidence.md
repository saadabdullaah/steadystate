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
