[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateSet('Prepare','Test','Finalize','CaptureFailure')]
    [string]$Stage,
    [int]$HttpPort = 8080
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$ArtifactRoot = Join-Path $Root '.artifacts/phase7/acceptance'
$StatePath = Join-Path $ArtifactRoot 'state.json'
$EvidencePath = Join-Path $ArtifactRoot 'evidence.json'
$Namespace = 'team-payments'
$DatabaseName = 'orders'
$ApplicationName = 'demo'
$ImageTag = 'v0.7.0'

function Write-Utf8([string]$Path, [string]$Value) {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null
    [IO.File]::WriteAllText($Path, $Value, [Text.UTF8Encoding]::new($false))
}

function Get-State {
    if (-not (Test-Path -LiteralPath $StatePath)) { throw 'Phase 7 acceptance state is missing.' }
    return Get-Content -Raw -LiteralPath $StatePath | ConvertFrom-Json
}

function Save-State($State) {
    Write-Utf8 $StatePath (($State | ConvertTo-Json -Depth 50) + [Environment]::NewLine)
}

function Add-Check($State, [string]$Name, [datetime]$Started, [string]$Details) {
    $State.checks += [pscustomobject]@{
        name = $Name
        status = 'passed'
        elapsedSeconds = [Math]::Round(((Get-Date) - $Started).TotalSeconds, 3)
        details = $Details
    }
    Save-State $State
}

function Wait-Until([int]$TimeoutSeconds, [string]$Failure, [scriptblock]$Condition) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        if (& $Condition) { return }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw $Failure
}

function Get-KubeObject([string[]]$Arguments) {
    $raw = @(& kubectl @Arguments -o json 2>$null)
    if ($LASTEXITCODE -ne 0 -or -not $raw) { return $null }
    return ($raw -join [Environment]::NewLine) | ConvertFrom-Json
}

function Get-ServiceRaw([string]$Service, [int]$Port, [string]$Path) {
    $raw = @(& kubectl --request-timeout=20s get --raw "/api/v1/namespaces/monitoring/services/http:$Service`:$Port/proxy$Path")
    if ($LASTEXITCODE -ne 0 -or -not $raw) { throw "Could not query monitoring service $Service." }
    return ($raw -join [Environment]::NewLine)
}

function Wait-ArgoHealthy([string]$Name, [int]$TimeoutSeconds = 900) {
    $application = $null
    Wait-Until $TimeoutSeconds "Argo Application $Name did not become Healthy and Synced." {
        $script:application = Get-KubeObject @('get','application.argoproj.io',$Name,'-n','argocd')
        return $script:application -and $script:application.status.health.status -eq 'Healthy' -and $script:application.status.sync.status -eq 'Synced'
    }
    return $script:application
}

function Wait-Ready([string]$Kind, [string]$Name, [string]$Namespace, [int]$TimeoutSeconds = 900) {
    $result = $null
    Wait-Until $TimeoutSeconds "$Kind $Namespace/$Name did not become current-generation Ready." {
        $arguments = @('get',$Kind,$Name)
        if ($Namespace) { $arguments += @('-n',$Namespace) }
        $script:result = Get-KubeObject $arguments
        if (-not $script:result) { return $false }
        return @($script:result.status.conditions | Where-Object {
            $_.type -eq 'Ready' -and $_.status -eq 'True' -and
            [int64]$_.observedGeneration -eq [int64]$script:result.metadata.generation
        }).Count -eq 1
    }
    return $script:result
}

function Get-CanonicalOrders {
    $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/orders" -Headers @{Host='demo.team-payments.steadystate.localtest.me'} -TimeoutSec 15
    if ($response.StatusCode -ne 200) { throw 'Orders endpoint was not reachable.' }
    $orders = @($response.Content | ConvertFrom-Json | Sort-Object id)
    $canonical = ($orders | ForEach-Object { "$($_.id)|$($_.item)|$($_.quantity)" }) -join "`n"
    $bytes = [Text.Encoding]::UTF8.GetBytes($canonical)
    $hash = [Security.Cryptography.SHA256]::Create()
    try {
        return [pscustomobject]@{
            Count = $orders.Count
            Checksum = (([BitConverter]::ToString($hash.ComputeHash($bytes))) -replace '-', '').ToLowerInvariant()
        }
    } finally {
        $hash.Dispose()
    }
}

