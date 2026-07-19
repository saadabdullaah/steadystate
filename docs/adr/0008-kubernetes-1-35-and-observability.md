# ADR-0008: Align Kubernetes 1.35 Before Adding the Observability Plane

## Context

SteadyState completed Phase 4 on Kubernetes `1.36.1`. Phase 6 will use Kyverno `1.18.2`, whose supported Kubernetes range ends at `1.35`. Continuing on `1.36` would make admission compatibility an assumption and would leave the local cluster ahead of the policy engine's documented support window.

kind `v0.32.0` publishes a Kubernetes `1.35.5` node image. Kubernetes 1.35 also consumes kubeadm `v1beta3`, whose `nodeRegistration.kubeletExtraArgs` field is a map rather than the list form used by kubeadm `v1beta4` in the prior baseline.

## Decision

Rebaseline every SteadyState kind profile and the installed kubectl client on Kubernetes `1.35.5`. Pin the kind node image by digest in `scripts/versions.env`, and render every kind kubeadm patch with `kubeadm.k8s.io/v1beta3` and map-form kubelet arguments. Keep kind `0.32.0`, Go `1.25.12`, and the existing Go Kubernetes modules unchanged.

Treat this as a compatibility gate before activating Phase 5 observability. The branch must pass the five CI jobs, CodeQL, Nightly Integration, and the Phase 4 acceptance workflow. Diagnostics retain the expected version lock and capture the live client/server versions so a failed hosted proof can distinguish pin drift from cluster behavior.

ADR-0007 remains an accurate historical record of the Phase 4 release baseline and is not rewritten. This ADR will also record the Phase 5 telemetry topology, ownership, retention, resource budgets, and readiness-derived service health when those contracts are implemented.

## Consequences

Kyverno can be introduced later on a documented supported Kubernetes baseline, while all released Phase 0–4 behavior is re-proved before new platform components are added. The project deliberately gives up Kubernetes `1.36` features for compatibility and must not use `v1beta4` kubeadm patches until the baseline moves forward again.

The rebaseline is not complete merely because templates render. Hosted validation must prove cluster bootstrap, networking, GitOps, progressive delivery, rollback, and cleanup against the exact pinned image.
