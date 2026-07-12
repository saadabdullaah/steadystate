# ADR-0002: Use Kubebuilder for Operator Development

## Context

The project needs CRD scaffolding, controller-runtime integration, generated RBAC and manifests, and established testing conventions without hiding reconciliation behavior.

## Decision

Use Kubebuilder's Go v4 project layout at the monorepo root with API domain `steadystate.dev`.

## Consequences

The repository follows a recognized operator structure and retains direct access to controller-runtime. Kubebuilder is Linux/macOS-oriented, so Windows scaffolding may invoke it through WSL while Windows Git remains authoritative.
