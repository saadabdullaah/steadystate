[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('Prepare','Test','Finalize','CaptureFailure')][string]$Stage,
    [int]$HttpPort = 8080,
    [string]$EvidencePath = '.artifacts/phase5/acceptance/evidence.json'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$ArtifactRoot = Join-Path $Root '.artifacts/phase5/acceptance'
$StatePath = Join-Path $ArtifactRoot 'state.json'
$Namespace = 'team-payments'
$GoodImage = 'ghcr.io/saadabdullaah/steadystate-demo-app:v0.1.0'
$BadImage = 'ghcr.io/saadabdullaah/steadystate-demo-app:v0.1.0-bad'

function Write-Utf8 {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Content)
    $directory = Split-Path -Parent $Path
    if ($directory) { New-Item -ItemType Directory -Force -Path $directory | Out-Null }
    [IO.File]::WriteAllText($Path, $Content, [Text.UTF8Encoding]::new($false))
}

function Invoke-ExternalText {
    param([Parameter(Mandatory)][string]$Executable, [Parameter(ValueFromRemainingArguments)][string[]]$Arguments)
    $output = @(& $Executable @Arguments)
    if ($LASTEXITCODE -ne 0) { throw "$Executable exited with code $LASTEXITCODE" }
    return (($output -join [Environment]::NewLine).Trim())
}

function Get-KubeJSON {
    param([Parameter(Mandatory)][string[]]$Arguments, [switch]$AllowMissing)
    $previous = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
    $raw = @(& kubectl @Arguments --request-timeout=20s -o json 2>$null); $code = $LASTEXITCODE
    $ErrorActionPreference = $previous
    if ($code -ne 0 -or -not $raw) {
        if ($AllowMissing) { return $null }
        throw "kubectl $($Arguments -join ' ') failed"
    }
    return (($raw -join [Environment]::NewLine) | ConvertFrom-Json)
}

function Get-ServiceRaw {
    param([Parameter(Mandatory)][string]$Service, [Parameter(Mandatory)][int]$Port, [Parameter(Mandatory)][string]$Path)
    return Invoke-ExternalText kubectl --request-timeout=20s get --raw "/api/v1/namespaces/monitoring/services/http:$Service`:$Port/proxy$Path"
}

function Read-State {
    if (-not (Test-Path -LiteralPath $StatePath)) { throw 'Phase 5 acceptance state is missing.' }
    return Get-Content -Raw -LiteralPath $StatePath | ConvertFrom-Json -AsHashtable
}

function Save-State($State) {
    Write-Utf8 $StatePath (($State | ConvertTo-Json -Depth 30) + [Environment]::NewLine)
}

function Set-AcceptanceStage {
    param($State, [Parameter(Mandatory)][string]$Name)
    $State.currentStage = $Name
    $State.stageStartedAt = (Get-Date).ToUniversalTime().ToString('o')
    Save-State $State
    Write-Host "[STAGE] $Name at $($State.stageStartedAt)" -ForegroundColor Cyan
}

function Add-Check {
    param($State, [string]$Name, [datetime]$Started, [string]$Details)
    $State.checks = @($State.checks) + @([ordered]@{name=$Name;status='passed';elapsedSeconds=[Math]::Round(((Get-Date)-$Started).TotalSeconds,3);details=$Details})
    Save-State $State
    Write-Host "[PASS] $Name" -ForegroundColor Green
}

function Wait-Until {
    param([scriptblock]$Condition, [int]$TimeoutSeconds, [string]$Failure, [int]$IntervalSeconds = 3)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $value = & $Condition
        if ($null -ne $value -and $value -ne $false) { return $value }
        Start-Sleep -Seconds $IntervalSeconds
    } while ((Get-Date) -lt $deadline)
    throw $Failure
}

function Wait-ArgoHealthy([string]$Name, [int]$TimeoutSeconds = 900) {
    return Wait-Until -TimeoutSeconds $TimeoutSeconds -Failure "Argo Application $Name did not become Healthy/Synced." -Condition {
        $app = Get-KubeJSON @('get','applications.argoproj.io',$Name,'-n','argocd') -AllowMissing
        if ($app -and $app.status.health.status -eq 'Healthy' -and $app.status.sync.status -eq 'Synced') { return $app }
        return $null
    }
}

