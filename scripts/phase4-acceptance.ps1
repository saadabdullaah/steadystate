[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('Prepare','Promote','Rollback','CaptureFailure')][string]$Stage,
    [int]$HttpPort = 8080,
    [ValidateSet('minimal','standard','full')][string]$Profile = 'standard',
    [string]$EvidencePath = '.artifacts/phase4/acceptance/evidence.json'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$ArtifactRoot = Join-Path $Root '.artifacts/phase4/acceptance'
$StatePath = Join-Path $ArtifactRoot 'state.json'
$ManifestPath = Join-Path $Root 'gitops/applications/demo/application.yaml'
$Namespace = 'team-payments'
$ApplicationName = 'demo'
$Hostname = 'demo.team-payments.steadystate.localtest.me'
$Repository = 'ghcr.io/saadabdullaah/steadystate-demo-app'
$GoodTag = 'v0.4.0'
$BadTag = 'v0.4.0-bad'
$SourceSHA = [string]$env:GITHUB_SHA
$BranchName = [string]$env:PHASE4_ACCEPTANCE_BRANCH
$AppSlug = [string]$env:PHASE4_APP_SLUG

function Write-Utf8 {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Content)
    $directory = Split-Path -Parent $Path
    if ($directory) { New-Item -ItemType Directory -Force -Path $directory | Out-Null }
    [IO.File]::WriteAllText($Path, $Content, [Text.UTF8Encoding]::new($false))
}

function Invoke-External {
    param([Parameter(Mandatory)][string]$Executable, [Parameter(ValueFromRemainingArguments)][string[]]$Arguments)
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Executable exited with code $LASTEXITCODE" }
}

function Invoke-ExternalText {
    param([Parameter(Mandatory)][string]$Executable, [Parameter(ValueFromRemainingArguments)][string[]]$Arguments)
    $output = @(& $Executable @Arguments)
    if ($LASTEXITCODE -ne 0) { throw "$Executable exited with code $LASTEXITCODE" }
    return (($output -join [Environment]::NewLine).Trim())
}

function Get-KubernetesObject {
    param([Parameter(Mandatory)][string[]]$Arguments)
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $raw = @(& kubectl @Arguments -o json 2>$null)
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previous
    if ($exitCode -ne 0 -or -not $raw) { return $null }
    return (($raw -join [Environment]::NewLine) | ConvertFrom-Json)
}

function Read-State {
    if (-not (Test-Path -LiteralPath $StatePath -PathType Leaf)) { throw "Acceptance state is missing: $StatePath" }
    return (Get-Content -Raw -LiteralPath $StatePath -Encoding UTF8 | ConvertFrom-Json -AsHashtable)
}

function Save-State {
    param([Parameter(Mandatory)]$State)
    Write-Utf8 -Path $StatePath -Content (($State | ConvertTo-Json -Depth 20) + [Environment]::NewLine)
}

function Add-Check {
    param([Parameter(Mandatory)]$State, [Parameter(Mandatory)][string]$Name, [Parameter(Mandatory)][datetime]$Started, [Parameter(Mandatory)][string]$Details)
    $State.checks = @($State.checks) + @([ordered]@{
        name = $Name
        status = 'passed'
        elapsedSeconds = [Math]::Round(((Get-Date) - $Started).TotalSeconds, 3)
        details = $Details
    })
    Save-State $State
    Write-Host "[PASS] $Name" -ForegroundColor Green
}

function Set-DemoManifest {
    param(
        [Parameter(Mandatory)][string]$Tag,
        [Parameter(Mandatory)][ValidateSet('rolling','canary')][string]$Strategy,
        [switch]$TagOnly,
        [Parameter(Mandatory)][string]$Snapshot
    )
    $steps = if ($Strategy -eq 'canary') {
@"
    steps:
      - weight: 10
        pause: 30s
      - weight: 25
        pause: 30s
      - weight: 50
        pause: 30s
      - weight: 100
        pause: 30s
"@
    } else { '' }
    if ($steps) { $steps += [Environment]::NewLine }
    $metrics = if ($Strategy -eq 'canary') { 'true' } else { 'false' }
    $content = @"
apiVersion: platform.steadystate.dev/v1alpha1
kind: Application
metadata:
  name: demo
  namespace: team-payments
  annotations:
    argocd.argoproj.io/sync-wave: "0"
spec:
  owner: platform-team
  image:
    repository: $Repository
    tag: $Tag
  runtime:
    port: 8080
    replicas:
      min: 1
      max: 3
  resources:
    requests:
      cpu: 50m
      memory: 32Mi
    limits:
      cpu: 200m
      memory: 128Mi
  deployment:
    strategy: $Strategy
$steps    automaticRollback: true
  reliability:
    availabilityTarget: "99.9%"
    maximumP95Latency: 250ms
    maximumErrorRate: "1%"
  observability:
    metrics: $metrics
    logs: false
    traces: false
  security:
    requireSignedImage: false
    runAsNonRoot: true
    networkIsolation: false
"@
    Write-Utf8 -Path $ManifestPath -Content $content
    Invoke-External kubectl kustomize (Split-Path -Parent $ManifestPath) | Out-Null
    $changed = @(git diff --name-only)
    if ($changed.Count -ne 1 -or $changed[0] -ne 'gitops/applications/demo/application.yaml') {
        throw "Acceptance changed unexpected tracked files: $($changed -join ', ')"
    }
    if ($TagOnly) {
        $diff = @(git diff --unified=0 -- gitops/applications/demo/application.yaml)
        $lines = @($diff | Where-Object { ($_ -match '^[+-]') -and ($_ -notmatch '^(---|\+\+\+)') })
        if ($lines.Count -ne 2 -or $lines[0] -notmatch '^-    tag: ' -or $lines[1] -notmatch '^\+    tag: ') {
            throw 'This delivery commit must change only spec.image.tag.'
        }
    }
    Write-Utf8 -Path (Join-Path $ArtifactRoot "rendered/$Snapshot.yaml") -Content $content
}

