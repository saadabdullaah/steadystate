[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$versions = @{}
foreach ($line in Get-Content -LiteralPath (Join-Path $PSScriptRoot 'versions.env') -Encoding UTF8) {
    $trimmed = $line.Trim()
    if (-not $trimmed -or $trimmed.StartsWith('#')) { continue }
    $parts = $trimmed.Split('=', 2)
    $versions[$parts[0]] = $parts[1]
}

$expected = @{
    'config/gateway-api/crds/gateway.networking.k8s.io_gatewayclasses.yaml' = $versions.GATEWAYCLASS_CRD_SHA256
    'config/gateway-api/crds/gateway.networking.k8s.io_gateways.yaml' = $versions.GATEWAY_CRD_SHA256
    'config/gateway-api/crds/gateway.networking.k8s.io_httproutes.yaml' = $versions.HTTPROUTE_CRD_SHA256
    'config/rollouts/crds/argoproj.io_rollouts.yaml' = $versions.ARGO_ROLLOUT_CRD_SHA256
    'config/rollouts/crds/argoproj.io_analysistemplates.yaml' = $versions.ARGO_ANALYSIS_TEMPLATE_CRD_SHA256
    'config/rollouts/crds/argoproj.io_analysisruns.yaml' = $versions.ARGO_ANALYSIS_RUN_CRD_SHA256
    'config/monitoring/crds/monitoring.coreos.com_servicemonitors.yaml' = $versions.SERVICE_MONITOR_CRD_SHA256
    'config/monitoring/crds/monitoring.coreos.com_prometheusrules.yaml' = $versions.PROMETHEUS_RULE_CRD_SHA256
}

foreach ($entry in $expected.GetEnumerator()) {
    $path = Join-Path $Root $entry.Key
    if (-not (Test-Path -LiteralPath $path)) { throw "Missing vendored CRD: $($entry.Key)" }
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
    if ($actual -ne $entry.Value) { throw "Checksum mismatch for $($entry.Key): expected $($entry.Value), got $actual" }
    Write-Host "[PASS] $(Split-Path -Leaf $entry.Key)"
}
