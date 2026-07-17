# Vendored Prometheus Operator CRDs

The ServiceMonitor and PrometheusRule CRDs are extracted from the
kube-prometheus-stack `87.16.1` chart. They are used by envtest and schema
tests; the GitOps-managed monitoring release remains responsible for cluster
installation.

Their SHA-256 checksums are pinned in `scripts/versions.env` and verified by
`scripts/check-vendored.ps1`.

Source: `https://github.com/prometheus-community/helm-charts/releases/tag/kube-prometheus-stack-87.16.1`