function New-DeliveryCommit {
    param([Parameter(Mandatory)][string]$Message)
    Invoke-External git add -- gitops/applications/demo/application.yaml | Out-Host
    Invoke-External git commit -m $Message | Out-Host
    $commit = Invoke-ExternalText git rev-parse HEAD
    Invoke-External git push origin $BranchName | Out-Host
    return $commit
}

function Test-ArgoRevision {
    param($Application, [string]$Revision)
    return ([string]$Application.status.sync.revision -eq $Revision -or $Revision -in @($Application.status.sync.revisions))
}

function Wait-ArgoApplication {
    param([Parameter(Mandatory)][string]$Name, [Parameter(Mandatory)][ValidateSet('Healthy','Degraded')][string]$Health, [string]$Revision, [int]$TimeoutSeconds = 900)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $application = Get-KubernetesObject @('get','applications.argoproj.io',$Name,'-n','argocd')
        if ($application -and $application.status.sync.status -eq 'Synced' -and
            $application.status.health.status -eq $Health -and
            (-not $Revision -or (Test-ArgoRevision $application $Revision))) { return $application }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "Argo Application $Name did not reach Synced/$Health at $Revision."
}

function Wait-Application {
    param(
        [Parameter(Mandatory)][ValidateSet('Healthy','Progressing','RollingBack','Degraded')][string]$Phase,
        [string]$Reason, [string]$Version, [string]$Revision, [string]$Digest,
        [int]$TimeoutSeconds = 900
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $application = Get-KubernetesObject @('get','applications.platform.steadystate.dev',$ApplicationName,'-n',$Namespace)
        if ($application -and [int64]$application.status.observedGeneration -eq [int64]$application.metadata.generation) {
            $ready = @($application.status.conditions | Where-Object type -eq 'Ready' | Select-Object -First 1)
            $matches = $application.status.phase -eq $Phase -and $ready.Count -eq 1
            if ($Phase -eq 'Healthy') { $matches = $matches -and $ready[0].status -eq 'True' } else { $matches = $matches -and $ready[0].status -eq 'False' }
            if ($Reason) { $matches = $matches -and $ready[0].reason -eq $Reason }
            if ($Version) { $matches = $matches -and $application.status.activeVersion -eq $Version }
            if ($Revision) { $matches = $matches -and $application.status.resolvedGitRevision -eq $Revision }
            if ($Digest) { $matches = $matches -and $application.status.resolvedImageDigest -eq $Digest }
            if ($matches) { return $application }
        }
        Start-Sleep -Seconds 3
    } while ((Get-Date) -lt $deadline)
    throw "Application did not reach Phase=$Phase Reason=$Reason Version=$Version Revision=$Revision."
}

function Wait-DesiredApplication {
    param(
        [Parameter(Mandatory)][string]$Tag,
        [Parameter(Mandatory)][ValidateSet('rolling','canary')][string]$Strategy,
        [Parameter(Mandatory)][string]$Revision,
        [int]$TimeoutSeconds = 300
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $application = Get-KubernetesObject @('get','applications.platform.steadystate.dev',$ApplicationName,'-n',$Namespace)
        if ($application -and $application.spec.image.tag -eq $Tag -and
            $application.spec.deployment.strategy -eq $Strategy -and
            $application.metadata.annotations.'steadystate.dev/source-revision' -eq $Revision) { return }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw "Argo did not apply desired tag=$Tag strategy=$Strategy revision=$Revision."
}

function Wait-GatewayVersion {
    param([Parameter(Mandatory)][string]$Version, [int]$TimeoutSeconds = 180)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $consecutive = 0
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -SkipHttpErrorCheck -Uri "http://127.0.0.1:$HttpPort/" -Headers @{Host=$Hostname} -TimeoutSec 5
            $body = $response.Content | ConvertFrom-Json
            if ($response.StatusCode -eq 200 -and $body.version -eq $Version) { $consecutive++ } else { $consecutive = 0 }
            if ($consecutive -ge 5) { return }
        } catch { $consecutive = 0 }
        Start-Sleep -Milliseconds 500
    } while ((Get-Date) -lt $deadline)
    throw "Gateway did not return five consecutive successful $Version responses."
}

