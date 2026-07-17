# Vendored Argo Rollouts CRDs

These namespaced Rollout, AnalysisTemplate, and AnalysisRun CRDs come from
Argo Rollouts `v1.9.0`. They are used by envtest and schema-contract tests;
the GitOps-managed Rollouts Helm release remains responsible for cluster
installation.

Their SHA-256 checksums are pinned in `scripts/versions.env` and verified by
`scripts/check-vendored.ps1`.

Source: `https://github.com/argoproj/argo-rollouts/tree/v1.9.0/manifests/crds`