function Get-PrimaryPod([string]$ClusterName) {
    $pod = $null
    Wait-Until 600 "Primary Pod for $ClusterName was not found." {
        $script:pod = (& kubectl get pod -n $Namespace -l "cnpg.io/cluster=$ClusterName,cnpg.io/instanceRole=primary" -o jsonpath='{.items[0].metadata.name}' 2>$null)
        return $LASTEXITCODE -eq 0 -and [bool]$script:pod
    }
    return $script:pod
}

function Wait-Backup([string]$Name, [int]$TimeoutSeconds = 900, [switch]$AllowFailure) {
    $backup = $null
    Wait-Until $TimeoutSeconds "Backup $Namespace/$Name did not finish." {
        $script:backup = Get-KubeObject @('get','backup.postgresql.cnpg.io',$Name,'-n',$Namespace)
        if (-not $script:backup) { return $false }
        $phase = [string]$script:backup.status.phase
        return $phase -eq 'completed' -or ($AllowFailure -and $phase -eq 'failed')
    }
    return $script:backup
}

function New-Backup([string]$Name, [string]$ClusterName) {
    [ordered]@{
        apiVersion='postgresql.cnpg.io/v1';kind='Backup'
        metadata=[ordered]@{name=$Name;namespace=$Namespace}
        spec=[ordered]@{
            cluster=[ordered]@{name=$ClusterName}
            method='plugin'
            pluginConfiguration=[ordered]@{name='barman-cloud.cloudnative-pg.io'}
        }
    } | ConvertTo-Json -Depth 15 | & kubectl apply -f - *> $null
    if ($LASTEXITCODE -ne 0) { throw "Could not create Backup $Name." }
}

function Wait-BackupAlert([bool]$Firing) {
    $prometheus = (& kubectl get pod -n monitoring -l app.kubernetes.io/name=prometheus -o jsonpath='{.items[0].metadata.name}' 2>$null)
    if ($LASTEXITCODE -ne 0 -or -not $prometheus) { throw 'Prometheus Pod was not found.' }
    $expected = if ($Firing) { 1 } else { 0 }
    Wait-Until 150 "Database backup-freshness alert did not reach firing=$Firing within 150 seconds." {
        $raw = @(& kubectl exec -n monitoring $prometheus -c prometheus -- wget -qO- 'http://127.0.0.1:9090/api/v1/query?query=ALERTS%7Balertname%3D%22SteadyStateDatabaseBackupStale%22%2Calertstate%3D%22firing%22%7D' 2>$null)
        if ($LASTEXITCODE -ne 0 -or -not $raw) { return $false }
        $query = ($raw -join [Environment]::NewLine) | ConvertFrom-Json
        return @($query.data.result).Count -eq $expected
    }
}

function Invoke-PrometheusScalar([string]$Expression) {
    $prometheus = (& kubectl get pod -n monitoring -l app.kubernetes.io/name=prometheus -o jsonpath='{.items[0].metadata.name}' 2>$null)
    if ($LASTEXITCODE -ne 0 -or -not $prometheus) { throw 'Prometheus Pod was not found.' }
    $encoded = [Uri]::EscapeDataString($Expression)
    $raw = @(& kubectl exec -n monitoring $prometheus -c prometheus -- wget -qO- "http://127.0.0.1:9090/api/v1/query?query=$encoded")
    if ($LASTEXITCODE -ne 0 -or -not $raw) { throw 'Prometheus resource query failed.' }
    $result = (($raw -join [Environment]::NewLine) | ConvertFrom-Json).data.result
    if (@($result).Count -ne 1) { throw "Prometheus query returned $(@($result).Count) results." }
    return [double]$result[0].value[1]
}

