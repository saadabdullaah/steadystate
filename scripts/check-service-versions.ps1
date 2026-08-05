[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidatePattern('^[0-9a-f]{40}$')]
    [string]$BaseRevision
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Push-Location $Root
try {
    $sourceRevision = (git rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or $sourceRevision -cnotmatch '^[0-9a-f]{40}$') {
        throw 'Could not resolve the exact service source revision.'
    }
    $plan = go run ./cmd/platformctl service release-plan --base-sha $BaseRevision --source-sha $sourceRevision
    if ($LASTEXITCODE -ne 0) { throw 'Generated service VERSION validation failed.' }
    $parsed = $plan | ConvertFrom-Json
    Write-Host "Generated service VERSION contracts passed for $(@($parsed.include).Count) component(s)."
} finally {
    Pop-Location
}