function New-TestApplication {
    param([string]$Name, [string]$Image, [bool]$Metrics, [bool]$Logs, [bool]$Traces)
    $tag = $Image.Substring($Image.LastIndexOf(':') + 1)
    $repository = $Image.Substring(0, $Image.LastIndexOf(':'))
    $manifest = @"
apiVersion: platform.steadystate.dev/v1alpha1
kind: Application
metadata:
  name: $Name
  namespace: $Namespace
spec:
  owner: platform-team
  image:
    repository: $repository
    tag: $tag
  runtime:
    port: 8080
    replicas: {min: 1, max: 2}
  resources:
    requests: {cpu: 25m, memory: 32Mi}
    limits: {cpu: 150m, memory: 128Mi}
  deployment:
    strategy: rolling
    automaticRollback: true
  reliability:
    availabilityTarget: "99.9%"
    maximumP95Latency: 250ms
    maximumErrorRate: "1%"
  observability:
    metrics: $($Metrics.ToString().ToLowerInvariant())
    logs: $($Logs.ToString().ToLowerInvariant())
    traces: $($Traces.ToString().ToLowerInvariant())
  security:
    requireSignedImage: false
    runAsNonRoot: true
    networkIsolation: false
"@
    $path = Join-Path $ArtifactRoot "rendered/$Name.yaml"
    Write-Utf8 $path $manifest
    $manifest | kubectl apply --request-timeout=20s -f - | Out-Host
    if ($LASTEXITCODE -ne 0) { throw "Failed to apply $Name Application." }
}

function Wait-ApplicationHealthy([string]$Name, [int]$TimeoutSeconds = 300) {
    return Wait-Until -TimeoutSeconds $TimeoutSeconds -Failure "Application $Name did not become Healthy with ServiceHealth=True." -Condition {
        $app = Get-KubeJSON @('get','applications.platform.steadystate.dev',$Name,'-n',$Namespace) -AllowMissing
        if (-not $app -or $app.status.phase -ne 'Healthy' -or [int64]$app.status.observedGeneration -ne [int64]$app.metadata.generation) { return $null }
        $ready = @($app.status.conditions | Where-Object {$_.type -eq 'Ready' -and $_.status -eq 'True'})
        $service = @($app.status.conditions | Where-Object {$_.type -eq 'ServiceHealth' -and $_.status -eq 'True'})
        if ($ready.Count -eq 1 -and $service.Count -eq 1) { return $app }
        return $null
    }
}

function Invoke-AppRequest([string]$Name, [string]$RequestID, [string]$TraceID) {
    $headers = @{Host="$Name.$Namespace.steadystate.localtest.me";'X-Request-ID'=$RequestID;traceparent="00-$TraceID-2222222222222222-01"}
    return Invoke-WebRequest -UseBasicParsing -SkipHttpErrorCheck -Uri "http://127.0.0.1:$HttpPort/" -Headers $headers -TimeoutSec 10
}

function Invoke-PrometheusQuery([string]$Expression) {
    $encoded = [uri]::EscapeDataString($Expression)
    return (Get-ServiceRaw 'monitoring-kube-prometheus-prometheus' 9090 "/api/v1/query?query=$encoded" | ConvertFrom-Json)
}

function Wait-PrometheusResult([string]$Expression, [int]$TimeoutSeconds = 180) {
    return Wait-Until -TimeoutSeconds $TimeoutSeconds -Failure "Prometheus query returned no data: $Expression" -Condition {
        $result = Invoke-PrometheusQuery $Expression
        if ($result.status -eq 'success' -and @($result.data.result).Count -gt 0) { return $result }
        return $null
    } -IntervalSeconds 5
}

