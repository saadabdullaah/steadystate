[CmdletBinding()]
param(
    [ValidateSet('Test','CaptureFailure')]
    [string]$Stage = 'Test',
    [string]$Namespace = 'team-payments',
    [string]$DatabaseName = 'compat',
    [string]$ArtifactDirectory
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
if (-not $ArtifactDirectory) {
    $ArtifactDirectory = Join-Path $Root '.artifacts/phase7/foundation'
}
New-Item -ItemType Directory -Force -Path $ArtifactDirectory | Out-Null

function Invoke-Kubectl {
    param([Parameter(ValueFromRemainingArguments)][string[]]$Arguments)
    & kubectl @Arguments
    if ($LASTEXITCODE -ne 0) { throw "kubectl exited with code $LASTEXITCODE" }
}

function Write-JsonFile([string]$Name, [object]$Value) {
    $path = Join-Path $ArtifactDirectory $Name
    [IO.File]::WriteAllText($path, ($Value | ConvertTo-Json -Depth 30), [Text.UTF8Encoding]::new($false))
}

function Add-Check([System.Collections.Generic.List[object]]$Checks, [string]$Name, [datetime]$Started, [string]$Details) {
    $Checks.Add([ordered]@{
        name = $Name
        status = 'passed'
        elapsedSeconds = [Math]::Round(((Get-Date) - $Started).TotalSeconds, 3)
        details = $Details
    })
}

function Wait-ArgoApplicationsHealthy([string[]]$Names, [int]$TimeoutSeconds = 600) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $pending = [System.Collections.Generic.List[string]]::new()
        foreach ($name in $Names) {
            $raw = @(& kubectl get applications.argoproj.io $name -n argocd -o json 2>$null)
            if ($LASTEXITCODE -ne 0 -or -not $raw) {
                $pending.Add($name)
                continue
            }
            $application = ($raw -join [Environment]::NewLine) | ConvertFrom-Json
            if ($application.status.sync.status -ne 'Synced' -or
                $application.status.health.status -ne 'Healthy') {
                $pending.Add($name)
            }
        }
        if ($pending.Count -eq 0) {
            return
        }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "Argo Applications did not appear and become Synced/Healthy within $TimeoutSeconds seconds: $($pending -join ', ')."
}

function New-DatabaseDocument([string]$RecoverySource = '') {
    $metadata = [ordered]@{name=$DatabaseName;namespace=$Namespace}
    if ($env:GITHUB_SHA -match '^([0-9a-f]{40}|[0-9a-f]{64})$') {
        $metadata.annotations = [ordered]@{'steadystate.dev/source-revision'=$env:GITHUB_SHA}
    }
    $spec = [ordered]@{
        engine = 'postgres'
        instances = 1
        storage = [ordered]@{size='1Gi';storageClassName='local-path'}
        backups = [ordered]@{enabled=$true;schedule='0 0 2 * * *';retention='7d'}
    }
    if ($RecoverySource) {
        $spec.recovery = [ordered]@{sourceServerName=$RecoverySource}
    }
    return [ordered]@{
        apiVersion = 'platform.steadystate.dev/v1alpha1'
        kind = 'Database'
        metadata = $metadata
        spec = $spec
    }
}

function Apply-Document([object]$Document) {
    $Document | ConvertTo-Json -Depth 20 | & kubectl apply -f -
    if ($LASTEXITCODE -ne 0) { throw 'Applying the compatibility Database failed.' }
}

function Wait-DatabaseHealthy([int]$TimeoutSeconds = 900) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $raw = @(& kubectl get database $DatabaseName -n $Namespace -o json 2>$null)
        if ($LASTEXITCODE -eq 0 -and $raw) {
            $database = ($raw -join [Environment]::NewLine) | ConvertFrom-Json
            $ready = @($database.status.conditions | Where-Object {
                $_.type -eq 'Ready' -and $_.status -eq 'True' -and
                [int64]$_.observedGeneration -eq [int64]$database.metadata.generation
            })
            if ($database.status.phase -eq 'Healthy' -and $ready.Count -eq 1) {
                return $database
            }
        }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "Database $Namespace/$DatabaseName did not become Healthy within $TimeoutSeconds seconds."
}