function Convert-MemoryToMiB([string]$Value) {
    if ($Value -notmatch '^\s*([0-9.]+)([KMG]iB)') { throw "Unsupported Docker memory value '$Value'." }
    $number = [double]$Matches[1]
    switch ($Matches[2]) {
        'KiB' { return $number / 1024 }
        'MiB' { return $number }
        'GiB' { return $number * 1024 }
    }
}

function Git-CommitAndPush([string]$Message) {
    git add -- gitops/applications/demo/application.yaml gitops/databases/orders/database.yaml gitops/databases/orders/kustomization.yaml
    git commit -m $Message
    if ($LASTEXITCODE -ne 0) { throw 'Acceptance Git commit failed.' }
    git push origin "HEAD:$((Get-State).branch)"
    if ($LASTEXITCODE -ne 0) { throw 'Acceptance Git push failed.' }
    return (& git rev-parse HEAD).Trim()
}

function Capture([string]$Prefix) {
    New-Item -ItemType Directory -Force -Path (Join-Path $ArtifactRoot 'snapshots'), (Join-Path $ArtifactRoot 'logs') | Out-Null
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & kubectl get database,application -n $Namespace -o yaml *> (Join-Path $ArtifactRoot "snapshots/$Prefix-platform.yaml")
        & kubectl get cluster.postgresql.cnpg.io,backup.postgresql.cnpg.io,scheduledbackup.postgresql.cnpg.io,objectstore.barmancloud.cnpg.io -n $Namespace -o yaml *> (Join-Path $ArtifactRoot "snapshots/$Prefix-data.yaml")
        & kubectl get application.argoproj.io -n argocd -o yaml *> (Join-Path $ArtifactRoot "snapshots/$Prefix-argo.yaml")
        & kubectl get pod,pvc,service,networkpolicy -n $Namespace -o wide *> (Join-Path $ArtifactRoot "snapshots/$Prefix-workloads.txt")
        & kubectl get prometheusrule -n $Namespace -o yaml *> (Join-Path $ArtifactRoot "snapshots/$Prefix-alert-rules.yaml")
        & kubectl logs -n steadystate-system deployment/steadystate-controller-manager --tail=1000 *> (Join-Path $ArtifactRoot "logs/$Prefix-operator.log")
        & kubectl logs -n cnpg-system deployment/cloudnative-pg --tail=1000 *> (Join-Path $ArtifactRoot "logs/$Prefix-cnpg.log")
        & kubectl logs -n cnpg-system deployment/barman-cloud-plugin-barman-cloud --tail=1000 *> (Join-Path $ArtifactRoot "logs/$Prefix-barman.log")
        & kubectl logs -n $Namespace -l "app.kubernetes.io/instance=$ApplicationName" --all-containers --tail=1000 *> (Join-Path $ArtifactRoot "logs/$Prefix-application.log")
        & kubectl logs -n argocd statefulset/argocd-application-controller --all-containers --tail=1000 *> (Join-Path $ArtifactRoot "logs/$Prefix-argo.log")
        & docker logs steadystate-seaweedfs --tail 1000 *> (Join-Path $ArtifactRoot "logs/$Prefix-seaweedfs.log")
        & (Join-Path $PSScriptRoot 'backup-store.ps1') -Action Inventory *> (Join-Path $ArtifactRoot "snapshots/$Prefix-object-inventory.txt")
    } finally {
        $ErrorActionPreference = $previous
    }
}

function Capture-RenderedState {
    $directory = Join-Path $ArtifactRoot 'rendered'
    New-Item -ItemType Directory -Force -Path $directory | Out-Null
    & kustomize build (Join-Path $Root 'gitops/databases/orders') *> (Join-Path $directory 'database.yaml')
    if ($LASTEXITCODE -ne 0) { throw 'Could not render Database GitOps state.' }
    & kustomize build (Join-Path $Root 'gitops/applications/demo') *> (Join-Path $directory 'application.yaml')
    if ($LASTEXITCODE -ne 0) { throw 'Could not render Application GitOps state.' }
    & kubectl get cluster.postgresql.cnpg.io,objectstore.barmancloud.cnpg.io,scheduledbackup.postgresql.cnpg.io,networkpolicy,servicemonitor,prometheusrule -n $Namespace -o yaml *> (Join-Path $directory 'generated-data-resources.yaml')
    if ($LASTEXITCODE -ne 0) { throw 'Could not capture generated Database resources.' }
}

