# ADR-0011: Use platformctl with a typed Git broker and protected lifecycle

- Status: Accepted
- Date: 2026-08-07
- Decision owners: SteadyState maintainers

## Context

Phases 0-7 established declarative APIs, GitOps, progressive delivery,
observability, admission security, and durable PostgreSQL recovery. Those
capabilities were operable, but a developer still needed repository knowledge,
Kubernetes commands, and hand-authored desired-state files. A daily golden path
needs to reduce that surface without creating a second control plane, moving
GitHub App private keys onto laptops, bypassing review, or transferring
operator-owned resources to Argo.

The platform also needs safe retirement. Enabling Argo prune globally without
per-resource protection could turn one accidental manifest deletion into
Application or Database loss. Conversely, permanently disabling prune prevents
Git from completing an intentional deletion and its finalizer workflow.

Direct Rollout promotion and abortion are useful incident tools, but they are
imperative mutations. Treating them as ordinary CLI writes would undermine the
Git recovery boundary and create an inaccurate audit claim.

## Decision

### Local-owner CLI

Use one CGO-disabled Go CLI, `platformctl`, as the supported developer
entrypoint. It owns no in-cluster component. It reads Kubernetes and existing
monitoring APIs with the configured local cluster-admin context and delegates
cluster lifecycle to the established PowerShell contract. Configuration is
non-secret and portable across Windows, Linux, and macOS.

The CLI has stable table/JSON/YAML output, bounded timeouts, redaction, and exit
codes. Version metadata embeds semver, commit, date, Go version, and dirty state.
Release archives cover Windows, Linux, and macOS on amd64/arm64 with checksums,
SPDX SBOMs, checksum signature, and GitHub build provenance. Windows and Linux
support the full local lifecycle; macOS local kind operation is best-effort.

### Typed GitHub App broker

Normal writes become `cli.steadystate.dev/v1alpha1 ChangeRequest` proposals.
The CLI sends only schema version, request UUID, exact base SHA, and strict
Base64 JSON to a fixed workflow. Proposal content is capped at 48 KiB. Resource
names and paths are derived from validated DNS labels; users cannot submit raw
files, arbitrary paths, commands, workflows, or Kubernetes children.

The workflow validates and renders before App authentication, then mints the
repository-scoped App token and re-renders. Proposal and render digests must be
identical across the boundary. The base must still equal `main`, changed paths
must equal the renderer allowlist, and request-ID reuse with different content
fails closed. The PR body records the trusted `github.actor`, operation, base,
renderer, digests, workflow, and changed paths. The App retains only Metadata
read, Contents read/write, and Pull Requests read/write.

### Catalog and monorepo templates

Use `TenantCatalog` as the deterministic root input instead of hard-coded
payments/demo/orders values. Existing names and paths are preserved during
migration. Generated Go APIs, embedded React static sites, and full-stack
React/Go/PostgreSQL services stay in this monorepo with strict VERSION files,
tests, lockfiles, Dockerfiles, descriptors, and developer documentation.

Scaffold PR A creates source and non-Application intent. The main-branch generic
release workflow publishes immutable component/version/SHA tags to the shared
public package, scans them, creates SPDX JSON, signs and attests their digests
with GitHub OIDC, and verifies the exact workflow identity. Activation PR B is
created only after publication. This ordering prevents Argo from requesting an
image that does not exist or lacks the enforced provenance.

### Protected prune and two-PR deletion

Tenant Argo Applications may prune, but every active Team, Application, and
Database carries `argocd.argoproj.io/sync-options: Prune=false`.

1. An approval PR adds one deletion-request UUID, marks the selected catalog
   entries Retiring, and removes prune protection only from those resources.
   Team retirement also puts the Argo resources finalizer on the exact tenant
   child Application.
2. A finalization PR is valid only after the approval commit is in `main`, the
   expected Argo revision is visible, and the live resource carries that UUID.
   It removes the catalog entry and deterministic leaf. Argo prunes the CR;
   SteadyState finalizers perform workload cleanup, final backup, Database
   deletion, ordered Team deletion, and namespace removal.

Force remains an explicit Git-reviewed `--force --acknowledge-data-loss`
operation. The CLI never invents or adds it automatically.

Hosted acceptance may auto-merge retirement PRs only against the disposable
`acceptance/phase8-<run>-<attempt>` branch and only inside the Phase 8 workflow.
The same function rejects `main`, normal branches, missing workflow context, or
an unexpected base. This exception proves live finalizers without weakening the
human review boundary.

### Diagnosis and break glass

`app doctor` evaluates dependencies in a fixed order: local/context and Team,
Git/Argo, Application/provenance, policy, workload/rollout, Gateway, metrics,
logs/traces, and Database/backups. Each result is Pass, Warning, Fail, or
Unknown with sanitized evidence and remediation. Unknown never means healthy.

`app promote` and `app abort` remain explicit break glass. They require a
reason, exact Application confirmation, a canary target, and optimistic
resource-version checks. They emit Kubernetes Events and a redacted local JSON
record before and after mutation. These laptop records are operational evidence,
not an immutable enterprise audit log, and they do not replace a recovery Git
change.

## Consequences

- Developers can create and operate supported services without authoring
  Kubernetes YAML or holding the GitHub App key.
- Git review, Argo ownership, operator ownership, Kyverno admission, telemetry,
  and Database finalizers remain the authoritative delivery path.
- A stale checkout or broker base fails instead of silently rebasing intent.
- The monorepo makes template and platform changes atomic but intentionally does
  not support arbitrary external source repositories in v0.8.
- The CLI's local cluster-admin scope is appropriate for the laptop owner, not a
  multi-user portal. Phase 9 must use authenticated backend APIs for shared
  users rather than distributing kubeconfig access.
- The future portal will consume the stable CLI/broker/catalog contracts and
  remain a client; it will not become another reconciler or control plane.

## Alternatives rejected

- Store the App private key in `platformctl`: rejected because it expands secret
  custody to every workstation.
- Let the CLI push branches directly with the developer token: rejected because
  actor, renderer, permissions, and idempotency would vary by laptop.
- Generate arbitrary repositories or accept raw template paths: rejected because
  they defeat deterministic rendering and the file allowlist.
- Enable unprotected Argo prune: rejected because an accidental one-step removal
  could bypass finalizer intent and review.
- Build a portal first: rejected because it would invent APIs before the daily
  workflow, trust boundary, and lifecycle contract were stable.
