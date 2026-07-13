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
    'gateway.networking.k8s.io_gatewayclasses.yaml' = $versions.GATEWAYCLASS_CRD_SHA256
    'gateway.networking.k8s.io_gateways.yaml' = $versions.GATEWAY_CRD_SHA256
    'gateway.networking.k8s.io_httproutes.yaml' = $versions.HTTPROUTE_CRD_SHA256
}

foreach ($entry in $expected.GetEnumerator()) {
    $path = Join-Path $Root "config/gateway-api/crds/$($entry.Key)"
    if (-not (Test-Path -LiteralPath $path)) { throw "Missing vendored Gateway API CRD: $($entry.Key)" }
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
    if ($actual -ne $entry.Value) { throw "Checksum mismatch for $($entry.Key): expected $($entry.Value), got $actual" }
    Write-Host "[PASS] $($entry.Key)"
}