function Wait-MemoryWithinBudget {
    param(
        [int]$TimeoutSeconds = 300,
        [int]$ConsecutiveSamplesRequired = 3,
        [int]$IntervalSeconds = 15
    )
    $observabilityBudgetBytes = [double]900MB
    $totalBudgetBytes = [double]6.5GB
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $consecutive = 0
    $samples = @()
    $lastDetail = 'no valid samples'
    do {
        try {
            $observability = Invoke-PrometheusQuery 'sum(container_memory_working_set_bytes{namespace="monitoring",container!="",image!=""})'
            $total = Invoke-PrometheusQuery 'sum(container_memory_working_set_bytes{container!="",image!=""})'
            if ($observability.status -ne 'success' -or @($observability.data.result).Count -ne 1 -or $total.status -ne 'success' -or @($total.data.result).Count -ne 1) {
                throw 'Prometheus returned an empty or non-scalar memory result.'
            }
            $observabilityBytes = [double]$observability.data.result[0].value[1]
            $totalBytes = [double]$total.data.result[0].value[1]
            $withinBudget = $observabilityBytes -gt 0 -and $totalBytes -gt 0 -and $observabilityBytes -le $observabilityBudgetBytes -and $totalBytes -le $totalBudgetBytes
            $sample = [ordered]@{
                observedAt = (Get-Date).ToUniversalTime().ToString('o')
                observabilityWorkingSetBytes = $observabilityBytes
                totalWorkingSetBytes = $totalBytes
                withinBudget = $withinBudget
            }
            $samples = @($samples) + @($sample)
            if ($withinBudget) { $consecutive++ } else { $consecutive = 0 }
            $lastDetail = "observability=$observabilityBytes total=$totalBytes consecutive=$consecutive/$ConsecutiveSamplesRequired"
            if ($consecutive -ge $ConsecutiveSamplesRequired) {
                $breakdownResult = Invoke-PrometheusQuery 'sum by (pod,container) (container_memory_working_set_bytes{namespace="monitoring",container!="",image!=""})'
                if ($breakdownResult.status -ne 'success' -or @($breakdownResult.data.result).Count -eq 0) {
                    throw 'Prometheus returned no per-container observability memory breakdown.'
                }
                $componentBreakdown = @($breakdownResult.data.result | ForEach-Object {
                    [ordered]@{
                        pod = [string]$_.metric.pod
                        container = [string]$_.metric.container
                        workingSetBytes = [double]$_.value[1]
                    }
                } | Sort-Object -Property workingSetBytes -Descending)
                return [ordered]@{
                    observabilityWorkingSetBytes = $observabilityBytes
                    totalWorkingSetBytes = $totalBytes
                    observedAt = $sample.observedAt
                    consecutiveSamplesRequired = $ConsecutiveSamplesRequired
                    sampleIntervalSeconds = $IntervalSeconds
                    samples = $samples
                    componentBreakdown = $componentBreakdown
                }
            }
        } catch {
            $consecutive = 0
            $lastDetail = $_.Exception.Message
        }
        if ((Get-Date) -lt $deadline) { Start-Sleep -Seconds $IntervalSeconds }
    } while ((Get-Date) -lt $deadline)
    throw "Memory did not remain within budget for $ConsecutiveSamplesRequired consecutive samples: $lastDetail"
}

function Save-ClusterEvidence {
    New-Item -ItemType Directory -Force -Path (Join-Path $ArtifactRoot 'snapshots'), (Join-Path $ArtifactRoot 'logs') | Out-Null
    foreach ($entry in @(
        @{name='applications';args=@('get','applications.platform.steadystate.dev','-n',$Namespace,'-o','yaml')},
        @{name='observability';args=@('get','pod,service,configmap,servicemonitor,prometheusrule,httproute','-A','-o','yaml')},
        @{name='argo';args=@('get','applications.argoproj.io','-n','argocd','-o','yaml')}
    )) {
        $text = @(& kubectl @($entry.args) --request-timeout=30s 2>&1) -join [Environment]::NewLine
        Write-Utf8 (Join-Path $ArtifactRoot "snapshots/$($entry.name).yaml") ($text + [Environment]::NewLine)
    }
    foreach ($entry in @(
        @{name='operator';args=@('logs','-n','steadystate-system','deployment/steadystate-controller-manager','--all-containers','--tail=1000')},
        @{name='grafana';args=@('logs','-n','monitoring','deployment/monitoring-grafana','--all-containers','--tail=1000')},
        @{name='alloy';args=@('logs','-n','monitoring','daemonset/alloy','--all-containers','--tail=1000')},
        @{name='otel-collector';args=@('logs','-n','monitoring','deployment/otel-collector','--all-containers','--tail=1000')},
        @{name='loki';args=@('logs','-n','monitoring','statefulset/loki','--all-containers','--tail=1000')},
        @{name='tempo';args=@('logs','-n','monitoring','statefulset/tempo','--all-containers','--tail=1000')}
    )) {
        $text = @(& kubectl @($entry.args) --request-timeout=30s 2>&1) -join [Environment]::NewLine
        Write-Utf8 (Join-Path $ArtifactRoot "logs/$($entry.name).log") ($text + [Environment]::NewLine)
    }
    $rendered = Invoke-ExternalText kustomize build (Join-Path $Root 'gitops/platform')
    Write-Utf8 (Join-Path $ArtifactRoot 'rendered/gitops-platform.yaml') ($rendered + [Environment]::NewLine)
}