function Wait-RouteWeights {
    param([Parameter(Mandatory)][int]$Stable, [Parameter(Mandatory)][int]$Canary, [int]$TimeoutSeconds = 240)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $route = Get-KubernetesObject @('get','httproute',$ApplicationName,'-n',$Namespace)
        $backends = @($route.spec.rules[0].backendRefs)
        $conditions = @($route.status.parents | ForEach-Object {$_.conditions})
        $current = @($conditions | Where-Object {
            $_.type -eq 'Accepted' -and $_.status -eq 'True' -and
            [int64]$_.observedGeneration -eq [int64]$route.metadata.generation
        }).Count -gt 0
        if ($current -and $backends.Count -eq 2 -and [int]$backends[0].weight -eq $Stable -and [int]$backends[1].weight -eq $Canary) { return }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw "HTTPRoute did not reach stable=$Stable canary=$Canary."
}

function Wait-CanaryEndpoint {
    param([int]$TimeoutSeconds = 120)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $slices = Get-KubernetesObject @('get','endpointslices.discovery.k8s.io','-n',$Namespace,'-l',"kubernetes.io/service-name=$ApplicationName-canary")
        $ready = @($slices.items.endpoints | Where-Object {$_.conditions.ready -ne $false -and $_.addresses.Count -gt 0})
        if ($ready.Count -gt 0) { return }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw 'The canary Service did not obtain a ready endpoint.'
}

function Measure-Traffic {
    param([Parameter(Mandatory)][string]$CandidateVersion, [Parameter(Mandatory)][int]$ExpectedPercent, [int]$Samples = 500)
    Wait-CanaryEndpoint
    $lastObserved = 0
    for ($attempt = 1; $attempt -le 3; $attempt++) {
        $candidate = 0; $stable = 0; $errors = 0
        for ($index = 0; $index -lt $Samples; $index++) {
            $response = Invoke-WebRequest -UseBasicParsing -SkipHttpErrorCheck -DisableKeepAlive -Uri "http://127.0.0.1:$HttpPort/" -Headers @{Host=$Hostname} -TimeoutSec 5
            $body = $response.Content | ConvertFrom-Json
            if ($body.version -eq $CandidateVersion) { $candidate++ } else { $stable++ }
            if ($response.StatusCode -notin 200..299) { $errors++ }
        }
        $lastObserved = 100.0 * $candidate / $Samples
        $withinTolerance = if ($ExpectedPercent -eq 100) {$lastObserved -ge 99} else {[Math]::Abs($lastObserved - $ExpectedPercent) -le 8}
        if ($withinTolerance) {
            return [ordered]@{requestedCanaryPercent=$ExpectedPercent;observedCanaryPercent=[Math]::Round($lastObserved,3);samples=$Samples;stableResponses=$stable;canaryResponses=$candidate;errorResponses=$errors;attempt=$attempt;observedAt=(Get-Date).ToUniversalTime().ToString('o')}
        }
        Start-Sleep -Seconds 3
    }
    throw "Observed canary share $lastObserved% did not reach the $ExpectedPercent% acceptance boundary after three samples."
}

function Measure-StableWindow {
    param([int]$Window)
    $started = Get-Date; $samples = 0
    do {
        $response = Invoke-WebRequest -UseBasicParsing -SkipHttpErrorCheck -Uri "http://127.0.0.1:$HttpPort/" -Headers @{Host=$Hostname} -TimeoutSec 5
        $body = $response.Content | ConvertFrom-Json
        if ($response.StatusCode -ne 200 -or $body.version -ne $GoodTag) { throw "Stable-only window $Window observed non-stable traffic." }
        $samples++
    } while (((Get-Date) - $started).TotalSeconds -lt 30)
    return [ordered]@{window=$Window;durationSeconds=[Math]::Round(((Get-Date)-$started).TotalSeconds,3);samples=$samples;successfulStableResponses=$samples}
}

function Start-Load {
    param([Parameter(Mandatory)][string]$Name)
    $scriptPath = Join-Path $ArtifactRoot "$Name-k6.js"
    $outputPath = Join-Path $ArtifactRoot "$Name-k6-output.log"
    $errorPath = Join-Path $ArtifactRoot "$Name-k6-error.log"
    $summaryPath = ".artifacts/phase4/acceptance/$Name-k6-summary.json"
    Write-Utf8 $scriptPath @"
import http from 'k6/http';
export const options = { vus: 20, duration: '20m', discardResponseBodies: true };
export default function () { http.get('http://127.0.0.1:$HttpPort/', { headers: { Host: '$Hostname' } }); }
export function handleSummary(data) { return { '$summaryPath': JSON.stringify(data, null, 2) }; }
"@
    $platform = if ($env:OS -eq 'Windows_NT') {'windows-amd64'} else {'linux-amd64'}
    $binary = if ($env:OS -eq 'Windows_NT') {'k6.exe'} else {'k6'}
    $k6 = Join-Path $Root ".tools/bin/$platform/$binary"
    if (-not (Test-Path -LiteralPath $k6 -PathType Leaf)) { throw "Pinned k6 is missing at $k6." }
    return Start-Process -FilePath $k6 -ArgumentList @('run',$scriptPath) -PassThru -RedirectStandardOutput $outputPath -RedirectStandardError $errorPath
}

