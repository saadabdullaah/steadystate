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
- operator, Alloy, OTel Collector, Loki, and Tempo logs;
- cluster diagnostics from the common diagnostics contract.

The Phase 5 schema is version `1`. It records the exact source SHA, profile, result/failure, timestamps, the current named stage, named elapsed checks, request/trace identity, and memory values. Memory passes only after three consecutive samples, 15 seconds apart, remain within both budgets; the raw samples and final per-container breakdown are retained in `metrics/memory.json`. The normally 3.5-minute functional recording has nested 12-minute screen, 13-minute process, and 14-minute step bounds inside the 45-minute job envelope, leaving time to capture and upload the last stage, failure evidence, and diagnostics before unconditional GitOps/cluster cleanup.