switch ($Stage) {
    'Prepare' {
        if (-not $env:GH_TOKEN) { throw 'GH_TOKEN from the repository-scoped GitHub App is required.' }
        New-Item -ItemType Directory -Force -Path $ArtifactRoot | Out-Null
        $branch = "acceptance/phase7-$env:GITHUB_RUN_ID-$env:GITHUB_RUN_ATTEMPT"
        git config user.name 'steadystate-delivery[bot]'
        $botID = gh api '/users/steadystate-delivery[bot]' --jq .id
        if ($LASTEXITCODE -ne 0 -or -not $botID) { throw 'Could not resolve the GitHub App bot identity.' }
        git config user.email "$botID+steadystate-delivery[bot]@users.noreply.github.com"
        git switch --create $branch $env:GITHUB_SHA
        if ($LASTEXITCODE -ne 0) { throw 'Could not create the ephemeral acceptance branch.' }
        $manifest = Join-Path $Root 'gitops/applications/demo/application.yaml'
        $content = Get-Content -Raw $manifest
        $updated = [regex]::Replace($content, '(?m)^    tag: v[0-9]+\.[0-9]+\.[0-9]+$', "    tag: $ImageTag")
        if ($updated -eq $content -and $content -notmatch "(?m)^    tag: $([regex]::Escape($ImageTag))$") {
            throw 'Could not set the signed Phase 7 demo tag.'
        }
        Write-Utf8 $manifest $updated
        git add -- gitops/applications/demo/application.yaml
        git commit -m 'test(data): establish Phase 7 recovery baseline'
        if ($LASTEXITCODE -ne 0) { throw 'Could not commit the baseline.' }
        git push --set-upstream origin $branch
        if ($LASTEXITCODE -ne 0) { throw 'Could not push the baseline.' }
        $state = [ordered]@{
            schemaVersion=1;result='running';sourceSHA=$env:GITHUB_SHA;branch=$branch
            startedAt=(Get-Date).ToUniversalTime().ToString('o')
            baselineCommit=(& git rev-parse HEAD).Trim()
            checks=@()
        }
        Save-State $state
        Write-Host "PHASE7_ACCEPTANCE_PREPARED $branch"
    }
    'Test' {
        $state = Get-State
        $started = Get-Date
        $database = Wait-Ready 'database' $DatabaseName $Namespace
        $application = Wait-Ready 'application' $ApplicationName $Namespace
        Add-Check $state 'database-application-argo-healthy' $started 'Database and signed database-bound Application agree on current-generation readiness.'

        $started = Get-Date
        foreach ($argoApplication in @('local-path-storage','cert-manager','cloudnative-pg','barman-cloud','steadystate-operator','payments')) {
            $null = Wait-ArgoHealthy $argoApplication
        }
        $databaseStatus = $database.status | ConvertTo-Json -Depth 10
        if ($databaseStatus -match 'postgresql://' -or $databaseStatus -match 'ACCESS_SECRET_KEY') {
            throw 'Database status exposed secret material.'
        }
        $applicationPod = Get-KubeObject @('get','pod','-n',$Namespace,'-l',"app.kubernetes.io/instance=$ApplicationName")
        $applicationImages = @($applicationPod.items[0].spec.containers | ForEach-Object { [string]$_.image })
        if (@($applicationImages | Where-Object { $_ -match '^ghcr.io/saadabdullaah/steadystate-demo-app@sha256:[0-9a-f]{64}$' }).Count -ne 1) {
            throw 'The signed Application Pod was not admission-pinned to an immutable digest.'
        }
        $postgresPod = Get-KubeObject @('get','pod','-n',$Namespace,'-l',"cnpg.io/cluster=$(([string]$database.status.connectionSecretName) -replace '-app$','')")
        $postgresImages = @($postgresPod.items[0].spec.containers | ForEach-Object { [string]$_.image })
        $postgresInitImages = @($postgresPod.items[0].spec.initContainers | ForEach-Object { [string]$_.image })
        if ($postgresImages -notcontains 'ghcr.io/cloudnative-pg/postgresql:18.4-system-trixie@sha256:1e6adb18ff3d5a538ff8fcc422c47652cc3b2cc133d5c87b6fd306660f36ffe9' -or
            $postgresImages -notcontains 'ghcr.io/cloudnative-pg/plugin-barman-cloud-sidecar:v0.13.0@sha256:990361af3319f9e23aafa0f6d7981f99bf1f69b4e6a85cf1bc7d71d6f09bb288' -or
            $postgresInitImages -notcontains 'ghcr.io/cloudnative-pg/cloudnative-pg:1.30.0@sha256:a2701eb97cdd2a34b1fdb2cb51987f544b706e40bec72ae7146cd8580efefebb') {
            throw 'CNPG, bootstrap-controller, or Barman operand images are not the exact chart-pinned Phase 7 tag-plus-digest references.'
        }
        Add-Check $state 'pinned-data-stack-and-security-enforced' $started 'Pinned data controllers are Argo Healthy; Application and CNPG operand images are immutable; Database status contains no credentials.'

        $started = Get-Date
        $databaseTraceID = '77777777777777777777777777777777'
        for ($index = 1; $index -le 100; $index++) {
            $body = @{item=("order-{0:d3}" -f $index);quantity=(($index % 7) + 1)} | ConvertTo-Json -Compress
            $headers = @{Host='demo.team-payments.steadystate.localtest.me'}
            if ($index -eq 1) {
                $headers['X-Request-ID'] = 'phase7-database-trace'
                $headers.traceparent = "00-$databaseTraceID-1111111111111111-01"
            }
            $response = Invoke-WebRequest -UseBasicParsing -Method Post -Uri "http://127.0.0.1:$HttpPort/orders" -Headers $headers -ContentType 'application/json' -Body $body -TimeoutSec 15
            if ($response.StatusCode -ne 201) { throw "Order $index was not acknowledged." }
        }
        $source = Get-CanonicalOrders
        if ($source.Count -ne 100) { throw "Expected 100 source orders, found $($source.Count)." }
        Add-Check $state 'one-hundred-orders-checksummed' $started 'One hundred acknowledged orders were read in stable order and canonically checksummed.'

        $started = Get-Date
        $traceResult = $null
        Wait-Until 180 'Database operation span did not appear in Tempo.' {
            try {
                $script:traceResult = Get-ServiceRaw 'tempo' 3200 "/api/traces/$databaseTraceID"
                return $script:traceResult -match 'db.operation.name' -and $script:traceResult -match 'orders'
            } catch {
                return $false
            }
        }
        Write-Utf8 (Join-Path $ArtifactRoot 'snapshots/database-trace.json') $traceResult
        Add-Check $state 'database-span-correlated' $started 'The acknowledged order produced a Tempo database client span containing only operation and table metadata.'

        $clusterName = ([string]$database.status.connectionSecretName) -replace '-app$',''
        $pod = Get-PrimaryPod $clusterName
        & kubectl exec -n $Namespace $pod -c postgres -- psql -v ON_ERROR_STOP=1 -U postgres -d app -qAtc 'SELECT pg_switch_wal();' *> $null
        if ($LASTEXITCODE -ne 0) { throw 'WAL switch failed.' }
        $backupName = "phase7-dr-$env:GITHUB_RUN_ID-$env:GITHUB_RUN_ATTEMPT"
        New-Backup $backupName $clusterName
        $backup = Wait-Backup $backupName
        Write-Utf8 (Join-Path $ArtifactRoot 'snapshots/source-backup.json') (($backup | ConvertTo-Json -Depth 30) + [Environment]::NewLine)
        $sourceBackupServerName = [string]$database.status.backupServerName
        $allSourceObjects = @(& (Join-Path $PSScriptRoot 'backup-store.ps1') -Action Inventory)
        $sourceInventory = @($allSourceObjects | Where-Object { $_.StartsWith("$sourceBackupServerName/", [StringComparison]::Ordinal) })
        if ($LASTEXITCODE -ne 0 -or $sourceInventory.Count -eq 0) { throw 'No external source backup objects were found.' }
        $baseObjects = @($sourceInventory | Where-Object { $_ -match '(?i)/base/' })
        if ($baseObjects.Count -eq 0) { throw 'No source base-backup object was found.' }
        $walObjects = @($sourceInventory | Where-Object { $_ -match '(?i)(/wals?/|wal_)' })
        if ($walObjects.Count -eq 0) { throw 'No archived WAL object was found after the forced WAL switch.' }
        Write-Utf8 (Join-Path $ArtifactRoot 'snapshots/source-object-inventory.txt') (($sourceInventory -join [Environment]::NewLine) + [Environment]::NewLine)
        $archiveTime = (Get-Date).ToUniversalTime()
        $state.sourceBackupServerName = $sourceBackupServerName
        $state.sourceChecksum = $source.Checksum
        $state.backupName = $backupName
        $state.walObjectCount = $walObjects.Count
        $state.archiveConfirmedAt = $archiveTime.ToString('o')
        Save-State $state
        Add-Check $state 'base-backup-and-wal-archived' $started 'An on-demand plugin backup completed after a forced WAL switch.'

        $rtoStarted = (Get-Date).ToUniversalTime()
        $state.failureStartedAt = $rtoStarted.ToString('o')
        Save-State $state
        & (Join-Path $PSScriptRoot 'dev.ps1') destroy -Profile full
        if ($LASTEXITCODE -ne 0) { throw 'Whole-cluster destruction failed.' }
        Add-Check $state 'entire-kind-cluster-destroyed' $rtoStarted 'The kind cluster was destroyed while the named SeaweedFS volume remained intact.'

        $databaseManifest = Join-Path $Root 'gitops/databases/orders/database.yaml'
        $databaseContent = Get-Content -Raw $databaseManifest
        if ($databaseContent -match '(?m)^  recovery:') { throw 'Baseline Database unexpectedly contains recovery state.' }
        $databaseContent = $databaseContent.TrimEnd() + @"

  recovery:
    sourceServerName: $($state.sourceBackupServerName)
"@ + [Environment]::NewLine
        Write-Utf8 $databaseManifest $databaseContent
        $state.recoveryCommit = Git-CommitAndPush 'test(data): restore orders from retained archive'
        Save-State $state

        & (Join-Path $PSScriptRoot 'dev.ps1') bootstrap -Profile full
        if ($LASTEXITCODE -ne 0) { throw 'Full-profile rebootstrap failed.' }
        & (Join-Path $PSScriptRoot 'backup-store.ps1') -Action Start
        if ($LASTEXITCODE -ne 0) { throw 'Reconnecting the retained backup store failed.' }
        & (Join-Path $PSScriptRoot 'dev.ps1') load-images
        if ($LASTEXITCODE -ne 0) { throw 'Branch image load failed.' }
        & (Join-Path $PSScriptRoot 'dev.ps1') deploy-gitops -Profile full -GitRevision $state.branch
        if ($LASTEXITCODE -ne 0) { throw 'Recovery GitOps deployment failed.' }
        $recoveredDatabase = Wait-Ready 'database' $DatabaseName $Namespace 1200
        $recoveredApplication = Wait-Ready 'application' $ApplicationName $Namespace 600
        $recovered = Get-CanonicalOrders
        $rto = ((Get-Date).ToUniversalTime() - $rtoStarted).TotalMinutes
        if ($rto -gt 30) { throw "Recovery RTO $([Math]::Round($rto,2)) minutes exceeds 30 minutes." }
        if ($recovered.Count -ne 100 -or $recovered.Checksum -ne $source.Checksum) { throw 'Recovered orders checksum is not exact.' }
        if ($recoveredDatabase.status.backupServerName -eq $state.sourceBackupServerName) { throw 'Recovered Database reused its source archive for writes.' }
        if ($recoveredApplication.status.resolvedGitRevision -ne $state.recoveryCommit) { throw 'Application revision does not match the recovery commit.' }
        $state.recoveredChecksum = $recovered.Checksum
        $state.recoveryBackupServerName = [string]$recoveredDatabase.status.backupServerName
        $state.rtoMinutes = [Math]::Round($rto,3)
        $state.rpoMinutes = [Math]::Round(($rtoStarted - $archiveTime).TotalMinutes,3)
        Save-State $state
        Add-Check $state 'cluster-recreated-and-checksum-restored' $rtoStarted 'GitOps recreated the full cluster and restored all acknowledged orders within the RTO/RPO objectives.'

        $started = Get-Date
        $dataMiB = Invoke-PrometheusScalar 'sum(container_memory_working_set_bytes{namespace=~"cnpg-system|cert-manager|local-path-storage|team-payments",container!="",image!=""}) / 1024 / 1024'
        $totalMiB = Invoke-PrometheusScalar 'sum(container_memory_working_set_bytes{container!="",image!=""}) / 1024 / 1024'
        $seaweedStats = (& docker stats steadystate-seaweedfs --no-stream --format '{{json .}}' | ConvertFrom-Json)
        $seaweedMiB = Convert-MemoryToMiB ([string]$seaweedStats.MemUsage)
        if ($dataMiB -gt 1228.8 -or $seaweedMiB -gt 400 -or $totalMiB -gt 8192) {
            throw "Resource budget exceeded: data=$([Math]::Round($dataMiB,1))MiB seaweed=$([Math]::Round($seaweedMiB,1))MiB total=$([Math]::Round($totalMiB,1))MiB."
        }
        $state.memoryMiB = [ordered]@{data=[Math]::Round($dataMiB,3);seaweed=[Math]::Round($seaweedMiB,3);total=[Math]::Round($totalMiB,3)}
        Save-State $state
        Add-Check $state 'data-resource-budgets' $started 'Data add-ons, external SeaweedFS, and the full profile remained within declared memory budgets.'
        Capture-RenderedState
        Capture 'recovered'

        $started = Get-Date
        & (Join-Path $PSScriptRoot 'backup-store.ps1') -Action Stop
        $failedBackup = "phase7-outage-$env:GITHUB_RUN_ID-$env:GITHUB_RUN_ATTEMPT"
        New-Backup $failedBackup (([string]$recoveredDatabase.status.connectionSecretName) -replace '-app$','')
        $failed = Wait-Backup $failedBackup 300 -AllowFailure
        if ([string]$failed.status.phase -ne 'failed') { throw 'Backup-store outage did not produce a failed Backup.' }
        Wait-BackupAlert $true
        Write-Utf8 (Join-Path $ArtifactRoot 'snapshots/prometheus-backup-alert.json') (Get-ServiceRaw 'monitoring-kube-prometheus-prometheus' 9090 '/api/v1/query?query=ALERTS%7Balertname%3D%22SteadyStateDatabaseBackupStale%22%7D')
        Write-Utf8 (Join-Path $ArtifactRoot 'snapshots/alertmanager-backup-outage.json') (Get-ServiceRaw 'monitoring-kube-prometheus-alertmanager' 9093 '/api/v2/alerts')
        & (Join-Path $PSScriptRoot 'backup-store.ps1') -Action Start
        $recoveryBackup = "phase7-recovered-$env:GITHUB_RUN_ID-$env:GITHUB_RUN_ATTEMPT"
        New-Backup $recoveryBackup (([string]$recoveredDatabase.status.connectionSecretName) -replace '-app$','')
        $null = Wait-Backup $recoveryBackup
        $recoveredDatabase = Wait-Ready 'database' $DatabaseName $Namespace 300
        Wait-BackupAlert $false
        Add-Check $state 'backup-alert-fired-and-recovered' $started 'A stopped external store made backup freshness unhealthy and fired an alert; restarting it and completing a backup cleared both.'

        $started = Get-Date
        Write-Utf8 (Join-Path $Root 'gitops/databases/orders/kustomization.yaml') @"
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
"@
        $state.retirementCommit = Git-CommitAndPush 'test(data): retire recovered orders database'
        Save-State $state
        Start-Sleep -Seconds 15
        & kubectl delete database $DatabaseName -n $Namespace --wait=false *> $null
        Wait-Until 900 'Database final backup deletion did not complete.' {
            & kubectl get database $DatabaseName -n $Namespace *> $null
            return $LASTEXITCODE -ne 0
        }
        $allObjects = @(& (Join-Path $PSScriptRoot 'backup-store.ps1') -Action Inventory)
        $sourceRetainedObjects = @($allObjects | Where-Object { $_.StartsWith("$($state.sourceBackupServerName)/", [StringComparison]::Ordinal) })
        $recoveryRetainedObjects = @($allObjects | Where-Object { $_.StartsWith("$($state.recoveryBackupServerName)/", [StringComparison]::Ordinal) })
        if ($LASTEXITCODE -ne 0 -or $sourceRetainedObjects.Count -eq 0 -or $recoveryRetainedObjects.Count -eq 0) {
            throw 'Source or recovery-lifetime external objects were not retained after Database deletion.'
        }
        $objects = @($sourceRetainedObjects) + @($recoveryRetainedObjects)
        Add-Check $state 'final-backup-and-external-retention' $started 'Database deletion completed its final backup and retained external data.'

        Capture 'success'
        $state.completedAt = (Get-Date).ToUniversalTime().ToString('o')
        $state.result = 'passed'
        $state.objectCount = $objects.Count
        Save-State $state
        Write-Host 'PHASE7_ACCEPTANCE_RESULT_PASSED'
    }
    'Finalize' {
        $state = Get-State
        $checkNames = @($state.checks | ForEach-Object { [string]$_.name })
        $requiredState = @(
            'sourceSHA','branch','baselineCommit','recoveryCommit','retirementCommit',
            'sourceBackupServerName','recoveryBackupServerName','backupName',
            'archiveConfirmedAt','failureStartedAt','completedAt','memoryMiB','walObjectCount'
        )
        foreach ($field in $requiredState) {
            if (-not $state.PSObject.Properties[$field] -or $null -eq $state.$field -or [string]::IsNullOrWhiteSpace([string]$state.$field)) {
                throw "Phase 7 acceptance state is missing $field."
            }
        }
        if ($state.result -ne 'passed' -or $state.sourceChecksum -ne $state.recoveredChecksum -or
            [double]$state.rtoMinutes -le 0 -or [double]$state.rtoMinutes -gt 30 -or
            [double]$state.rpoMinutes -lt 0 -or [double]$state.rpoMinutes -gt 5 -or
            [int]$state.walObjectCount -lt 1 -or
            @($state.checks).Count -ne 10 -or @($checkNames | Sort-Object -Unique).Count -ne 10) {
            throw 'Phase 7 acceptance state is incomplete or violates RTO/RPO/checksum gates.'
        }
        $recording = Join-Path $ArtifactRoot 'phase7-disaster-recovery.gif'
        if (-not (Test-Path -LiteralPath $recording -PathType Leaf) -or (Get-Item $recording).Length -le 0) {
            throw 'Phase 7 recovery recording is missing.'
        }
        Copy-Item -LiteralPath $StatePath -Destination $EvidencePath -Force
        Write-Utf8 (Join-Path $ArtifactRoot 'rto-rpo-report.json') (([ordered]@{
            rtoMinutes=$state.rtoMinutes;rpoMinutes=$state.rpoMinutes
            sourceChecksum=$state.sourceChecksum;recoveredChecksum=$state.recoveredChecksum
        } | ConvertTo-Json) + [Environment]::NewLine)
        Write-Host 'Phase 7 acceptance evidence finalized.'
    }
    'CaptureFailure' {
        try { Capture 'failure' } catch { Write-Warning $_.Exception.Message }
        $failure = [ordered]@{
            schemaVersion=1;result='failed';sourceSHA=$env:GITHUB_SHA
            capturedAt=(Get-Date).ToUniversalTime().ToString('o')
            failure=$(if ($env:PHASE7_FAILURE_MESSAGE) { $env:PHASE7_FAILURE_MESSAGE } else { 'Phase 7 acceptance failed before completion.' })
        }
        Write-Utf8 (Join-Path $ArtifactRoot 'failure.json') (($failure | ConvertTo-Json -Depth 10) + [Environment]::NewLine)
    }
}