function Stop-Load {
    param($Process)
    if ($Process -and -not $Process.HasExited) {
        Stop-Process -Id $Process.Id -ErrorAction SilentlyContinue
        $null = $Process.WaitForExit(15000)
    }
}

function Assert-K6NoFailures {
    param([Parameter(Mandatory)][string]$Name)
    $path = Join-Path $ArtifactRoot "$Name-k6-summary.json"
    $deadline = (Get-Date).AddSeconds(15)
    while (-not (Test-Path -LiteralPath $path -PathType Leaf) -and (Get-Date) -lt $deadline) { Start-Sleep -Seconds 1 }
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw "k6 did not write $Name summary evidence." }
    $summary = Get-Content -Raw -LiteralPath $path -Encoding UTF8 | ConvertFrom-Json
    $failureRate = [double]$summary.metrics.http_req_failed.values.rate
    if ($failureRate -ne 0) { throw "$Name observed an HTTP failure rate of $failureRate." }
}

function Get-RegistryMetadata {
    param([Parameter(Mandatory)][string]$Tag)
    $scope = [uri]::EscapeDataString('repository:saadabdullaah/steadystate-demo-app:pull')
    $token = (Invoke-RestMethod -Uri "https://ghcr.io/token?scope=$scope").token
    if (-not $token) { throw 'GHCR did not issue an anonymous pull token.' }
    $headers = @{Authorization="Bearer $token";Accept='application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json'}
    $uri = "https://ghcr.io/v2/saadabdullaah/steadystate-demo-app/manifests/$Tag"
    $head = Invoke-WebRequest -UseBasicParsing -Method Head -Uri $uri -Headers $headers
    $requested = [string]$head.Headers.'Docker-Content-Digest'
    $mediaType = ([string]$head.Headers.'Content-Type').Split(';')[0]
    $runtime = $requested
    if ($mediaType -match '(index|manifest.list)') {
        $index = Invoke-RestMethod -Uri $uri -Headers $headers
        $descriptor = @($index.manifests | Where-Object {$_.platform.os -eq 'linux' -and $_.platform.architecture -eq 'amd64'})
        if ($descriptor.Count -ne 1) { throw "$Tag does not expose exactly one linux/amd64 manifest." }
        $runtime = [string]$descriptor[0].digest
    }
    if ($runtime -notmatch '^sha256:[0-9a-f]{64}$') { throw "Invalid runtime digest for $Tag." }
    return [ordered]@{repository=$Repository;tag=$Tag;requestedDigest=$requested;runtimeDigest=$runtime;mediaType=$mediaType;platform='linux/amd64';anonymousPull=$true;observedAt=(Get-Date).ToUniversalTime().ToString('o')}
}

function Save-Kubectl {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string[]]$Arguments, [switch]$AllowFailure)
    $previous = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    $output = @(& kubectl @Arguments 2>&1); $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previous
    if ($exitCode -ne 0 -and -not $AllowFailure) { throw "kubectl $($Arguments -join ' ') failed." }
    Write-Utf8 $Path (($output -join [Environment]::NewLine) + [Environment]::NewLine)
}

function Save-Snapshot {
    param([Parameter(Mandatory)][string]$Name)
    Save-Kubectl (Join-Path $ArtifactRoot "snapshots/$Name.yaml") @('get','applications.platform.steadystate.dev,rollout,analysisrun,analysistemplate,deployment,replicaset,pod,service,endpoints,servicemonitor,prometheusrule,httproute','-n',$Namespace,'-o','yaml')
    Save-Kubectl (Join-Path $ArtifactRoot "snapshots/$Name-argo.json") @('get','applications.argoproj.io','-n','argocd','-o','json')
}

function Get-MonitoringServiceName {
    param([Parameter(Mandatory)][ValidateSet('alertmanager','prometheus')][string]$Component)
    # The frozen kube-prometheus-stack chart labels these Services with its
    # legacy `app` label. app.kubernetes.io/name is present on the selected
    # Pods, but not on the Service metadata queried by kubectl -l.
    $label = "app=kube-prometheus-stack-$Component"
    $services = Get-KubernetesObject @('get','service','-n','monitoring','-l',$label)
    $matches = @($services.items)
    if ($matches.Count -ne 1) { throw "Expected exactly one $Component Service with $label; found $($matches.Count)." }
    $expectedPort = if ($Component -eq 'alertmanager') { 9093 } else { 9090 }
    if (@($matches[0].spec.ports | Where-Object {[int]$_.port -eq $expectedPort}).Count -ne 1) {
        throw "$Component Service does not expose the expected port $expectedPort."
    }
    return [string]$matches[0].metadata.name
}

