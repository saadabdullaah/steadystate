# SteadyState threat model

This STRIDE-lite model covers the repository, GitHub App, Actions/OIDC release
identity, GHCR/Sigstore, GitOps review, Kyverno, operator authority, Team
isolation, encrypted secrets, and the local single-cluster host.

| Threat | Boundary and mitigation | Residual risk |
|---|---|---|
| Spoofed maintainer or bot | Protected `main`, required checks/review, repository-scoped GitHub App | A sufficiently privileged GitHub account can approve malicious source |
| Spoofed release identity | Exact main-workflow OIDC subject and issuer; Rekor verification | GitHub OIDC or Sigstore compromise/outage blocks trusted publication |
| Tampered image or tag | Immutable pairwise tags, recorded digests, signatures, SPDX attestations, admission digest pinning | GHCR is required for new admissions |
| GitOps review bypass | One-manifest delivery PR, exact Argo revision, operator ownership of children | An authorized reviewer can approve malicious desired state |
| Policy bypass | Stable CEL policy, fail-closed webhooks, immutable Team selector, no native workload rights, CNPG bypass regression | Cluster administrators can alter admission or Namespace labels |
| Operator abuse | Explicit generated-resource RBAC and resource-name-scoped Team-owner binding | The cluster-wide operator remains trusted |
| Secret disclosure | SOPS ciphertext only, ignored/repository-secret age identity, short-lived plaintext, redacted evidence | A compromised runner or host can read material while decrypted |
| Tenant network escape | Calico default deny and exact Gateway, monitoring, OTLP, DNS, and same-Team selectors | Shared kernel, CNI, Gateway, and host remain trusted |
| Evidence repudiation | Source SHA, workflow, digest, identity, reports, checksums, and diagnostics | Repository administrators control retention |
| Denial of service | Bounded workflows/resources and 15-second fail-closed webhook timeout | Provider outage blocks new Pods by design |

## Trust decisions

- A signature proves workflow identity; review, scanners, SBOMs, and admission
  remain independent layers.
- `requireSignedImage=false` reports
  `SignatureVerificationNotRequested`; it never claims verification.
- Future CNPG exceptions must be exact. Broad Team exemptions are forbidden.
- Host administrator compromise is outside the local isolation claim.
- Runtime detection, Vault custody, and operator image signing remain future
  work.
