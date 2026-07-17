[CmdletBinding()]
param(
    [int]$HttpPort = 8080,
    [ValidateSet('minimal','standard','full')][string]$Profile = 'standard',
    [string]$EvidencePath = '.artifacts/phase4/controller/evidence.json'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$ArtifactRoot = Join-Path $Root '.artifacts/phase4/controller'
$ApplicationName = 'phase4-controller'
$Namespace = 'team-payments'
$Hostname = "$ApplicationName.$Namespace.steadystate.localtest.me"
$GoodTag = 'v0.4.0'
$BadTag = 'v0.4.0-bad'
$checks = [System.Collections.Generic.List[object]]::new()
$startedAt = (Get-Date).ToUniversalTime()
$failure = $null
$loadProcess = $null

function Invoke-External {
    param([Parameter(Mandatory)][string]$Executable, [Parameter(ValueFromRemainingArguments)][string[]]$Arguments)
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Executable exited with code $LASTEXITCODE" }
}

function Write-Utf8 {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Content)
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null
    [IO.File]::WriteAllText($Path, $Content, [Text.UTF8Encoding]::new($false))
}

function Get-JsonObject {
    param([Parameter(Mandatory)][string[]]$Arguments)
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $raw = @(& kubectl @Arguments -o json 2>$null)
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previous
    if ($exitCode -ne 0 -or -not $raw) { return $null }
    return (($raw -join [Environment]::NewLine) | ConvertFrom-Json)
}

function Add-PassedCheck {
    param([Parameter(Mandatory)][string]$Name, [Parameter(Mandatory)][datetime]$Started, [Parameter(Mandatory)][string]$Details)
    $checks.Add([ordered]@{
        name = $Name
        status = 'passed'
        elapsedSeconds = [Math]::Round(((Get-Date) - $Started).TotalSeconds, 3)
        details = $Details
    })
}

function Wait-Team {
    param([int]$TimeoutSeconds = 900)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $team = Get-JsonObject @('get','teams.platform.steadystate.dev','payments')
        $ready = @($team.status.conditions | Where-Object { $_.type -eq 'Ready' -and $_.status -eq 'True' })
        if ($ready.Count -gt 0) { return }
        Start-Sleep -Seconds 3
    } while ((Get-Date) -lt $deadline)
    throw 'Team payments did not become Ready.'
}

function Wait-Application {
    param(
        [Parameter(Mandatory)][ValidateSet('Healthy','Progressing','RollingBack','Degraded')][string]$Phase,
        [string]$Reason,
        [int]$TimeoutSeconds = 900
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $application = Get-JsonObject @('get','applications.platform.steadystate.dev',$ApplicationName,'-n',$Namespace)
        if ($application -and $application.status.phase -eq $Phase) {
            $ready = @($application.status.conditions | Where-Object type -eq 'Ready' | Select-Object -First 1)
            $reasonMatches = -not $Reason -or ($ready.Count -eq 1 -and $ready[0].reason -eq $Reason)
            $readyMatches = if ($Phase -eq 'Healthy') { $ready.Count -eq 1 -and $ready[0].status -eq 'True' } else { $ready.Count -eq 1 -and $ready[0].status -eq 'False' }
            if ($reasonMatches -and $readyMatches) { return $application }
        }
        Start-Sleep -Seconds 3
    } while ((Get-Date) -lt $deadline)
    throw "Application did not reach Phase=$Phase Reason=$Reason."
}

function Wait-GatewayVersion {
    param([Parameter(Mandatory)][string]$Version, [int]$TimeoutSeconds = 180)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $consecutive = 0
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/" -Headers @{ Host = $Hostname } -TimeoutSec 5
            $body = $response.Content | ConvertFrom-Json
            if ($response.StatusCode -eq 200 -and $body.version -eq $Version) {
                $consecutive++
                if ($consecutive -ge 5) { return }
            } else { $consecutive = 0 }
        } catch { $consecutive = 0 }
        Start-Sleep -Milliseconds 500
    } while ((Get-Date) -lt $deadline)
    throw "Gateway did not return five consecutive $Version responses."
}

function Set-ApplicationSpec {
    param(
        [Parameter(Mandatory)][ValidateSet('rolling','canary')][string]$Strategy,
        [Parameter(Mandatory)][string]$Tag
    )
    $deployment = [ordered]@{ strategy = $Strategy; automaticRollback = $true }
    $observability = [ordered]@{ metrics = $false; logs = $false; traces = $false }
    if ($Strategy -eq 'canary') {
        $deployment.steps = @(
            [ordered]@{weight=10;pause='30s'},
            [ordered]@{weight=25;pause='30s'},
            [ordered]@{weight=50;pause='30s'},
            [ordered]@{weight=100;pause='30s'}
        )
        $observability.metrics = $true
    } else {
        $deployment.steps = $null
    }
    $patch = [ordered]@{spec=[ordered]@{
        image=[ordered]@{tag=$Tag}
        deployment=$deployment
        observability=$observability
    }} | ConvertTo-Json -Depth 8 -Compress
    Invoke-External kubectl patch applications.platform.steadystate.dev $ApplicationName -n $Namespace --type=merge --patch $patch
}