function Wait-DatabaseAbsent([int]$TimeoutSeconds = 900) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        & kubectl get database $DatabaseName -n $Namespace *> $null
        if ($LASTEXITCODE -ne 0) { return }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "Database $Namespace/$DatabaseName did not finalize within $TimeoutSeconds seconds."
}

function Get-PrimaryPod([string]$ClusterName, [int]$TimeoutSeconds = 600) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $pod = (& kubectl get pod -n $Namespace -l "cnpg.io/cluster=$ClusterName,cnpg.io/instanceRole=primary" -o jsonpath='{.items[0].metadata.name}' 2>$null)
        if ($LASTEXITCODE -eq 0 -and $pod) { return $pod }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "Primary Pod for $ClusterName was not found."
}

function Invoke-Psql([string]$ClusterName, [string]$SQL) {
    $pod = Get-PrimaryPod $ClusterName
    $output = @(& kubectl exec -n $Namespace $pod -c postgres -- psql -v ON_ERROR_STOP=1 -U postgres -d app -qAtc "SET ROLE app; $SQL")
    if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL compatibility command failed.' }
    return ($output -join "`n").Trim()
}

function Invoke-AdminPsql([string]$ClusterName, [string]$SQL) {
    $pod = Get-PrimaryPod $ClusterName
    $output = @(& kubectl exec -n $Namespace $pod -c postgres -- psql -v ON_ERROR_STOP=1 -U postgres -d app -qAtc $SQL)
    if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL administrative compatibility command failed.' }
    return ($output -join "`n").Trim()
}

function Get-DataChecksum([string]$ClusterName) {
    $canonical = Invoke-Psql $ClusterName "SELECT id || '|' || item || '|' || quantity FROM phase7_orders ORDER BY id;"
    $bytes = [Text.Encoding]::UTF8.GetBytes($canonical)
    $hash = [Security.Cryptography.SHA256]::Create()
    try {
        return ([BitConverter]::ToString($hash.ComputeHash($bytes)) -replace '-', '').ToLowerInvariant()
    } finally {
        $hash.Dispose()
    }
}

