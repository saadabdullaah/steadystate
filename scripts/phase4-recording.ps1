[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('Promote','Rollback')][string]$Stage,
    [ValidateSet('minimal','standard','full')][string]$Profile = 'standard',
    [string]$EvidencePath = '.artifacts/phase4/acceptance/evidence.json'
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$ArtifactRoot = Join-Path $Root '.artifacts/phase4/acceptance'
$StatePath = Join-Path $ArtifactRoot 'state.json'
$ResultProperty = if ($Stage -eq 'Promote') { 'promotionResult' } else { 'rollbackResult' }
$MarkerPrefix = if ($Stage -eq 'Promote') { 'PHASE4_PROMOTION_RESULT' } else { 'PHASE4_ROLLBACK_RESULT' }
$TimeoutMinutes = if ($Stage -eq 'Promote') { 18 } else { 33 }

function Read-AcceptanceState {
    if (-not (Test-Path -LiteralPath $StatePath -PathType Leaf)) { return $null }
    try {
        return Get-Content -Raw -LiteralPath $StatePath -Encoding UTF8 | ConvertFrom-Json
    } catch {
        return $null
    }
}

Push-Location $Root
try {
    $recordingRoot = Join-Path $ArtifactRoot 'recordings'
    New-Item -ItemType Directory -Path $recordingRoot -Force | Out-Null
    $stdout = Join-Path $recordingRoot "$($Stage.ToLowerInvariant())-stdout.log"
    $stderr = Join-Path $recordingRoot "$($Stage.ToLowerInvariant())-stderr.log"
    $pwsh = (Get-Command pwsh -ErrorAction Stop).Source
    $arguments = @(
        '-NoProfile',
        '-File', (Join-Path $PSScriptRoot 'phase4-acceptance.ps1'),
        '-Stage', $Stage,
        '-Profile', $Profile,
        '-EvidencePath', $EvidencePath
    )
    $process = Start-Process -FilePath $pwsh -ArgumentList $arguments -PassThru -RedirectStandardOutput $stdout -RedirectStandardError $stderr
    $deadline = (Get-Date).AddMinutes($TimeoutMinutes)
    $reportedChecks = @{}
    $state = $null

    while ((Get-Date) -lt $deadline) {
        $state = Read-AcceptanceState
        if ($state) {
            foreach ($check in @($state.checks)) {
                if (-not $reportedChecks.ContainsKey($check.name)) {
                    Write-Host "[PASS] $($check.name)" -ForegroundColor Green
                    $reportedChecks[$check.name] = $true
                }
            }
            $result = [string]$state.$ResultProperty
            if ($result -in @('passed','failed')) { break }
        }
        if ($process.HasExited) { break }
        Write-Host "[WAIT] Phase 4 $($Stage.ToLowerInvariant()) is still running..." -ForegroundColor DarkGray
        Start-Sleep -Seconds 5
    }

    $state = Read-AcceptanceState
    $result = if ($state) { [string]$state.$ResultProperty } else { 'missing' }
    if ($result -eq 'passed') {
        if (-not $process.HasExited -and -not $process.WaitForExit(10000)) {
            Stop-Process -Id $process.Id -ErrorAction SilentlyContinue
        }
        Clear-Host
        foreach ($check in @($state.checks | Select-Object -Last 8)) {
            Write-Host "[PASS] $($check.name)" -ForegroundColor Green
        }
        Write-Host "${MarkerPrefix}_PASSED" -ForegroundColor Cyan
        Start-Sleep -Seconds 2
        exit 0
    }

    if (-not $process.HasExited) { Stop-Process -Id $process.Id -ErrorAction SilentlyContinue }
    Clear-Host
    if (Test-Path -LiteralPath $stdout -PathType Leaf) { Get-Content -LiteralPath $stdout -Tail 20 | Out-Host }
    if (Test-Path -LiteralPath $stderr -PathType Leaf) { Get-Content -LiteralPath $stderr -Tail 20 | Out-Host }
    $failure = if ($state -and $state.failure) { [string]$state.failure } else { "Recording wrapper timed out or exited before $ResultProperty became passed." }
    Write-Host "[FAIL] $failure" -ForegroundColor Red
    Write-Host "${MarkerPrefix}_FAILED" -ForegroundColor Red
    Start-Sleep -Seconds 2
    exit 1
} finally {
    Pop-Location
}