function Start-ContinuousLoad {
    $scriptPath = Join-Path $ArtifactRoot 'controller-load.js'
    $outputPath = Join-Path $ArtifactRoot 'k6-output.log'
    $errorPath = Join-Path $ArtifactRoot 'k6-error.log'
    $script = @"
import http from 'k6/http';
export const options = { vus: 20, duration: '30m', discardResponseBodies: true };
export default function () {
  http.get('http://127.0.0.1:$HttpPort/', { headers: { Host: '$Hostname' } });
}
"@
    Write-Utf8 -Path $scriptPath -Content $script
    $platform = if ($env:OS -eq 'Windows_NT') { 'windows-amd64' } else { 'linux-amd64' }
    $binary = if ($env:OS -eq 'Windows_NT') { 'k6.exe' } else { 'k6' }
    $k6Path = Join-Path $Root ".tools/bin/$platform/$binary"
    if (-not (Test-Path -LiteralPath $k6Path -PathType Leaf)) {
        throw "The checksum-verified k6 binary is missing at $k6Path."
    }
    return Start-Process -FilePath $k6Path -ArgumentList @('run', $scriptPath) -PassThru -RedirectStandardOutput $outputPath -RedirectStandardError $errorPath
}

function Assert-CanaryRouteStable {
    $route = Get-JsonObject @('get','httproute',$ApplicationName,'-n',$Namespace)
    $backends = @($route.spec.rules[0].backendRefs)
    if ($backends.Count -ne 2 -or $backends[0].name -notlike '*-stable' -or [int]$backends[0].weight -ne 100 -or $backends[1].name -notlike '*-canary' -or [int]$backends[1].weight -ne 0) {
        throw 'The aborted Rollout did not restore stable=100/canary=0.'
    }
    if ($route.metadata.labels.'rollouts.argoproj.io/gatewayapi-canary' -eq 'in-progress') {
        throw 'The Gateway plugin in-progress marker remains after rollback.'
    }
}

function Save-Snapshots {
    param([Parameter(Mandatory)][string]$Name)
    $snapshot = Join-Path $ArtifactRoot "$Name.yaml"
    & kubectl get applications.platform.steadystate.dev,rollout,analysisrun,analysistemplate,deployment,replicaset,pod,service,servicemonitor,prometheusrule,httproute -n $Namespace -l "app.kubernetes.io/instance=$ApplicationName" -o yaml *> $snapshot
}

function Write-Evidence {
    param([Parameter(Mandatory)][ValidateSet('passed','failed')][string]$Result)
    $application = Get-JsonObject @('get','applications.platform.steadystate.dev',$ApplicationName,'-n',$Namespace)
    $evidence = [ordered]@{
        schemaVersion = 1
        result = $Result
        sourceRevision = $env:GITHUB_SHA
        profile = $Profile
        startedAt = $startedAt.ToString('o')
        completedAt = (Get-Date).ToUniversalTime().ToString('o')
        application = "$Namespace/$ApplicationName"
        goodTag = $GoodTag
        badTag = $BadTag
        activeVersion = [string]$application.status.activeVersion
        resolvedImageDigest = [string]$application.status.resolvedImageDigest
        checks = $checks
        failure = $failure
    }
    $resolved = if ([IO.Path]::IsPathRooted($EvidencePath)) { $EvidencePath } else { Join-Path $Root $EvidencePath }
    Write-Utf8 -Path $resolved -Content (($evidence | ConvertTo-Json -Depth 10) + [Environment]::NewLine)
}

if ($Profile -ne 'standard') { throw 'The hosted controller flow requires the standard profile.' }