function Capture-Snapshots([string]$Prefix) {
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & docker ps -a --filter 'label=io.x-k8s.kind.cluster=steadystate' --format '{{.Names}} {{.Status}}' *> (Join-Path $ArtifactDirectory "$Prefix-kind-containers.txt")
        $kindContainerIDs = @(& docker ps -aq --filter 'label=io.x-k8s.kind.cluster=steadystate')
        if ($kindContainerIDs.Count -gt 0) {
            & docker inspect --format '{{.Name}} status={{.State.Status}} oomKilled={{.State.OOMKilled}} exitCode={{.State.ExitCode}}' @kindContainerIDs *> (Join-Path $ArtifactDirectory "$Prefix-kind-container-state.txt")
            & docker stats --no-stream --format '{{.Name}} {{.MemUsage}} {{.CPUPerc}}' @kindContainerIDs *> (Join-Path $ArtifactDirectory "$Prefix-kind-container-resources.txt")
        } else {
            'No kind containers were discoverable.' | Set-Content -LiteralPath (Join-Path $ArtifactDirectory "$Prefix-kind-container-state.txt") -Encoding UTF8
            'No kind container resource measurements were available.' | Set-Content -LiteralPath (Join-Path $ArtifactDirectory "$Prefix-kind-container-resources.txt") -Encoding UTF8
        }
        & docker system df *> (Join-Path $ArtifactDirectory "$Prefix-docker-disk.txt")
        & kubectl --request-timeout=10s get database,cluster.postgresql.cnpg.io,backup.postgresql.cnpg.io,scheduledbackup.postgresql.cnpg.io,objectstore.barmancloud.cnpg.io -n $Namespace -o yaml *> (Join-Path $ArtifactDirectory "$Prefix-data-resources.yaml")
        & kubectl --request-timeout=10s get pod,pvc,service,networkpolicy -n $Namespace -o wide *> (Join-Path $ArtifactDirectory "$Prefix-workloads.txt")
        & kubectl --request-timeout=10s get pod -n $Namespace -l 'cnpg.io/jobRole' -o yaml *> (Join-Path $ArtifactDirectory "$Prefix-cnpg-job-pods.yaml")
        $jobPodLogs = [System.Collections.Generic.List[string]]::new()
        $jobPods = @(& kubectl --request-timeout=10s get pod -n $Namespace -l 'cnpg.io/jobRole' -o name)
        foreach ($jobPod in $jobPods) {
            $jobPodLogs.Add("===== $jobPod =====")
            foreach ($line in @(& kubectl --request-timeout=10s logs -n $Namespace $jobPod --all-containers --prefix=true 2>&1)) {
                $jobPodLogs.Add([string]$line)
            }
        }
        if ($jobPodLogs.Count -eq 0) { $jobPodLogs.Add('No CNPG job Pod logs were available.') }
        [IO.File]::WriteAllLines((Join-Path $ArtifactDirectory "$Prefix-cnpg-job-pods.log"), $jobPodLogs, [Text.UTF8Encoding]::new($false))
        & kubectl --request-timeout=10s logs -n steadystate-system deployment/steadystate-controller-manager --all-containers --tail=1000 *> (Join-Path $ArtifactDirectory "$Prefix-operator.log")
        & kubectl --request-timeout=10s logs -n cnpg-system deployment/cloudnative-pg --all-containers --tail=1000 *> (Join-Path $ArtifactDirectory "$Prefix-cnpg.log")
        & kubectl --request-timeout=10s logs -n cnpg-system deployment/barman-cloud-plugin-barman-cloud --all-containers --tail=1000 *> (Join-Path $ArtifactDirectory "$Prefix-barman.log")
        & docker logs steadystate-seaweedfs --tail 1000 *> (Join-Path $ArtifactDirectory "$Prefix-seaweedfs.log")
        & (Join-Path $PSScriptRoot 'backup-store.ps1') -Action Inventory *> (Join-Path $ArtifactDirectory "$Prefix-object-inventory.txt")
    } finally {
        $ErrorActionPreference = $previous
    }
}

if ($Stage -eq 'CaptureFailure') {
    Capture-Snapshots 'failure'
    exit 0
}

$startedAt = (Get-Date).ToUniversalTime()
$checks = [System.Collections.Generic.List[object]]::new()

$started = Get-Date
& (Join-Path $PSScriptRoot 'backup-store.ps1') -Action Verify
Add-Check $checks 'seaweedfs-exact-store-healthy' $started 'The exact pinned SeaweedFS store is healthy and bound only to loopback on the host.'

$started = Get-Date
Wait-ArgoApplicationsHealthy @('local-path-storage','cert-manager','cloudnative-pg','barman-cloud','steadystate-operator')
Add-Check $checks 'pinned-data-stack-ready' $started 'StorageClass, cert-manager, CloudNativePG, Barman, and the SteadyState operator are Healthy.'

Invoke-Kubectl delete database $DatabaseName -n $Namespace --ignore-not-found=true --wait=false
Wait-DatabaseAbsent 120

$started = Get-Date
Apply-Document (New-DatabaseDocument)
$database = Wait-DatabaseHealthy
$clusterName = [string]$database.status.connectionSecretName
$clusterName = $clusterName.Substring(0, $clusterName.Length - 4)
Add-Check $checks 'database-provisioned' $started 'A one-instance PostgreSQL Database became current-generation Healthy.'

$started = Get-Date
$null = Invoke-Psql $clusterName 'CREATE TABLE IF NOT EXISTS phase7_orders (id integer PRIMARY KEY, item text NOT NULL, quantity integer NOT NULL); TRUNCATE phase7_orders; INSERT INTO phase7_orders SELECT value, ''order-'' || lpad(value::text, 3, ''0''), (value % 7) + 1 FROM generate_series(1, 100) AS value;'
$null = Invoke-AdminPsql $clusterName 'SELECT pg_switch_wal();'
$sourceChecksum = Get-DataChecksum $clusterName
if ($sourceChecksum -notmatch '^[0-9a-f]{64}$') { throw 'The source checksum is not canonical SHA-256.' }
Add-Check $checks 'data-seeded-and-wal-switched' $started 'One hundred canonical rows were committed and a WAL switch was requested.'

