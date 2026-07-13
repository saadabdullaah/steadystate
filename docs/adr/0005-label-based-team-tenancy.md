# ADR-0005: Use Label-Based Team Tenancy and Finalizer-Controlled Namespace Deletion

## Context

SteadyState needs a small, inspectable tenancy boundary that works on a local kind cluster and proves isolation at the network, RBAC, quota, and image-repository levels. A `Team` is cluster-scoped, while most resources it manages are namespaced. Direct Team owner references on namespaced children would blur that scope boundary, and granting unrestricted namespace administration would allow Team users to remove the policies that provide isolation.

Hierarchical Namespace Controller and separate clusters per Team provide stronger delegation models, but add controllers, APIs, lifecycle behavior, and operating cost that are unnecessary for the two-tenant v1 demonstration.

## Decision

Each cluster-scoped `Team` deterministically manages one flat Namespace named `team-<name>`. The Namespace and every generated object carry `steadystate.dev/team=<name>` and a `steadystate.dev/team-uid` annotation. Reserved-name resources are adopted or deleted only when both identifiers match the current Team incarnation.

The Team controller will use `steadystate.dev/team-finalizer` to verify and delete the managed Namespace, wait for Kubernetes namespace cascading to finish, and then release the Team. It will not place Team owner references on namespaced children. Label-based watches will map child drift back to the cluster-scoped Team without periodic polling.

The generated owner Role permits Application management, configuration and Secret management, and workload diagnostics inside the Team Namespace. It excludes Namespace, ResourceQuota, LimitRange, NetworkPolicy, ServiceAccount, and RBAC mutation so Team users cannot remove platform guardrails. Owners are explicit Kubernetes `User` subjects; a non-automounting ServiceAccount receives the same Role for isolation testing.

Tenant Pods are default-denied for ingress and egress. DNS is allowed only to CoreDNS. North-south ingress is allowed only from proxy Pods belonging to the shared `steadystate` Gateway, selected by the Envoy Gateway managed-proxy labels and its system Namespace. Application repository authorization is derived from the verified Team Namespace and never from the descriptive `Application.spec.owner` field.

## Consequences

The tenancy model remains deterministic, easy to audit, and compatible with the existing Windows-first kind workflow. Team deletion has an intentionally strong blast radius, so UID verification and finalizer blocking are mandatory safety controls. Team users cannot administer platform guardrails directly. Workload egress, same-Team east-west traffic, hierarchical delegation, and external identity-provider group mapping require explicit future policy rather than being enabled implicitly.
