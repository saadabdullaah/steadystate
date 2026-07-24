# Kyverno policy boundaries

Phase 6 installs Kyverno `1.18.2` through chart `3.8.2` on Kubernetes
`1.35.5`. The foundation checkpoint uses only the stable
`policies.kyverno.io/v1` `ValidatingPolicy` and `ImageValidatingPolicy`
APIs. Legacy `ClusterPolicy` resources are not installed.

## Enforced foundation

All validated policies use `validationActions: Deny`,
`failurePolicy: Fail`, a 15-second webhook timeout, and background
evaluation. Admission fails closed; background reports retain visibility for
existing resources.

Policies select namespaces carrying the operator-owned
`steadystate.dev/team` label. This immutable administrative boundary excludes
platform namespaces without maintaining an error-prone namespace-name list.

The tiers are:

- universal Team safety: no host namespaces, hostPath, privileged containers,
  mutable latest images, or missing CPU/memory requests and limits;
- SteadyState Application hardening: non-root, read-only root filesystem,
  no privilege escalation, and all capabilities dropped;
- image verification: unmanaged Team Pods and managed Applications explicitly
  requesting verification are resolved to digests and checked against the exact
  main-branch demo-release OIDC identity.

The image policy excludes a managed Application only when its operator-owned
label explicitly says `steadystate.dev/require-signed-image=false`. Status then
reports `SignatureVerificationNotRequested`; it never claims verification.

## CloudNativePG boundary

No Phase 7 exception exists in the foundation. Universal Team safety remains
applicable to CloudNativePG and Barman workloads.

If exact pinned CloudNativePG behavior later requires an exception, it must:

1. name only the affected policy and validation;
2. select the exact operator-managed ServiceAccount and workload labels;
3. restrict the namespace to the owning Team;
4. restrict images to the Phase 7 pinned repositories and digests;
5. preserve the universal privileged, host namespace, hostPath, latest-tag,
   and resource-requirement controls.

Wildcard policies, namespace-wide exemptions, `team-*` exemptions, and
user-supplied bypass labels are forbidden.
