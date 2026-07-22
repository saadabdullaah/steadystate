# ADR-0004: Use Deterministic, Watch-Driven Application Reconciliation

## Context

An `Application` must converge to a running and reachable workload, repair deleted or drifted children quickly, and expose status that reflects Kubernetes observations rather than controller intent. Periodic imperative patches would make ownership unclear, create needless API writes, and delay self-healing.

## Decision

The SteadyState controller computes deterministic desired Deployment, Service, ConfigMap, and HTTPRoute objects from each `Application`. It reconciles them with `CreateOrUpdate`, applies controller owner references, and watches every owned kind so deletion and drift enqueue the owning `Application` immediately.

Builders remain pure and byte-stable. Reconciliation preserves server-assigned fields but restores SteadyState-owned fields. Status is derived from observed Deployment and HTTPRoute status, patched only when it changes, and retried on conflicts. The finalizer is released once SteadyState-specific cleanup is complete; Kubernetes garbage collection removes namespaced children.

The operator records a sorted fingerprint of the referenced Service UIDs in its own HTTPRoute annotation. A recreated Service changes that fingerprint once, which gives Gateway implementations a deterministic object update on which to re-resolve backend references. An unchanged backend set does not rewrite the route.

## Consequences

Self-healing does not depend on a polling interval, and an unchanged second reconcile performs zero writes. The operator is the only intended mutator of generated child specifications. Envtest covers rendering, updates, conditions, conflicts, and idempotency; a real kind cluster covers owner-watch timing and garbage collection because envtest does not run those Kubernetes controllers.
