[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('Verify','Test')][string]$Mode,
    [int]$HttpPort = 8080,
    [string]$EvidencePath,
    [ValidateSet('minimal','standard','full')][string]$Profile = 'standard'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$ArtifactRoot = Join-Path $Root '.artifacts/phase4/foundation'
$ChartRoot = Join-Path $Root '.tools/downloads/phase4'
$RenderedRoot = Join-Path $ArtifactRoot 'rendered'

function Read-Versions {
    $values = @{}
    foreach ($line in Get-Content -LiteralPath (Join-Path $PSScriptRoot 'versions.env') -Encoding UTF8) {
        $trimmed = $line.Trim()
        if (-not $trimmed -or $trimmed.StartsWith('#')) { continue }
        $parts = $trimmed.Split('=', 2)
        if ($parts.Count -ne 2) { throw "Invalid versions.env line: $line" }
        $values[$parts[0]] = $parts[1]
    }
    return $values
}

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

function Get-VerifiedArtifact {
    param(
        [Parameter(Mandatory)][string]$Url,
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Sha256
    )
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null
    if (Test-Path -LiteralPath $Path) {
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
        if ($actual -eq $Sha256.ToLowerInvariant()) { return }
        Move-Item -LiteralPath $Path -Destination "$Path.invalid-$(Get-Date -Format 'yyyyMMddHHmmss')"
    }
    $curl = Get-Command curl.exe -ErrorAction SilentlyContinue
    if (-not $curl) { $curl = Get-Command curl -ErrorAction SilentlyContinue }
    if (-not $curl) { throw 'curl is required for checksum-verified artifact downloads.' }
    Invoke-External $curl.Source --fail --location --retry 3 --output $Path $Url
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Path).Hash.ToLowerInvariant()
    if ($actual -ne $Sha256.ToLowerInvariant()) {
        throw "Checksum mismatch for $Url. Expected $Sha256, got $actual."
    }
}

function Assert-Commands([string[]]$Names) {
    $missing = @($Names | Where-Object { -not (Get-Command $_ -ErrorAction SilentlyContinue) })
    if ($missing) { throw "Missing commands: $($missing -join ', ')" }
}