function Get-ServiceAPI {
    param([Parameter(Mandatory)][string]$Service, [Parameter(Mandatory)][int]$Port, [Parameter(Mandatory)][string]$Path)
    $raw = Invoke-ExternalText kubectl get --raw "/api/v1/namespaces/monitoring/services/http:$Service`:$Port/proxy$Path"
    return ($raw | ConvertFrom-Json)
}

function Wait-CandidateAlert {
    param([int]$TimeoutSeconds = 120)
    $service = Get-MonitoringServiceName alertmanager
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $alerts = @(Get-ServiceAPI $service 9093 '/api/v2/alerts')
        $match = @($alerts | Where-Object {$_.labels.alertname -eq 'SteadyStateCandidateHighErrorRate' -and $_.labels.version -eq $BadTag})
        if ($match.Count -gt 0) {
            Write-Utf8 (Join-Path $ArtifactRoot 'metrics/alertmanager.json') (($alerts | ConvertTo-Json -Depth 20) + [Environment]::NewLine)
            return
        }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw 'Alertmanager did not show the bad-candidate alert within 120 seconds.'
}

function Save-FinalEvidence {
    param([Parameter(Mandatory)]$State, [Parameter(Mandatory)][ValidateSet('passed','failed')][string]$Result, [string]$Failure)
    try {
        Save-Snapshot 'final'
        Save-Kubectl (Join-Path $ArtifactRoot 'logs/operator.log') @('logs','-n','steadystate-system','deployment/steadystate-controller-manager','--all-containers','--tail=2000') -AllowFailure
        Save-Kubectl (Join-Path $ArtifactRoot 'logs/rollouts.log') @('logs','-n','argo-rollouts','deployment/argo-rollouts','--all-containers','--tail=2000') -AllowFailure
        Save-Kubectl (Join-Path $ArtifactRoot 'logs/argocd.log') @('logs','-n','argocd','statefulset/argocd-application-controller','--all-containers','--tail=2000') -AllowFailure
        Save-Kubectl (Join-Path $ArtifactRoot 'logs/prometheus.log') @('logs','-n','monitoring','-l','app.kubernetes.io/name=prometheus','--all-containers','--tail=2000') -AllowFailure
        Save-Kubectl (Join-Path $ArtifactRoot 'metrics/analysis-runs-final.json') @('get','analysisruns','-n',$Namespace,'-o','json') -AllowFailure
        if ($State.commits.canaryToRolling) {
            $rootRender = @(& helm template steadystate-root (Join-Path $Root 'gitops/clusters/local') --namespace argocd --set-string "gitRevision=$($State.commits.canaryToRolling)")
            if ($LASTEXITCODE -ne 0) { throw 'Final GitOps root rendering failed.' }
            Write-Utf8 (Join-Path $ArtifactRoot 'rendered/root.yaml') (($rootRender -join [Environment]::NewLine) + [Environment]::NewLine)
        }
        $prometheus = Get-MonitoringServiceName prometheus
        $query = [uri]::EscapeDataString('sum(container_memory_working_set_bytes{namespace="monitoring",container!="",image!=""})')
        $memory = Get-ServiceAPI $prometheus 9090 "/api/v1/query?query=$query"
        Write-Utf8 (Join-Path $ArtifactRoot 'metrics/prometheus-working-set.json') (($memory | ConvertTo-Json -Depth 20) + [Environment]::NewLine)
        $bytes = if (@($memory.data.result).Count -eq 1) {[double]$memory.data.result[0].value[1]} else {-1}
        if ($Result -eq 'passed' -and ($bytes -lt 0 -or $bytes -gt 1.2GB)) { throw "Monitoring working set $bytes bytes exceeds the 1.2 GiB budget." }
    } catch {
        if ($Result -eq 'passed') { throw }
        Write-Warning "Failure evidence capture was partial: $($_.Exception.Message)"
        $bytes = -1
    }
    $State.monitoringWorkingSetBytes = $bytes
    $State.result = $Result; $State.failure = $Failure; $State.completedAt = (Get-Date).ToUniversalTime().ToString('o')
    Save-State $State
    $evidence = [ordered]@{
        schemaVersion=1;result=$Result;sourceSHA=$State.sourceSHA;ephemeralBranch=$State.ephemeralBranch
        profile=$State.profile;startedAt=$State.startedAt;completedAt=$State.completedAt;application=@{namespace=$Namespace;name=$ApplicationName}
        imageTags=@{baseline=$State.sourceTag;good=$GoodTag;bad=$BadTag};registry=$State.registry;commits=$State.commits
        timestamps=$State.timestamps;releaseTuples=$State.releaseTuples;activeRelease=$State.activeRelease;measurements=$State.measurements
        stableWindows=$State.stableWindows;monitoringWorkingSetBytes=$State.monitoringWorkingSetBytes;checks=$State.checks;failure=$Failure
    }
    $resolved = if ([IO.Path]::IsPathRooted($EvidencePath)) {$EvidencePath} else {Join-Path $Root $EvidencePath}
    Write-Utf8 $resolved (($evidence | ConvertTo-Json -Depth 20) + [Environment]::NewLine)
}

