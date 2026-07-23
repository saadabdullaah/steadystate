# Hosted Evidence Contracts

Every hosted acceptance artifact is revision-bound and must contain schema-versioned JSON, generated/rendered state, success or failure diagnostics captured before cleanup, and the phase's real VHS recording where required. Missing required files fail the workflow.

Phase 5 uploads `phase5-acceptance-<commit>` with:

- `phase5-request-telemetry.gif` and its tracked tape;
- `evidence.json` and the incremental `state.json`;
- Grafana Prometheus/Loki/Tempo datasource health responses;
- correlated Prometheus, Loki, and Tempo query results for one request/trace identity;
- Prometheus, Alertmanager, and Grafana fast-burn alert results;
- stabilized memory working-set measurements, the bounded sample series, and a per-container observability breakdown;
- rendered GitOps platform state and Kubernetes/Argo snapshots;
- operator, Grafana, Alloy, OTel Collector, Loki, and Tempo logs;
- cluster diagnostics from the common diagnostics contract.

The Phase 5 schema is version `1`. It records the exact source SHA, profile, result/failure, timestamps, the current named stage, named elapsed checks, request/trace identity, and memory values. Memory passes only after three consecutive samples, 15 seconds apart, remain within both budgets; the raw samples and final per-container breakdown are retained in `metrics/memory.json`. The functional proof has a seven-minute screen bound and is rendered at two frames per second with 8x playback, inside eight-minute process and nine-minute step bounds. This preserves the complete terminal proof while leaving the 40-minute job envelope available for failure evidence, Grafana startup logs, diagnostics, artifact upload, and unconditional cleanup.

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