function Invoke-VerifyFoundation {
    Assert-Commands @('helm')
    $versions = Read-Versions
    $rolloutsChart = Join-Path $ChartRoot "argo-rollouts-$($versions.ARGO_ROLLOUTS_CHART_VERSION).tgz"
    $monitoringChart = Join-Path $ChartRoot "kube-prometheus-stack-$($versions.KUBE_PROMETHEUS_STACK_VERSION).tgz"
    $plugin = Join-Path $ChartRoot "gatewayapi-plugin-v$($versions.GATEWAY_API_PLUGIN_VERSION)-linux-amd64"

    Get-VerifiedArtifact `
        -Url "https://github.com/argoproj/argo-helm/releases/download/argo-rollouts-$($versions.ARGO_ROLLOUTS_CHART_VERSION)/argo-rollouts-$($versions.ARGO_ROLLOUTS_CHART_VERSION).tgz" `
        -Path $rolloutsChart `
        -Sha256 $versions.ARGO_ROLLOUTS_CHART_SHA256
    Get-VerifiedArtifact `
        -Url "https://github.com/prometheus-community/helm-charts/releases/download/kube-prometheus-stack-$($versions.KUBE_PROMETHEUS_STACK_VERSION)/kube-prometheus-stack-$($versions.KUBE_PROMETHEUS_STACK_VERSION).tgz" `
        -Path $monitoringChart `
        -Sha256 $versions.KUBE_PROMETHEUS_STACK_CHART_SHA256
    Get-VerifiedArtifact `
        -Url "https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi/releases/download/v$($versions.GATEWAY_API_PLUGIN_VERSION)/gatewayapi-plugin-linux-amd64" `
        -Path $plugin `
        -Sha256 $versions.GATEWAY_API_PLUGIN_LINUX_AMD64_SHA256

    New-Item -ItemType Directory -Force -Path $RenderedRoot | Out-Null
    $rolloutsArguments = @('template','argo-rollouts',$rolloutsChart,'--namespace','argo-rollouts','--values',(Join-Path $Root 'gitops/platform/rollouts/values.yaml'),'--include-crds')
    $monitoringArguments = @('template','monitoring',$monitoringChart,'--namespace','monitoring','--values',(Join-Path $Root 'gitops/platform/monitoring/values.yaml'),'--include-crds')
    $rolloutsFirst = (@(& helm @rolloutsArguments) -join [Environment]::NewLine) + [Environment]::NewLine
    if ($LASTEXITCODE -ne 0) { throw 'Rollouts Helm rendering failed.' }
    $rolloutsSecond = (@(& helm @rolloutsArguments) -join [Environment]::NewLine) + [Environment]::NewLine
    if ($LASTEXITCODE -ne 0 -or $rolloutsFirst -cne $rolloutsSecond) { throw 'Rollouts Helm rendering is not deterministic.' }
    $monitoringFirst = (@(& helm @monitoringArguments) -join [Environment]::NewLine) + [Environment]::NewLine
    if ($LASTEXITCODE -ne 0) { throw 'Monitoring Helm rendering failed.' }
    $monitoringSecond = (@(& helm @monitoringArguments) -join [Environment]::NewLine) + [Environment]::NewLine
    if ($LASTEXITCODE -ne 0 -or $monitoringFirst -cne $monitoringSecond) { throw 'Monitoring Helm rendering is not deterministic.' }
    Write-Utf8 -Path (Join-Path $RenderedRoot 'argo-rollouts.yaml') -Content $rolloutsFirst
    Write-Utf8 -Path (Join-Path $RenderedRoot 'monitoring.yaml') -Content $monitoringFirst

    $combined = $rolloutsFirst + $monitoringFirst
    foreach ($required in @(
        "quay.io/argoproj/argo-rollouts:v$($versions.ARGO_ROLLOUTS_VERSION)",
        'gatewayapi-plugin-linux-amd64',
        'kind: Prometheus',
        'kind: Alertmanager',
        'kind: ServiceMonitor',
        'retention: "6h"',
        'scrapeInterval: 15s',
        'evaluationInterval: 15s'
    )) {
        if (-not $combined.Contains($required)) { throw "Rendered foundation is missing required contract: $required" }
    }
    if ($combined -match 'rollouts-dashboard' -or $combined -match 'kind: DaemonSet') {
        throw 'Rendered foundation unexpectedly enables a dashboard or DaemonSet.'
    }
    Write-Host 'Progressive-delivery pins, checksums, and deterministic chart values are verified.'
}

function Wait-ArgoApplication {
    param([Parameter(Mandatory)][string]$Name, [int]$TimeoutSeconds = 900)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $previous = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $raw = @(& kubectl get application.argoproj.io $Name -n argocd -o json 2>$null)
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $previous
        if ($exitCode -eq 0 -and $raw) {
            $application = (($raw -join [Environment]::NewLine) | ConvertFrom-Json)
            if ($application.status.sync.status -eq 'Synced' -and $application.status.health.status -eq 'Healthy') { return }
        }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "Argo Application $Name did not become Synced and Healthy."
}

function Wait-RouteWeights {
    param([int]$Stable, [int]$Canary, [int]$TimeoutSeconds = 180)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $raw = @(& kubectl get httproute foundation -n steadystate-rollouts-proof -o json 2>$null)
        if ($LASTEXITCODE -eq 0 -and $raw) {
            $route = (($raw -join [Environment]::NewLine) | ConvertFrom-Json)
            $backends = @($route.spec.rules[0].backendRefs)
            if ($backends.Count -eq 2 -and [int]$backends[0].weight -eq $Stable -and [int]$backends[1].weight -eq $Canary) { return }
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw "HTTPRoute did not reach weights stable=$Stable canary=$Canary."
}

function Measure-Traffic {
    param([int]$ExpectedCanaryPercent, [int]$Samples = 500)
    $candidate = 0
    $stable = 0
    for ($index = 0; $index -lt $Samples; $index++) {
        $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/" -Headers @{ Host = 'phase4-foundation.steadystate.localtest.me' } -TimeoutSec 5
        $body = $response.Content | ConvertFrom-Json
        switch ($body.version) {
            'candidate' { $candidate++ }
            'stable' { $stable++ }
            default { throw "Unexpected compatibility response version '$($body.version)'." }
        }
    }
    $observed = 100.0 * $candidate / $Samples
    if ([Math]::Abs($observed - $ExpectedCanaryPercent) -gt 8) {
        throw "Observed canary share $observed% is outside +/-8 points of $ExpectedCanaryPercent%."
    }
    return [ordered]@{
        requestedCanaryPercent = $ExpectedCanaryPercent
        observedCanaryPercent = [Math]::Round($observed, 3)
        samples = $Samples
        stableResponses = $stable
        canaryResponses = $candidate
    }
}

function Invoke-TestFoundation {
    Assert-Commands @('kubectl','kubectl-argo-rollouts','helm')
    Invoke-VerifyFoundation
    $startedAt = (Get-Date).ToUniversalTime()
    $checks = [System.Collections.Generic.List[object]]::new()
    $measurements = [System.Collections.Generic.List[object]]::new()
    try {
        foreach ($name in @('argocd-configuration','monitoring','argo-rollouts')) {
            $started = Get-Date
            Wait-ArgoApplication -Name $name
            $checks.Add([ordered]@{name="argocd-$name-healthy";status='passed';elapsedSeconds=[Math]::Round(((Get-Date)-$started).TotalSeconds,3)})
        }

        $versions = Read-Versions
        $image = & kubectl get deployment argo-rollouts -n argo-rollouts -o "jsonpath={.spec.template.spec.containers[0].image}"
        if ($LASTEXITCODE -ne 0 -or $image -ne "quay.io/argoproj/argo-rollouts:v$($versions.ARGO_ROLLOUTS_VERSION)") {
            throw "Unexpected Rollouts controller image '$image'."
        }
        $pluginConfig = (@(& kubectl get configmap argo-rollouts-config -n argo-rollouts -o json) -join [Environment]::NewLine)
        if ($LASTEXITCODE -ne 0 -or $pluginConfig -notmatch [regex]::Escape("v$($versions.GATEWAY_API_PLUGIN_VERSION)")) {
            throw 'The pinned Gateway API plugin is not configured.'
        }
        $checks.Add([ordered]@{name='pinned-rollouts-and-gateway-plugin';status='passed';elapsedSeconds=0})

        Invoke-External kubectl apply -f (Join-Path $Root 'tests/progressive-delivery/foundation-baseline.yaml')
        Invoke-External kubectl-argo-rollouts status foundation -n steadystate-rollouts-proof --timeout 300s
        Invoke-External kubectl apply -f (Join-Path $Root 'tests/progressive-delivery/foundation-candidate.yaml')

        $started = Get-Date
        Wait-RouteWeights -Stable 90 -Canary 10
        $measurements.Add((Measure-Traffic -ExpectedCanaryPercent 10))
        $checks.Add([ordered]@{name='gateway-plugin-enforced-90-10';status='passed';elapsedSeconds=[Math]::Round(((Get-Date)-$started).TotalSeconds,3)})

        Invoke-External kubectl-argo-rollouts promote foundation -n steadystate-rollouts-proof
        $started = Get-Date
        Wait-RouteWeights -Stable 50 -Canary 50
        $measurements.Add((Measure-Traffic -ExpectedCanaryPercent 50))
        $checks.Add([ordered]@{name='envoy-traffic-followed-50-50';status='passed';elapsedSeconds=[Math]::Round(((Get-Date)-$started).TotalSeconds,3)})

        Invoke-External kubectl-argo-rollouts promote foundation -n steadystate-rollouts-proof --full
        Invoke-External kubectl-argo-rollouts status foundation -n steadystate-rollouts-proof --timeout 300s
        Wait-RouteWeights -Stable 100 -Canary 0
        $checks.Add([ordered]@{name='rollout-promoted-stable-100';status='passed';elapsedSeconds=0})

        New-Item -ItemType Directory -Force -Path (Join-Path $ArtifactRoot 'snapshots') | Out-Null
        & kubectl get rollout,replicaset,pod,service,httproute -n steadystate-rollouts-proof -o yaml *> (Join-Path $ArtifactRoot 'snapshots/foundation.yaml')
        & kubectl get prometheus,alertmanager,servicemonitor -n monitoring -o yaml *> (Join-Path $ArtifactRoot 'snapshots/monitoring.yaml')
        $evidence = [ordered]@{
            schemaVersion = 1
            result = 'passed'
            startedAt = $startedAt.ToString('o')
            completedAt = (Get-Date).ToUniversalTime().ToString('o')
            sourceRevision = $env:GITHUB_SHA
            profile = $Profile
            versions = [ordered]@{
                argoRolloutsChart = $versions.ARGO_ROLLOUTS_CHART_VERSION
                argoRolloutsController = $versions.ARGO_ROLLOUTS_VERSION
                gatewayAPIPlugin = $versions.GATEWAY_API_PLUGIN_VERSION
                kubePrometheusStack = $versions.KUBE_PROMETHEUS_STACK_VERSION
            }
            measurements = $measurements
            checks = $checks
        }
        if (-not $EvidencePath) { $EvidencePath = Join-Path $ArtifactRoot 'evidence.json' }
        Write-Utf8 -Path $EvidencePath -Content (($evidence | ConvertTo-Json -Depth 8) + [Environment]::NewLine)
        Write-Host "Hosted foundation compatibility passed. Evidence: $EvidencePath"
    } finally {
        & kubectl delete namespace steadystate-rollouts-proof --ignore-not-found=true --wait=true --timeout=180s
    }
}

Push-Location $Root
try {
    switch ($Mode) {
        'Verify' { Invoke-VerifyFoundation }
        'Test' { Invoke-TestFoundation }
    }
} finally {
    Pop-Location
}
