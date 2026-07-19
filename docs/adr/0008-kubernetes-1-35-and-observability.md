# ADR-0008: Kubernetes 1.35 Alignment and the Observability Plane

## Context

SteadyState completed Phase 4 on Kubernetes `1.36.1`. Phase 6 will use Kyverno `1.18.2`, whose documented support ends at Kubernetes 1.35. kind `v0.32.0` publishes an exact Kubernetes `1.35.5` image, and Kubernetes 1.35 uses kubeadm `v1beta3` with map-form `nodeRegistration.kubeletExtraArgs`.

Phase 4 already installed a deliberately trimmed kube-prometheus-stack. Phase 5 needs correlated application metrics, logs, and traces without adding cloud dependencies, a second Prometheus/Grafana pair, controller polling, or unbounded laptop resource consumption.

## Decision

Rebaseline all kind profiles and kubectl on Kubernetes `1.35.5`, pin the kind image by digest, and use kubeadm `v1beta3` map-form patches. Keep Go `1.25.12`, kind `0.32.0`, and the Go Kubernetes modules unchanged. ADR-0007 remains the historical Phase 4 baseline rather than being rewritten.

Extend the existing `monitoring` Namespace and Grafana installation with exact-revision Argo children:

- Loki chart `18.5.1` / app `3.7.3` at wave `-16`.
- Tempo chart `1.24.4` / app `2.9.0` at wave `-15`.
- OpenTelemetry Collector chart `0.165.0` / app `0.156.0` at wave `-14`.
- Grafana Alloy chart `1.10.1` / app `1.17.1` at wave `-13`.

Every chart archive is checksum-verified. Loki and Tempo are single-process, one-replica, 24-hour development stores using capped emptyDir volumes. Alloy is a DaemonSet with read-only Pod discovery and host log access; it retains only `team-*` Pods labeled `steadystate.dev/logs=true`. One OpenTelemetry Collector accepts cluster-internal OTLP, adds Kubernetes identity, rejects spans without service identity, batches, and forwards to Tempo. Loki, Tempo, Prometheus, and Alertmanager receive no external HTTPRoute. Grafana alone is exposed through the loopback-bound shared Gateway.

The Application operator owns telemetry intent rather than telemetry storage. It writes deterministic `steadystate.dev/logs` and `steadystate.dev/traces` Pod labels, OTLP environment only when traces are enabled, a collector-specific egress NetworkPolicy, and Application-owned ServiceMonitor/PrometheusRule resources. Disabling metrics removes the monitor and rule even for a rolling Application. Source-revision-only changes remain zero-workload-mutation updates.

The demo emits structured JSON access logs and W3C Trace Context/OTLP spans for application requests. It preserves or generates `X-Request-ID`, correlates request/trace/span identity, normalizes routes, and excludes health/readiness/metrics traffic. Bodies, query strings, credentials, and request values are never recorded.

Grafana uses explicit stable datasource UIDs for Prometheus, Loki, and Tempo. GitOps-owned dashboard ConfigMaps provide an Application view and a platform overview, including RED metrics, rollout state, availability burn, log/trace pivots, custom-resource health, alerts, and memory footprint.

Generated SLO rules record request rate, error rate, availability, P95 latency, and error-budget burn. Fast burn requires both 5-minute and 1-hour windows above `14.4`; slow burn requires both 30-minute and 6-hour windows above `6`. Empty request and latency vectors fail safe.

Add `Application.status.conditions[type=ServiceHealth]`, derived only from current workload availability and accepted/resolved HTTPRoute status. The controller never polls Prometheus, Alertmanager, Loki, Tempo, or Grafana.

## Resource and retention boundaries

- Loki: `256Mi` request / `512Mi` limit, capped 4 GiB emptyDir, 24-hour retention.
- Tempo: `128Mi` request / `256Mi` limit, capped 2 GiB emptyDir, 24-hour retention.
- OTel Collector: `64Mi` request / `128Mi` limit.
- Alloy: `64Mi` request / `128Mi` limit per node, plus bounded reloader.
- Hosted Phase 5 gate: observability working set at or below 900 MiB; standard-profile total at or below 6.5 GiB.

These are laptop demonstration budgets, not production capacity claims.

## Consequences

Kyverno can be introduced on a documented compatible Kubernetes baseline. Developers gain one request identity across Prometheus, Loki, Tempo, and Grafana while the reconciliation path stays watch-driven and backend-independent. Each Application can opt out independently without broadening collector access.

Loki and Tempo data disappears with the disposable cluster and is intentionally short-lived. Alloy requires platform-level host log access, so it remains confined to the trusted `monitoring` Namespace and is not a tenant resource. A telemetry-backend outage can affect dashboards and progressive analysis, but it cannot make `ServiceHealth` falsely report workload availability. Durable telemetry, external alert delivery, runtime security, and long-term capacity planning remain outside this phase.

Phase 5 publication requires the Kubernetes rebaseline to pass all existing Phase 0-4 hosted proofs, plus correlated metrics/logs/traces, opt-outs, fast-burn alerting, dashboard datasource health, resource budgets, retained diagnostics, and exact-main release gates.
