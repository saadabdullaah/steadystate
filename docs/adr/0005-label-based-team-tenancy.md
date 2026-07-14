# ADR-0005: Use Label-Based Team Tenancy and Finalizer-Controlled Namespace Deletion

## Context

SteadyState needs a small, inspectable tenancy boundary that works on a local kind cluster and proves isolation at the network, RBAC, quota, and image-repository levels. A `Team` is cluster-scoped, while most resources it manages are namespaced. Direct Team owner references on namespaced children would blur that scope boundary, and granting unrestricted namespace administration would allow Team users to remove the policies that provide isolation.

Hierarchical Namespace Controller and separate clusters per Team provide stronger delegation models, but add controllers, APIs, lifecycle behavior, and operating cost that are unnecessary for the two-tenant v1 demonstration.

## Decision

Each cluster-scoped `Team` deterministically manages one flat Namespace named `team-<name>`. The Namespace and every generated object carry `steadystate.dev/team=<name>` and a `steadystate.dev/team-uid` annotation. Reserved-name resources are reconciled only when the exact Team UID matches; the controller repairs ordinary label drift but never adopts an object without the UID. Namespace deletion requires both identifiers to match the current Team incarnation.

The Team controller uses `steadystate.dev/team-finalizer` to verify and delete the managed Namespace, wait for Kubernetes namespace cascading to finish, and then release the Team. It does not place Team owner references on namespaced children. Label-based watches map child drift back to the cluster-scoped Team without periodic polling.

The install-time Team owner ClusterRole permits Application management, configuration and Secret management, and workload diagnostics only when bound inside a Team Namespace. It excludes Namespace, ResourceQuota, LimitRange, NetworkPolicy, ServiceAccount, and RBAC mutation so Team users cannot remove platform guardrails. Each Team receives a generated RoleBinding for explicit Kubernetes `User` subjects and a non-automounting ServiceAccount used by isolation testing.

Kubernetes prevents a controller from creating a Role containing permissions the controller does not itself hold. The fixed ClusterRole is therefore installed by the cluster administrator rather than manufactured dynamically. The manager receives `bind` only for the named `steadystate-team-owner` ClusterRole and has no `escalate` permission. It does not receive the delegated Secret, Pod execution, or other tenant permissions directly. The immutable RoleBinding role reference, deterministic builder, protected Team-user RBAC, and exact Team UID ownership check constrain delegation to the platform-owned tenant role.

The static ClusterRole's Secret management and Pod execution permissions are accepted only because every SteadyState binding is a namespaced RoleBinding and the repository contains no ClusterRoleBinding for it. Trivy and Checkov exceptions are scoped to this manifest, documented with expiry dates where supported, and backed by tests that require the manager's `bind` authority to name exactly this role and reject any `escalate` verb.

Tenant Pods are default-denied for ingress and egress. DNS is allowed only to CoreDNS. North-south ingress is allowed only from proxy Pods belonging to the shared `steadystate` Gateway, selected by the Envoy Gateway managed-proxy labels and its system Namespace. Application repository authorization is derived from the verified Team Namespace and never from the descriptive `Application.spec.owner` field. An Application outside a verified Team Namespace, or whose repository does not match the Team's anchored allowlist, receives `ConfigurationReady=False` and no child mutation. Team and Namespace watches provide immediate recovery when authorization is corrected.

## Consequences

The tenancy model remains deterministic, easy to audit, and compatible with the existing Windows-first kind workflow. Team deletion has an intentionally strong blast radius, so UID verification and finalizer blocking are mandatory safety controls. Team users cannot administer platform guardrails directly. Workload egress, same-Team east-west traffic, hierarchical delegation, and external identity-provider group mapping require explicit future policy rather than being enabled implicitly.
