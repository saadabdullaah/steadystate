[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$Platform = if ($env:OS -eq 'Windows_NT') { 'windows-amd64' } else { 'linux-amd64' }
$env:PATH = "$(Join-Path $Root ".tools/bin/$Platform")$([IO.Path]::PathSeparator)$env:PATH"
$Policies = Join-Path $Root 'gitops/platform/kyverno-policies'
$Values = Join-Path $Root 'tests/security/values.yaml'

if (-not (Get-Command kyverno -ErrorAction SilentlyContinue)) {
    throw 'The checksum-verified Kyverno CLI is missing.'
}

function Invoke-PolicyFixture {
    param([Parameter(Mandatory)][string]$Name, [Parameter(Mandatory)][bool]$ShouldPass)
    $resource = Join-Path $Root "tests/security/$Name"
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $output = @(& kyverno apply $Policies --resource $resource --values-file $Values --detailed-results 2>&1)
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previous
    $summary = $output -join [Environment]::NewLine
    if ($ShouldPass -and ($exitCode -ne 0 -or $summary -notmatch 'pass:\s*[1-9]')) {
        throw "Expected $Name to pass the enforced policy set."
    }
    if (-not $ShouldPass -and $summary -notmatch 'fail:\s*[1-9]') {
        throw "Expected $Name to demonstrate a blocking policy result."
    }
}

Invoke-PolicyFixture -Name 'compliant-managed-pod.yaml' -ShouldPass $true
Invoke-PolicyFixture -Name 'vulnerable-pod.yaml' -ShouldPass $false
Invoke-PolicyFixture -Name 'cnpg-label-bypass.yaml' -ShouldPass $false
Write-Host 'Kyverno static policy fixtures passed, including the CNPG label bypass regression.'
