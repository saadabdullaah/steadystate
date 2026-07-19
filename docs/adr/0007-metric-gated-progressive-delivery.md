# ADR-0007: Use Metric-Gated Rollouts with Explicit Field Ownership

## Context

SteadyState must progress from rolling replacement to canary delivery without creating a second source of truth for application intent. Git and the `Application` resource remain desired state, the operator remains responsible for generated structure, and a rollout controller needs temporary control of traffic weights, selectors, and replicas. Promoting on missing traffic or an unavailable metrics provider would make failure look like success. Strategy changes must also remain recoverable when either controller restarts halfway through a cutover.

The platform is intentionally resource-constrained and Windows-first. Its existing Envoy Gateway, Argo CD app-of-apps graph, Team NetworkPolicies, and Go 1.25.12 baseline must remain compatible. The Prometheus Operator API module aligned with the selected monitoring chart requires a newer Go toolchain, so linking that module would violate the frozen baseline.

## Decision

Freeze Argo Rollouts Helm chart `2.41.0` and its packaged controller `v1.9.0`, Gateway API traffic-router plugin `v0.16.0`, kube-prometheus-stack `87.16.1`, and k6 `v2.1.0` for all of Phase 4. Verify every downloaded chart, plugin, and CLI against the SHA-256 values in `scripts/versions.env`. Keep Envoy Gateway `1.8.0`, Gateway API `1.4.0`, Kubernetes `1.36.1`, and Argo CD `3.4.2` unchanged.

Argo CD installs monitoring at sync wave `-18` and Rollouts at `-17`, before the operator at `-10`. External charts use exact revisions and repository-owned values through an exact-revision multi-source Application. The platform AppProject permits only those repositories, namespaces, and rendered resource kinds. The Rollouts dashboard and every unrelated traffic provider are disabled. The Gateway plugin receives only Service `get` and HTTPRoute `get`, `list`, `update`, and `patch` beyond the controller's core permissions.

The root Application ignores the monitoring child as an immediate-child health gate. Exact revision inheritance causes even tenant-only commits to refresh the monitoring child, and Argo can transiently report that multi-source chart Progressing after its workloads are already ready. Letting that transient state block later sync waves couples application delivery to an unchanged add-on. This annotation affects only the parent dependency calculation: bootstrap, `test-gitops`, and hosted acceptance still explicitly require monitoring and every other platform child to be Synced and Healthy before delivery begins.

Run one ephemeral Prometheus with six-hour retention and 15-second scrape/evaluation intervals, one null-routed Alertmanager, one non-persistent Grafana, and a Pod-focused kube-state-metrics. Disable node-exporter and unused control-plane scrapes and default rules. Prometheus discovers operator-generated ServiceMonitors and PrometheusRules across Team namespaces. Each Team owns a `steadystate-allow-monitoring` NetworkPolicy that permits only Prometheus Pods from the `monitoring` Namespace to the named TCP `http` port.

For canary Applications, retain a zero-replica Deployment as the official `workloadRef` template and reversible migration anchor. SteadyState owns the Deployment template, Rollout/analysis/monitoring structure, base Service structure, and HTTPRoute topology. Rollouts owns canary-mode Deployment replicas, stable/canary Service selectors, ReplicaSets, Pods, and AnalysisRuns. The Gateway plugin owns backend weights while a rollout is active. Reconciliation preserves those fields and repairs them only when their temporary owner is no longer active.

Metric decisions fail safe. Candidate success rate and P95 latency require real request vectors; empty vectors fail. Candidate restart absence evaluates to zero. The first failed metric aborts when automatic rollback is enabled, and two consecutive provider errors abort rather than promote. Automatic abort restores stable traffic but does not rewrite Git intent: status remains Degraded until a recovery commit requests the stable image. The last healthy version, digest, and Git revision remain atomic and unchanged throughout a failed candidate.

Derive rolling-to-canary and canary-to-rolling stages from live readiness instead of hidden persisted state. Create and validate the replacement data plane before switching the HTTPRoute, and remove obsolete resources only after the replacement is ready. A deleted Rollout is reconstructed from the last active release, never from an unanalysed failed desired image.

Use typed Argo Rollouts `v1.9.0` APIs in the controller. Build ServiceMonitor and PrometheusRule objects as schema-tested unstructured resources so the project can remain on Go `1.25.12`.

## Consequences

Good releases can progress through deterministic weighted steps while metrics and Envoy-observed traffic agree. Bad releases return traffic to the last healthy version without falsifying desired state or provenance. Controller restarts and strategy migrations can resume from Kubernetes truth, and explicit ownership prevents reconciliation fights.

Hosted acceptance proves both directions of migration, all four configured traffic weights with at least 500 samples each, promotion provenance, alert-backed automatic abort, three stable-only recovery windows, Argo/Kubernetes health agreement, and Git-only recovery. It retains the traffic measurements, AnalysisRuns, registry metadata, recordings, and success diagnostics as the release evidence rather than inferring correctness from controller state alone.

The monitoring stack adds a bounded local resource cost and several cluster-scoped CRDs. Provider outages deliberately pause or abort delivery, so availability of Prometheus becomes part of the promotion path. Full dashboards, recording rules, logs, traces, long-term storage, image signing, admission policy, and safe GitOps tenant deletion remain outside Phase 4.