if ($Profile -ne 'standard') { throw 'Phase 4 acceptance requires the standard profile.' }
if ($SourceSHA -notmatch '^([0-9a-f]{40}|[0-9a-f]{64})$') { throw 'GITHUB_SHA must be a full Git object ID.' }
if ($BranchName -notmatch '^acceptance/phase4-[0-9]+-[0-9]+$') { throw 'PHASE4_ACCEPTANCE_BRANCH is invalid.' }
if (-not $env:GH_TOKEN -or -not $AppSlug) { throw 'The repository-scoped delivery App token and slug are required.' }

Push-Location $Root
try {
    New-Item -ItemType Directory -Force -Path $ArtifactRoot | Out-Null
    if ($Stage -eq 'Prepare') {
        $sourceCommit = Invoke-ExternalText git log -1 --format=%H -- apps/demo-app/VERSION
        $sourceTag = "sha-$sourceCommit"
        $state = [ordered]@{schemaVersion=1;result='running';sourceSHA=$SourceSHA;ephemeralBranch=$BranchName;profile=$Profile;startedAt=(Get-Date).ToUniversalTime().ToString('o');sourceTag=$sourceTag;commits=[ordered]@{};timestamps=[ordered]@{};registry=[ordered]@{};releaseTuples=[ordered]@{};activeRelease=[ordered]@{};measurements=@();stableWindows=@();checks=@();failure=$null}
        $botLogin = "$AppSlug[bot]"
        $botID = Invoke-ExternalText gh api "/users/$botLogin" --jq .id
        Invoke-External git config user.name $botLogin
        Invoke-External git config user.email "$botID+$botLogin@users.noreply.github.com"
        Invoke-External git switch --create $BranchName $SourceSHA
        Set-DemoManifest -Tag $sourceTag -Strategy rolling -Snapshot baseline
        Invoke-External git add -- gitops/applications/demo/application.yaml
        Invoke-External git commit -m 'test(gitops): establish Phase 4 rolling baseline'
        $state.commits.baseline = Invoke-ExternalText git rev-parse HEAD
        Invoke-External git push --set-upstream origin $BranchName
        $state.timestamps.baselinePushedAt = (Get-Date).ToUniversalTime().ToString('o'); Save-State $state
        $started = Get-Date
        & (Join-Path $Root 'scripts/dev.ps1') deploy-gitops -Profile standard -GitRevision $BranchName
        if ($LASTEXITCODE -ne 0) { throw 'GitOps deployment failed.' }
        foreach ($name in @('argocd-configuration','monitoring','argo-rollouts','steadystate-operator','payments','steadystate-root')) { Wait-ArgoApplication $name Healthy $state.commits.baseline | Out-Null }
        $application = Wait-Application Healthy -Version $sourceTag -Revision $state.commits.baseline
        Wait-GatewayVersion $sourceTag
        $state.registry.baseline = Get-RegistryMetadata $sourceTag
        $state.registry.good = Get-RegistryMetadata $GoodTag
        $state.registry.bad = Get-RegistryMetadata $BadTag
        if ($state.registry.baseline.runtimeDigest -ne $state.registry.good.runtimeDigest -or $state.registry.bad.runtimeDigest -eq $state.registry.good.runtimeDigest) { throw 'Good/source-SHA or good/bad digest immutability proof failed.' }
        $state.activeRelease = [ordered]@{version=$sourceTag;digest=[string]$application.status.resolvedImageDigest;revision=$state.commits.baseline}
        $state.releaseTuples.baseline = $state.activeRelease
        Write-Utf8 (Join-Path $ArtifactRoot 'registry.json') (($state.registry | ConvertTo-Json -Depth 10) + [Environment]::NewLine)
        Add-Check $state 'baseline-rolling-reachable' $started 'The public source-SHA image is Healthy through the Gateway under rolling strategy.'
        Save-Snapshot 'baseline'
        Write-Host 'PHASE4_PREPARE_RESULT_PASSED' -ForegroundColor Cyan
    } elseif ($Stage -eq 'Promote') {
        $state = Read-State; $load = Start-Load 'promotion'; $stageStarted = Get-Date
        try {
            Set-DemoManifest -Tag $state.sourceTag -Strategy canary -Snapshot rolling-to-canary
            $state.commits.rollingToCanary = New-DeliveryCommit 'test(gitops): migrate Phase 4 baseline to canary'
            $state.timestamps.rollingToCanaryPushedAt = (Get-Date).ToUniversalTime().ToString('o'); Save-State $state
            Wait-Application Healthy -Version $state.sourceTag -Revision $state.commits.rollingToCanary | Out-Null
            Wait-GatewayVersion $state.sourceTag
            Add-Check $state 'rolling-to-canary-zero-downtime' $stageStarted 'Git-driven migration completed without a Gateway outage.'
            $promotionStarted = Get-Date
            Set-DemoManifest -Tag $GoodTag -Strategy canary -TagOnly -Snapshot good-candidate
            $state.commits.promotion = New-DeliveryCommit 'test(gitops): promote Phase 4 good candidate'
            $state.timestamps.promotionPushedAt = (Get-Date).ToUniversalTime().ToString('o'); Save-State $state
            Wait-DesiredApplication $GoodTag canary $state.commits.promotion
            foreach ($weight in @(10,25,50,100)) {
                Wait-RouteWeights (100-$weight) $weight
                $state.measurements = @($state.measurements) + @((Measure-Traffic $GoodTag $weight))
                Save-State $state
            }
            $application = Wait-Application Healthy -Version $GoodTag -Revision $state.commits.promotion -Digest $state.registry.good.runtimeDigest -TimeoutSeconds 720
            Wait-ArgoApplication payments Healthy $state.commits.promotion | Out-Null
            Wait-GatewayVersion $GoodTag
            if (((Get-Date)-$promotionStarted).TotalMinutes -gt 12) { throw 'Good rollout exceeded 12 minutes.' }
            $state.activeRelease = [ordered]@{version=$GoodTag;digest=[string]$application.status.resolvedImageDigest;revision=$state.commits.promotion}
            $state.releaseTuples.promotion = $state.activeRelease
            $state.timestamps.promotedAt = (Get-Date).ToUniversalTime().ToString('o')
            Add-Check $state 'good-canary-promoted-automatically' $promotionStarted 'Argo observed the commit and Rollouts promoted 10/25/50/100 within 12 minutes.'
            Add-Check $state 'runtime-digest-and-revision-match-promotion' $promotionStarted 'The active digest matches anonymous GHCR metadata and the active revision matches the Git commit.'
            Stop-Load $load; $load = $null
            Assert-K6NoFailures 'promotion'
            Add-Check $state 'promotion-path-no-routing-outage' $stageStarted 'Continuous k6 traffic observed no failed requests during migration and promotion.'
            Save-Snapshot 'after-promotion'
            Write-Host 'PHASE4_PROMOTION_RESULT_PASSED' -ForegroundColor Cyan
        } catch {
            $state.failure = $_.Exception.Message
            Save-State $state
            Write-Host 'PHASE4_PROMOTION_RESULT_FAILED' -ForegroundColor Red
            throw
        } finally { Stop-Load $load }
    } elseif ($Stage -eq 'Rollback') {
        $state = Read-State; $load = Start-Load 'rollback'; $failure = $null
        try {
            $failedTuple = @{} + $state.activeRelease
            Set-DemoManifest -Tag $BadTag -Strategy canary -TagOnly -Snapshot bad-candidate
            $state.commits.rejection = New-DeliveryCommit 'test(gitops): deliver Phase 4 failing candidate'
            $state.timestamps.rejectionPushedAt = (Get-Date).ToUniversalTime().ToString('o'); Save-State $state
            Wait-DesiredApplication $BadTag canary $state.commits.rejection
            Wait-RouteWeights 90 10
            $reachedTen = Get-Date; $state.timestamps.badCandidateReachedTenAt = $reachedTen.ToUniversalTime().ToString('o')
            $state.measurements = @($state.measurements) + @((Measure-Traffic $BadTag 10)); Save-State $state
            $alertStarted = Get-Date; Wait-CandidateAlert
            Add-Check $state 'candidate-alert-visible-in-alertmanager' $alertStarted 'Alertmanager exposed the high-error candidate alert within 120 seconds.'
            $application = Wait-Application Degraded -Reason CanaryAnalysisFailed -Version $failedTuple.version -Revision $failedTuple.revision -Digest $failedTuple.digest -TimeoutSeconds 180
            $state.releaseTuples.failedCandidate = [ordered]@{version=[string]$application.status.activeVersion;digest=[string]$application.status.resolvedImageDigest;revision=[string]$application.status.resolvedGitRevision}
            if (((Get-Date)-$reachedTen).TotalSeconds -gt 180) { throw 'Bad rollout did not abort within 180 seconds of reaching 10%.' }
            Wait-RouteWeights 100 0
            if ((Get-KubernetesObject @('get','httproute',$ApplicationName,'-n',$Namespace)).metadata.labels.'rollouts.argoproj.io/gatewayapi-canary' -eq 'in-progress') { throw 'The route still has the in-progress marker after abort.' }
            Add-Check $state 'bad-canary-aborted-and-preserved-active-tuple' $reachedTen 'The first failed analysis restored stable traffic without overwriting the healthy tuple.'
            for ($window=1; $window -le 3; $window++) { $state.stableWindows = @($state.stableWindows) + @((Measure-StableWindow $window)); Save-State $state }
            Add-Check $state 'three-stable-only-windows-after-abort' $reachedTen 'Three consecutive 30-second windows served only successful stable traffic.'
            Save-Snapshot 'after-rollback'
            Save-Kubectl (Join-Path $ArtifactRoot 'metrics/analysis-runs.json') @('get','analysisruns','-n',$Namespace,'-o','json')
            $recoveryStarted = Get-Date
            Set-DemoManifest -Tag $GoodTag -Strategy canary -TagOnly -Snapshot recovery
            $state.commits.recovery = New-DeliveryCommit 'test(gitops): recover Phase 4 stable release'
            $state.timestamps.recoveryPushedAt = (Get-Date).ToUniversalTime().ToString('o'); Save-State $state
            Wait-DesiredApplication $GoodTag canary $state.commits.recovery
            $application = Wait-Application Healthy -Version $GoodTag -Revision $state.commits.recovery -Digest $state.registry.good.runtimeDigest
            Wait-ArgoApplication payments Healthy $state.commits.recovery | Out-Null
            Wait-GatewayVersion $GoodTag
            $state.activeRelease = [ordered]@{version=$GoodTag;digest=[string]$application.status.resolvedImageDigest;revision=$state.commits.recovery}
            $state.releaseTuples.recovery = $state.activeRelease
            Add-Check $state 'recovery-commit-restored-healthy' $recoveryStarted 'A Git recovery commit restored Kubernetes and Argo health and advanced the active revision.'
            Stop-Load $load
            $load = Start-Load 'final-migration'
            $deliveryStarted = Get-Date
            Set-DemoManifest -Tag $GoodTag -Strategy rolling -Snapshot canary-to-rolling
            $state.commits.canaryToRolling = New-DeliveryCommit 'test(gitops): return Phase 4 application to rolling'
            $state.timestamps.canaryToRollingPushedAt = (Get-Date).ToUniversalTime().ToString('o'); Save-State $state
            # The root intentionally advances and health-gates every platform
            # child before the wave-0 tenant Application. Alert recovery can
            # therefore make Git propagation longer than the workload cutover
            # itself. Bound and record both intervals independently.
            Wait-DesiredApplication $GoodTag rolling $state.commits.canaryToRolling -TimeoutSeconds 600
            $state.timestamps.canaryToRollingAppliedAt = (Get-Date).ToUniversalTime().ToString('o')
            Add-Check $state 'canary-to-rolling-git-detected' $deliveryStarted 'Argo propagated the exact recovery-followup commit through every health-gated platform wave.'
            $migrationStarted = Get-Date
            $application = Wait-Application Healthy -Version $GoodTag -Revision $state.commits.canaryToRolling -Digest $state.registry.good.runtimeDigest -TimeoutSeconds 300
            Wait-GatewayVersion $GoodTag
            if (Get-KubernetesObject @('get','rollout',$ApplicationName,'-n',$Namespace)) { throw 'Rollout remains after canary-to-rolling migration.' }
            if (((Get-Date)-$migrationStarted).TotalMinutes -gt 5) { throw 'Canary-to-rolling migration exceeded five minutes.' }
            Stop-Load $load; $load = $null
            Assert-K6NoFailures 'final-migration'
            $state.activeRelease = [ordered]@{version=$GoodTag;digest=[string]$application.status.resolvedImageDigest;revision=$state.commits.canaryToRolling}
            $state.releaseTuples.final = $state.activeRelease
            Add-Check $state 'canary-to-rolling-zero-downtime' $migrationStarted 'Git-driven migration returned to Deployment ownership within five minutes without an outage.'
            Add-Check $state 'argo-and-application-health-agree' $recoveryStarted 'Argo health followed Degraded rollback, Healthy recovery, and Healthy final migration.'
            Save-FinalEvidence $state passed
            Write-Host 'PHASE4_ROLLBACK_RESULT_PASSED' -ForegroundColor Cyan
        } catch {
            $failure = $_.Exception.Message
            try { Save-FinalEvidence $state failed $failure } catch { Write-Warning "Could not complete failure evidence: $($_.Exception.Message)" }
            Write-Host 'PHASE4_ROLLBACK_RESULT_FAILED' -ForegroundColor Red
            throw
        } finally { Stop-Load $load }
    } else {
        $state = Read-State
        $message = if ($state.failure) {
            [string]$state.failure
        } elseif ($env:PHASE4_FAILURE_MESSAGE) {
            [string]$env:PHASE4_FAILURE_MESSAGE
        } else {
            'A hosted Phase 4 acceptance stage failed.'
        }
        Save-FinalEvidence $state failed $message
        Write-Host 'PHASE4_FAILURE_EVIDENCE_CAPTURED' -ForegroundColor Yellow
    }
} finally {
    Pop-Location
}
