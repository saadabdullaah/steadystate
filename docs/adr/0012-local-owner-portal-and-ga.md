# ADR-0012: Embed a secure local-owner portal in platformctl

- Status: Accepted
- Date: 2026-08-11

## Context

Phase 8 established stable catalog, proposal, broker, cluster-read,
diagnostics, lifecycle, and break-glass contracts. A deployed portal would add
another identity system, privileged backend, database, lifecycle, and failure
domain. Duplicating reconciliation or accepting arbitrary YAML would weaken
the operator and Git review boundaries.

## Decision

The React portal is deterministically built and embedded in every
`platformctl` binary. The CLI process is its only backend and binds exclusively
to IPv4 loopback. Portal termination cannot affect the platform.

CLI commands and HTTP handlers call shared typed services. Reads return curated
summaries from catalog, Kubernetes, Argo, telemetry, policy, backup, and
GitHub sources. The browser refetches snapshots after typed SSE invalidations;
raw watch objects and arbitrary proxies are forbidden.

Normal writes reuse the v0.8 `ChangeRequest`, `TenantCatalog`, renderer, and
default-branch broker. A short-lived immutable plan binds the session, request,
operation, parameters, base SHA, render digest, and exact paths. Submission
re-renders before dispatch. Two-PR retirement remains intact.

Promote/abort stays a separate confirmed break-glass path with Rollout
UID/resource-version checks, Kubernetes Events, and a private local audit.

A random 256-bit launch token is valid once for 60 seconds. Its exchange creates
a host-only HttpOnly SameSite=Strict session for at most eight hours. Mutations
also require exact Host/Origin and an in-memory CSRF token. The server rejects
non-loopback hosts, CORS, oversized or non-JSON mutation bodies, unknown JSON,
unsupported methods, and excessive writes; it emits strict browser headers.

Native summaries cover daily decisions. Argo and Grafana remain external deep
links, not iframes. Multi-user/OIDC, remote exposure, raw Kubernetes browsing,
arbitrary queries, source/YAML editing, and a second control plane are rejected.

## Consequences

- All six CLI archives contain the same versioned portal assets.
- Portal memory is host-side and adds no in-cluster footprint.
- Host user, browser, checkout, `gh` session, and kubeconfig remain trusted.
- Session and plan state vanish with the process; GitHub remains durable review history.
- Local break-glass audit is not immutable enterprise user-audit storage.
- Any future remote experience needs an explicit authorization design.
