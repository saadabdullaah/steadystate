# Vendored Gateway API CRDs

These three standard-channel CRDs come from the upstream Gateway API `v1.4.0`
release. SteadyState vendors only `GatewayClass`, `Gateway`, and `HTTPRoute` for
deterministic envtest startup. Their SHA-256 values are pinned in
`scripts/versions.env` and checked by `scripts/check-vendored.ps1`.

Source: `https://github.com/kubernetes-sigs/gateway-api/tree/v1.4.0/config/crd/standard`
