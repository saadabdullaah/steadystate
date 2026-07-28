# ADR-0009: Supply-chain trust, admission policy, and secret custody

- Status: Accepted
- Date: 2026-07-23

## Context

SteadyState hardened generated Pods but did not prove who built an image,
whether it carried a software bill of materials, or whether the cluster would
reject a workload which bypassed the intended controls. Grafana bootstrap
credentials also needed a Git-safe custody model.

## Decision

Demo releases are built as immutable good and bad variants, scanned, described
by separate SPDX JSON SBOMs, and signed and attested with Cosign keyless signing.
The only trusted certificate subject is
`https://github.com/saadabdullaah/steadystate/.github/workflows/demo-release.yml@refs/heads/main`;
the issuer is `https://token.actions.githubusercontent.com`. Transparency-log
verification remains required. Partial or conflicting immutable tags fail
closed.

Kyverno 1.18.2 uses only stable CEL-based `ValidatingPolicy` and
`ImageValidatingPolicy` resources. Universal Team safety and strict Application
Pod security run in `Deny`. Unmanaged Team Pods and Applications requesting
signature verification require the trusted signature and SPDX attestation.
Image tags are resolved to digests at admission. Platform namespaces are
excluded through the immutable Team Namespace boundary, without user-selectable
exceptions.

The operator labels Pod templates with workload kind, signature intent, and
network-isolation intent. Team owners cannot create native Pods or workload
controllers and cannot mint trusted templates. Policy reports and workload
failures are watched, never polled. Admission rejection is reported through
`SecurityPolicyReady=False` without replacing the last healthy tuple.

Every Team remains default-deny. Isolated Applications receive only Gateway,
Prometheus, OpenTelemetry, and DNS paths. Non-isolated Applications may
communicate only with other non-isolated Applications in the same Team.

SOPS 3.13.2 and age 1.3.1 encrypt the Grafana administrator Secret. Only the
public recipient and ciphertext are tracked. The private identity remains in
the ignored local artifact directory and repository `SOPS_AGE_KEY` secret.
Decryption is short-lived and precedes dependent GitOps reconciliation.

## Consequences

Sigstore, GHCR, GitHub OIDC, Kyverno admission, and branch protection are
availability dependencies for new signed deployments. Existing serving
children remain available when a candidate is rejected. External verification
failure fails closed.

Phase 7 may add only exact CNPG/Barman identities and immutable digests.
The activated boundary pins the CNPG bootstrap-controller and Barman sidecar
at their charts, excludes CNPG-managed Pods from the demo-image
mutator/verifier, and enforces their exact PostgreSQL labels, cluster
ServiceAccount, controller ownership (including six pinned bootstrap/recovery
Job roles), and tag-plus-digest images in the universal Deny policy. This
avoids depending on Kyverno's internal image-verification annotation for a
non-demo identity. Paired fixtures prove the legitimate initdb shape succeeds
while forged CNPG labels cannot bypass policy.

Runtime threat detection, external secret management, operator image signing,
and multi-cluster trust remain outside this decision.
