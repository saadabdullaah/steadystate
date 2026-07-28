# Kyverno policy boundaries

Phase 6 installs Kyverno `1.18.2` through chart `3.8.2` on Kubernetes
`1.35.5`. The foundation checkpoint uses only the stable
`policies.kyverno.io/v1` `ValidatingPolicy`, `MutatingPolicy`, and
`ImageValidatingPolicy` APIs. Legacy `ClusterPolicy` resources are not
installed.

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
- image custody: a registry-aware `MutatingPolicy` rewrites unmanaged Team Pods
  and managed Applications explicitly requesting verification to
  `repository@sha256:...`; an `ImageValidatingPolicy` independently checks the
  exact main-branch demo-release OIDC identity and SPDX attestation.

Kyverno `1.18.2` evaluates `ImageValidatingPolicy.validationConfigurations`
and records verification outcomes, but its CEL handler does not itself emit an
image-field patch. Keeping resolution in a separate stable `MutatingPolicy`
makes the immutable-reference boundary explicit and testable without falling
back to deprecated `ClusterPolicy`.

The image policy excludes a managed Application only when its operator-owned
label explicitly says `steadystate.dev/require-signed-image=false`. Status then
reports `SignatureVerificationNotRequested`; it never claims verification.

## CloudNativePG boundary

Phase 7 excludes CloudNativePG-managed Pods from the SteadyState demo-image
mutator and signature identity. A dedicated expression in the universal Deny
policy admits them only when all of these are true:

1. the CloudNativePG-managed PostgreSQL/database label set, with instance and
   cluster labels equal;
2. the operator-generated ServiceAccount whose name equals the Cluster;
3. either a direct controller reference to that Cluster or a controller
   reference to a cluster-prefixed Job with one of CloudNativePG `1.30.0`'s
   exact roles (`import`, `initdb`, `pgbasebackup`, `full-recovery`, `join`, or
   `snapshot-recovery`);
4. only the frozen PostgreSQL operand, CNPG bootstrap controller, and Barman
   sidecar image references, across both normal and init containers.

The CNPG and Barman charts inject tag-plus-digest references, so no admission
mutation or internal Kyverno verification annotation is needed for these
operands. This is not a `PolicyException`: universal Team safety remains
applicable to CloudNativePG and Barman workloads, including the privileged,
host namespace, hostPath, latest-tag, resource-requirement, and exact-identity
controls. A positive fixture proves the real initdb shape is admitted; a
forged-label fixture remains denied.

Wildcard policies, namespace-wide exemptions, `team-*` exemptions, and
user-supplied bypass labels are forbidden.