function Remove-TestApplications {
    foreach ($name in @('telemetry','telemetry-optout','telemetry-burn')) {
        & kubectl delete application.platform.steadystate.dev $name -n $Namespace --ignore-not-found --wait=false --request-timeout=15s 2>$null | Out-Null
    }
}

switch ($Stage) {
    'Prepare' {
        New-Item -ItemType Directory -Force -Path $ArtifactRoot | Out-Null
        $state = [ordered]@{schemaVersion=1;sourceSHA=[string]$env:GITHUB_SHA;profile='standard';startedAt=(Get-Date).ToUniversalTime().ToString('o');currentStage='prepare';stageStartedAt=(Get-Date).ToUniversalTime().ToString('o');checks=@();result='running';failure=$null}
        Save-State $state
        $started = Get-Date
        foreach ($name in @('monitoring','loki','tempo','otel-collector','alloy','steadystate-operator','payments')) { $null = Wait-ArgoHealthy $name }
        Add-Check $state 'observability-foundation-healthy' $started 'Pinned Prometheus, Grafana, Loki, Tempo, OTel Collector, Alloy, operator, and Team GitOps applications are Healthy/Synced.'
    }
    'Test' {
        $state = Read-State
        try {
            Set-AcceptanceStage $state 'grafana-and-datasources'
            $started = Get-Date
            $grafana = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/api/health" -Headers @{Host='grafana.localtest.me'} -TimeoutSec 10
            if ($grafana.StatusCode -ne 200) { throw 'Grafana HTTPRoute is not healthy.' }
            foreach ($uid in @('prometheus','loki','tempo')) {
                $health = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/api/datasources/uid/$uid/health" -Headers @{Host='grafana.localtest.me'} -TimeoutSec 15
                Write-Utf8 (Join-Path $ArtifactRoot "grafana/$uid-health.json") $health.Content
                if ($health.StatusCode -ne 200) { throw "Grafana datasource $uid is not healthy." }
            }
            Add-Check $state 'grafana-route-and-datasources-healthy' $started 'Grafana route and explicit Prometheus, Loki, and Tempo datasource health endpoints returned 200.'

            Set-AcceptanceStage $state 'correlated-request'
            New-TestApplication 'telemetry' $GoodImage $true $true $true
            $application = Wait-ApplicationHealthy 'telemetry'
            $traceID = '11111111111111111111111111111111'
            $requestID = "phase5-$($env:GITHUB_RUN_ID)-$($env:GITHUB_RUN_ATTEMPT)"
            $response = Invoke-AppRequest 'telemetry' $requestID $traceID
            if ($response.StatusCode -ne 200 -or $response.Headers.'X-Request-ID' -ne $requestID) { throw 'Request identity was not preserved through the Gateway.' }
            $started = Get-Date
            $prometheus = Wait-PrometheusResult 'http_requests_total{application="telemetry",namespace="team-payments"}'
            Write-Utf8 (Join-Path $ArtifactRoot 'queries/prometheus.json') (($prometheus | ConvertTo-Json -Depth 30) + [Environment]::NewLine)
            $lokiQuery = [uri]::EscapeDataString("{namespace=`"$Namespace`",application=`"telemetry`"} |= `"$requestID`"")
            $loki = Wait-Until -TimeoutSeconds 180 -Failure 'The correlated request did not appear in Loki.' -IntervalSeconds 5 -Condition {
                $result = Get-ServiceRaw 'loki' 3100 "/loki/api/v1/query_range?query=$lokiQuery&limit=20"
                if ($result -match [regex]::Escape($requestID) -and $result -match $traceID) { return $result }
                return $null
            }
            Write-Utf8 (Join-Path $ArtifactRoot 'queries/loki.json') $loki
            $tempo = Wait-Until -TimeoutSeconds 180 -Failure 'The correlated trace did not appear in Tempo.' -IntervalSeconds 5 -Condition {
                try {
                    $result = Get-ServiceRaw 'tempo' 3200 "/api/traces/$traceID"
                    if ($result) { return $result }
                } catch {}
                return $null
            }
            Write-Utf8 (Join-Path $ArtifactRoot 'queries/tempo.json') $tempo
            $state.requestIdentity = [ordered]@{requestID=$requestID;traceID=$traceID;application='telemetry';namespace=$Namespace;observedAt=(Get-Date).ToUniversalTime().ToString('o')}
            Add-Check $state 'one-request-correlated-across-metrics-logs-traces' $started 'The same request/trace identity was observed in Prometheus, Loki, and Tempo.'

            Set-AcceptanceStage $state 'telemetry-opt-out'
            New-TestApplication 'telemetry-optout' $GoodImage $false $false $false
            $null = Wait-ApplicationHealthy 'telemetry-optout'
            $deployment = Get-KubeJSON @('get','deployment','telemetry-optout','-n',$Namespace)
            $labels = $deployment.spec.template.metadata.labels
            $environment = @($deployment.spec.template.spec.containers[0].env)
            if ($labels.'steadystate.dev/logs' -ne 'false' -or $labels.'steadystate.dev/traces' -ne 'false' -or @($environment | Where-Object {$_.name -like 'OTEL_*'}).Count -ne 0) { throw 'Telemetry opt-out workload still exports logs or traces.' }
            Add-Check $state 'log-and-trace-opt-out-enforced' (Get-Date) 'Opt-out labels are false and the workload has no OTLP exporter environment.'

            Set-AcceptanceStage $state 'metrics-opt-out'
            $patch = '{"spec":{"observability":{"metrics":false,"logs":true,"traces":true}}}'
            $patchOutput = @(& kubectl patch applications.platform.steadystate.dev telemetry -n $Namespace --request-timeout=20s --type merge -p $patch 2>&1)
            $patchExitCode = $LASTEXITCODE
            $patchOutput | Out-Host
            if ($patchExitCode -ne 0) { throw "Failed to disable telemetry metrics: $($patchOutput -join ' ')" }
            $null = Wait-Until -TimeoutSeconds 120 -Failure 'Metrics opt-out did not delete monitoring children.' -Condition {
                $monitor = Get-KubeJSON @('get','servicemonitor','telemetry-monitor','-n',$Namespace) -AllowMissing
                $rule = Get-KubeJSON @('get','prometheusrule','telemetry-alerts','-n',$Namespace) -AllowMissing
                if (-not $monitor -and -not $rule) { return $true }
                return $false
            }
            Add-Check $state 'metrics-opt-out-removes-monitoring-children' (Get-Date) 'ServiceMonitor and PrometheusRule were removed without deleting the Application.'

            Set-AcceptanceStage $state 'fast-burn-alert'
            New-TestApplication 'telemetry-burn' $BadImage $true $false $false
            $null = Wait-ApplicationHealthy 'telemetry-burn'
            $burnStarted = Get-Date
            $alert = $null
            $measuredErrorRate = 0.0
            while (-not $alert -and ((Get-Date)-$burnStarted).TotalSeconds -lt 300) {
                for ($index=0; $index -lt 50; $index++) { $null = Invoke-AppRequest 'telemetry-burn' "burn-$index" '33333333333333333333333333333333' }
                $rate = Invoke-PrometheusQuery 'steadystate:application_error_rate:5m{application="telemetry-burn",namespace="team-payments"}'
                if (@($rate.data.result).Count -gt 0) { $measuredErrorRate = [double]$rate.data.result[0].value[1] }
                if ($measuredErrorRate -ge 0.08) {
                    $query = Invoke-PrometheusQuery 'ALERTS{alertname="SteadyStateAvailabilityFastBurn",application="telemetry-burn",alertstate="firing"}'
                    if (@($query.data.result).Count -gt 0) { $alert = $query; break }
                }
                Start-Sleep -Seconds 10
            }
            if (-not $alert) { throw 'Ten-percent errors did not fire the fast-burn alert within five minutes.' }
            Write-Utf8 (Join-Path $ArtifactRoot 'alerts/prometheus-fast-burn.json') (($alert | ConvertTo-Json -Depth 30) + [Environment]::NewLine)
            $alertmanager = Get-ServiceRaw 'monitoring-kube-prometheus-alertmanager' 9093 '/api/v2/alerts'
            if ($alertmanager -notmatch 'SteadyStateAvailabilityFastBurn') { throw 'Fast-burn alert was absent from Alertmanager.' }
            Write-Utf8 (Join-Path $ArtifactRoot 'alerts/alertmanager.json') $alertmanager
            $grafanaAlert = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/api/datasources/proxy/uid/prometheus/api/v1/query?query=$([uri]::EscapeDataString('ALERTS{alertname="SteadyStateAvailabilityFastBurn",alertstate="firing"}'))" -Headers @{Host='grafana.localtest.me'} -TimeoutSec 15
            if ($grafanaAlert.Content -notmatch 'SteadyStateAvailabilityFastBurn') { throw 'Fast-burn alert was not queryable through Grafana.' }
            Write-Utf8 (Join-Path $ArtifactRoot 'alerts/grafana-fast-burn.json') $grafanaAlert.Content
            $state.burnMeasurement = [ordered]@{errorRate=$measuredErrorRate;application='telemetry-burn';observedAt=(Get-Date).ToUniversalTime().ToString('o')}
            Add-Check $state 'ten-percent-errors-fire-fast-burn' $burnStarted 'Fast-burn alert fired in Prometheus and was visible through Alertmanager and Grafana within five minutes.'

            Set-AcceptanceStage $state 'memory-budget'
            $memoryStarted = Get-Date
            $state.memory = Wait-MemoryWithinBudget
            Write-Utf8 (Join-Path $ArtifactRoot 'metrics/memory.json') (($state.memory | ConvertTo-Json -Depth 30) + [Environment]::NewLine)
            Add-Check $state 'observability-and-standard-profile-within-budget' $memoryStarted 'Three consecutive 15-second samples showed observability <=900 MiB and total in-cluster working set <=6.5 GiB; per-container evidence was retained.'

            Set-AcceptanceStage $state 'progressive-delivery-regression'
            $demo = Get-KubeJSON @('get','applications.platform.steadystate.dev','demo','-n',$Namespace)
            $payments = Get-KubeJSON @('get','applications.argoproj.io','payments','-n','argocd')
            if ($demo.status.phase -ne 'Healthy' -or $payments.status.health.status -ne 'Healthy') { throw 'Existing progressive delivery is not healthy.' }
            Add-Check $state 'progressive-delivery-regression-healthy' (Get-Date) 'Existing demo Application and tenant Argo Application remained Healthy.'

            Set-AcceptanceStage $state 'success-evidence'
            Save-ClusterEvidence
            $state.result='passed'; $state.completedAt=(Get-Date).ToUniversalTime().ToString('o'); Save-State $state
            Write-Utf8 (Join-Path $Root $EvidencePath) (($state | ConvertTo-Json -Depth 30) + [Environment]::NewLine)
            Write-Host 'PHASE5_ACCEPTANCE_RESULT_PASSED' -ForegroundColor Cyan
        } catch {
            $state.failure = "stage=$($state.currentStage): $($_.Exception.Message)"; $state.result='failed'; $state.completedAt=(Get-Date).ToUniversalTime().ToString('o'); Save-State $state
            try { Save-ClusterEvidence } catch {}
            Write-Utf8 (Join-Path $Root $EvidencePath) (($state | ConvertTo-Json -Depth 30) + [Environment]::NewLine)
            Write-Host 'PHASE5_ACCEPTANCE_RESULT_FAILED' -ForegroundColor Red
            throw
        }
    }
    'Finalize' {
        $state = Read-State
        if ($state.result -ne 'passed' -or $state.failure) { throw "Phase 5 acceptance did not pass: $($state.failure)" }
        foreach ($path in @('queries/prometheus.json','queries/loki.json','queries/tempo.json','alerts/alertmanager.json','alerts/grafana-fast-burn.json','metrics/memory.json','rendered/gitops-platform.yaml','logs/operator.log')) {
            $file = Join-Path $ArtifactRoot $path
            if (-not (Test-Path -LiteralPath $file) -or (Get-Item $file).Length -le 0) { throw "Missing Phase 5 evidence: $path" }
        }
        Write-Utf8 (Join-Path $Root $EvidencePath) (($state | ConvertTo-Json -Depth 30) + [Environment]::NewLine)
        Remove-TestApplications
    }
    'CaptureFailure' {
        $state = if (Test-Path $StatePath) { Read-State } else { [ordered]@{schemaVersion=1;sourceSHA=[string]$env:GITHUB_SHA;profile='standard';checks=@()} }
        if (-not $state.failure) { $state.failure = "stage=$($state.currentStage): $([string]$env:PHASE5_FAILURE_MESSAGE)" }
        $state.result='failed'; $state.completedAt=(Get-Date).ToUniversalTime().ToString('o'); Save-State $state
        if (-not (Test-Path -LiteralPath (Join-Path $ArtifactRoot 'logs/operator.log'))) { try { Save-ClusterEvidence } catch {} }
        Write-Utf8 (Join-Path $Root $EvidencePath) (($state | ConvertTo-Json -Depth 30) + [Environment]::NewLine)
        Remove-TestApplications
    }
}