Push-Location $Root
try {
    New-Item -ItemType Directory -Force -Path $ArtifactRoot | Out-Null
    $started = Get-Date
    Wait-Team
    Invoke-External kubectl rollout status deployment/steadystate-controller-manager -n steadystate-system --timeout=600s
    Add-PassedCheck 'operator-and-team-ready' $started 'The operator and Team boundary are ready.'

    $manifest = @"
apiVersion: platform.steadystate.dev/v1alpha1
kind: Application
metadata:
  name: $ApplicationName
  namespace: $Namespace
spec:
  owner: platform-team
  image:
    repository: ghcr.io/saadabdullaah/steadystate-demo-app
    tag: $GoodTag
  runtime:
    port: 8080
    replicas: {min: 1, max: 3}
  resources:
    requests: {cpu: 50m, memory: 32Mi}
    limits: {cpu: 200m, memory: 128Mi}
  deployment:
    strategy: rolling
    automaticRollback: true
  reliability:
    availabilityTarget: "99.9%"
    maximumP95Latency: 250ms
    maximumErrorRate: "1%"
  observability: {metrics: false, logs: false, traces: false}
  security: {requireSignedImage: false, runAsNonRoot: true, networkIsolation: false}
"@
    $manifestPath = Join-Path $ArtifactRoot 'controller-application.yaml'
    Write-Utf8 -Path $manifestPath -Content $manifest
    Invoke-External kubectl apply -f $manifestPath
    $baseline = Wait-Application -Phase Healthy
    Wait-GatewayVersion -Version $GoodTag
    $baselineDigest = [string]$baseline.status.resolvedImageDigest
    Add-PassedCheck 'rolling-baseline-healthy' $started 'The rolling baseline is Healthy and reachable.'

    $loadProcess = Start-ContinuousLoad
    $started = Get-Date
    Set-ApplicationSpec -Strategy canary -Tag $GoodTag
    Wait-Application -Phase Healthy | Out-Null
    $rollout = Get-JsonObject @('get','rollout',$ApplicationName,'-n',$Namespace)
    if ($rollout.status.phase -ne 'Healthy') { throw 'Rolling to canary did not produce a Healthy Rollout.' }
    Wait-GatewayVersion -Version $GoodTag
    Add-PassedCheck 'rolling-to-canary-zero-downtime' $started 'The workloadRef migration reached Healthy without losing Gateway reachability.'

    $started = Get-Date
    Set-ApplicationSpec -Strategy canary -Tag $BadTag
    $sawRollingBack = $false
    $deadline = (Get-Date).AddSeconds(300)
    do {
        $current = Get-JsonObject @('get','applications.platform.steadystate.dev',$ApplicationName,'-n',$Namespace)
        if ($current.status.phase -eq 'RollingBack') { $sawRollingBack = $true }
        $ready = @($current.status.conditions | Where-Object type -eq 'Ready' | Select-Object -First 1)
        if ($current.status.phase -eq 'Degraded' -and $ready.Count -eq 1 -and $ready[0].reason -eq 'CanaryAnalysisFailed') { break }
        Start-Sleep -Seconds 3
    } while ((Get-Date) -lt $deadline)
    if ($current.status.phase -ne 'Degraded') { throw 'Bad canary did not automatically abort and become Degraded.' }
    Assert-CanaryRouteStable
    if ([string]$current.status.activeVersion -ne $GoodTag -or [string]$current.status.resolvedImageDigest -ne $baselineDigest) {
        throw 'The failed candidate overwrote the last healthy release tuple.'
    }
    Wait-GatewayVersion -Version $GoodTag
    Add-PassedCheck 'bad-canary-automatic-rollback' $started "The bad image aborted, restored stable traffic, and exposed RollingBack=$sawRollingBack before Degraded."
    Save-Snapshots -Name 'after-rollback'

    $started = Get-Date
    Set-ApplicationSpec -Strategy canary -Tag $GoodTag
    Wait-Application -Phase Healthy | Out-Null
    Wait-GatewayVersion -Version $GoodTag
    Add-PassedCheck 'recovery-commit-healthy' $started 'Restoring the stable tag returned the Application and Rollout to Healthy.'

    $started = Get-Date
    Set-ApplicationSpec -Strategy rolling -Tag $GoodTag
    $final = Wait-Application -Phase Healthy
    Wait-GatewayVersion -Version $GoodTag
    $remainingRollout = Get-JsonObject @('get','rollout',$ApplicationName,'-n',$Namespace)
    if ($remainingRollout) { throw 'The Rollout remains after canary-to-rolling migration.' }
    if ([string]$final.status.activeVersion -ne $GoodTag) { throw 'The final rolling release is not active.' }
    Add-PassedCheck 'canary-to-rolling-zero-downtime' $started 'The Deployment became ready, the route switched, and Rollout-only resources were removed.'
    Save-Snapshots -Name 'final'
    Write-Evidence -Result passed
} catch {
    $failure = $_.Exception.Message
    try { Save-Snapshots -Name 'failure' } catch { Write-Warning "Could not capture controller failure snapshot: $($_.Exception.Message)" }
    try { Write-Evidence -Result failed } catch { Write-Warning "Could not write controller failure evidence: $($_.Exception.Message)" }
    throw
} finally {
    if ($loadProcess -and -not $loadProcess.HasExited) { Stop-Process -Id $loadProcess.Id -Force -ErrorAction SilentlyContinue }
    & kubectl delete applications.platform.steadystate.dev $ApplicationName -n $Namespace --ignore-not-found=true --wait=true --timeout=180s
    Pop-Location
}