$started = Get-Date
$backupServerName = [string]$database.status.backupServerName
Invoke-Kubectl delete database $DatabaseName -n $Namespace --wait=false
Wait-DatabaseAbsent
Add-Check $checks 'final-backup-completed' $started 'Database finalization completed only after its deterministic final Barman backup.'

$inventory = @(& (Join-Path $PSScriptRoot 'backup-store.ps1') -Action Inventory)
if ($LASTEXITCODE -ne 0 -or $inventory.Count -eq 0) { throw 'No retained SeaweedFS backup objects were found after Database deletion.' }
$serverInventory = @($inventory | Where-Object { $_.StartsWith("$backupServerName/", [StringComparison]::Ordinal) })
if ($serverInventory.Count -eq 0) { throw "No retained objects were found for backup server $backupServerName." }
$baseInventory = @($serverInventory | Where-Object { $_ -match '(?i)/base/' })
if ($baseInventory.Count -eq 0) { throw "No retained base backup object was found for backup server $backupServerName." }
$walInventory = @($serverInventory | Where-Object { $_ -match '(?i)(/wals?/|wal_)' })
if ($walInventory.Count -eq 0) { throw 'No retained WAL archive object was found after the forced WAL switch.' }
[IO.File]::WriteAllLines((Join-Path $ArtifactDirectory 'object-inventory.txt'), $serverInventory, [Text.UTF8Encoding]::new($false))
Add-Check $checks 'external-objects-retained' (Get-Date) "Backup objects and $($walInventory.Count) WAL archive objects remain in the external named volume after Kubernetes resource deletion."

$started = Get-Date
Apply-Document (New-DatabaseDocument -RecoverySource $backupServerName)
$recovered = Wait-DatabaseHealthy
$recoveredCluster = [string]$recovered.status.connectionSecretName
$recoveredCluster = $recoveredCluster.Substring(0, $recoveredCluster.Length - 4)
$recoveredChecksum = Get-DataChecksum $recoveredCluster
if ($recoveredChecksum -ne $sourceChecksum) {
    throw "Recovered checksum $recoveredChecksum does not match source checksum $sourceChecksum."
}
if ($recovered.status.backupServerName -eq $backupServerName) {
    throw 'Recovered Database reused the prior write archive server name.'
}
Add-Check $checks 'barman-restore-checksum-exact' $started 'Recovery selected the prior archive, wrote to a new UID-derived server, and restored the exact checksum.'

Capture-Snapshots 'success'
$evidence = [ordered]@{
    schemaVersion = 1
    result = 'passed'
    sourceSHA = $(if ($env:GITHUB_SHA) { $env:GITHUB_SHA } else { (& git -C $Root rev-parse HEAD).Trim() })
    startedAt = $startedAt.ToString('o')
    completedAt = (Get-Date).ToUniversalTime().ToString('o')
    namespace = $Namespace
    database = $DatabaseName
    sourceBackupServerName = $backupServerName
    recoveryBackupServerName = [string]$recovered.status.backupServerName
    sourceChecksum = $sourceChecksum
    recoveredChecksum = $recoveredChecksum
    objectCount = $serverInventory.Count
    walObjectCount = $walInventory.Count
    checks = $checks
}
Write-JsonFile 'evidence.json' $evidence

# Compatibility resources are disposable; external data remains in the named volume.
& kubectl get database $DatabaseName -n $Namespace *> $null
if ($LASTEXITCODE -eq 0) {
    Invoke-Kubectl annotate database $DatabaseName -n $Namespace 'steadystate.dev/force-delete=true' --overwrite
    Invoke-Kubectl delete database $DatabaseName -n $Namespace --wait=false
}

Write-Host "Phase 7 SeaweedFS/Barman compatibility passed with checksum $sourceChecksum."
