[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('Start','Stop','Verify','Inventory','Endpoint')]
    [string]$Action = 'Verify',
    [string]$ClusterName = 'steadystate',
    [int]$HostPort = 8333,
    [string]$Subnet = '172.30.240.0/24',
    [string]$ContainerIP = '172.30.240.10',
    [ValidateRange(128, 400)]
    [int]$MemoryLimitMiB = 384,
    [switch]$PurgeData
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$ContainerName = 'steadystate-seaweedfs'
$NetworkName = 'steadystate-backup'
$VolumeName = 'steadystate-backup-data'
$BucketName = 'steadystate-backups'
$CredentialFile = Join-Path $Root '.artifacts/secrets/rendered/backup-store.yaml'

function Read-Versions {
    $values = @{}
    foreach ($line in Get-Content -LiteralPath (Join-Path $PSScriptRoot 'versions.env') -Encoding UTF8) {
        $trimmed = $line.Trim()
        if (-not $trimmed -or $trimmed.StartsWith('#')) { continue }
        $parts = $trimmed.Split('=', 2)
        $values[$parts[0]] = $parts[1]
    }
    return $values
}

function Invoke-Docker {
    param([Parameter(ValueFromRemainingArguments)][string[]]$Arguments)
    & docker @Arguments
    if ($LASTEXITCODE -ne 0) { throw "docker exited with code $LASTEXITCODE" }
}

function Get-ExactResource([string]$Kind, [string]$Name) {
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $value = (& docker $Kind inspect $Name 2>$null)
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previous
    $global:LASTEXITCODE = 0
    return [pscustomobject]@{ Exists = $exitCode -eq 0; Value = $value }
}

function Assert-Docker {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw 'Docker is required.' }
    & docker info *> $null
    if ($LASTEXITCODE -ne 0) { throw 'Docker engine is unavailable.' }
}

function Get-Credentials {
    $keyPath = Join-Path $Root '.artifacts/secrets/steadystate.agekey'
    & (Join-Path $PSScriptRoot 'secrets.ps1') -Action DecryptBackup -KeyPath $keyPath
    try {
        $content = Get-Content -LiteralPath $CredentialFile -Raw -Encoding UTF8
        $accessKey = [regex]::Match($content, '(?m)^\s*ACCESS_KEY_ID:\s*(\S+)\s*$').Groups[1].Value
        $secretKey = [regex]::Match($content, '(?m)^\s*ACCESS_SECRET_KEY:\s*(\S+)\s*$').Groups[1].Value
        if (-not $accessKey -or -not $secretKey) { throw 'The decrypted backup-store Secret is malformed.' }
        return [pscustomobject]@{ AccessKey = $accessKey; SecretKey = $secretKey }
    } finally {
        Remove-Item -LiteralPath $CredentialFile -Force -ErrorAction SilentlyContinue
    }
}

function Assert-SubnetAvailable {
    $networks = @(& docker network ls --format '{{.Name}}')
    foreach ($network in $networks) {
        if ($network -eq $NetworkName) { continue }
        $subnets = @(& docker network inspect $network --format '{{range .IPAM.Config}}{{.Subnet}}{{"\n"}}{{end}}' 2>$null)
        if ($subnets -contains $Subnet) {
            throw "Requested backup subnet $Subnet is already used by Docker network '$network'. Pass a collision-free -Subnet."
        }
    }
}

function Connect-ClusterNodes {
    if (-not (Get-Command kind -ErrorAction SilentlyContinue)) { return }
    $nodes = @(& kind get nodes --name $ClusterName 2>$null)
    if ($LASTEXITCODE -ne 0) { return }
    foreach ($node in $nodes) {
        if ($node -notmatch "^$([regex]::Escape($ClusterName))-(control-plane|worker[0-9]*)$") {
            throw "Refusing to connect unexpected container '$node' to the backup network."
        }
        $attached = & docker inspect $node --format "{{json .NetworkSettings.Networks.$NetworkName}}" 2>$null
        if ($LASTEXITCODE -ne 0 -or $attached -eq 'null') {
            Invoke-Docker network connect $NetworkName $node
        }
    }
}

function Wait-Healthy {
    $deadline = (Get-Date).AddMinutes(2)
    do {
        $status = (& docker inspect $ContainerName --format '{{.State.Health.Status}}' 2>$null)
        if ($LASTEXITCODE -eq 0 -and $status -eq 'healthy') { return }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    $health = (& docker inspect $ContainerName --format '{{json .State.Health}}' 2>$null)
    if ($health) { Write-Warning "SeaweedFS health state: $health" }
    throw 'SeaweedFS did not become healthy within two minutes.'
}

function Get-FilerDirectory([string]$Path) {
    $uri = "http://127.0.0.1:8888$Path/?pretty=y"
    $raw = @(& docker exec $ContainerName wget -qO- --header 'Accept: application/json' $uri)
    if ($LASTEXITCODE -ne 0 -or -not $raw) {
        throw "SeaweedFS filer could not list $Path."
    }
    try {
        return (($raw -join [Environment]::NewLine) | ConvertFrom-Json)
    } catch {
        throw "SeaweedFS filer returned invalid JSON for ${Path}: $($_.Exception.Message)"
    }
}

function Get-BucketObjectInventory {
    $bucketRoot = "/buckets/$BucketName"
    $directories = [System.Collections.Generic.Queue[string]]::new()
    $visited = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    $objects = [System.Collections.Generic.List[string]]::new()
    $directories.Enqueue($bucketRoot)
    while ($directories.Count -gt 0) {
        $directory = $directories.Dequeue()
        if (-not $visited.Add($directory)) { continue }
        $listing = Get-FilerDirectory $directory
        foreach ($entry in @($listing.Entries)) {
            $fullPath = [string]$entry.FullPath
            if (-not $fullPath.StartsWith("$bucketRoot/", [StringComparison]::Ordinal)) { continue }
            if (([uint64]$entry.Mode -band [uint64]2147483648) -ne 0) {
                $directories.Enqueue($fullPath)
            } else {
                $objects.Add($fullPath.Substring($bucketRoot.Length + 1))
            }
        }
    }
    return @($objects | Sort-Object)
}

Assert-Docker
$versions = Read-Versions
$image = $versions.SEAWEEDFS_IMAGE

switch ($Action) {
    'Start' {
        if ($HostPort -lt 1 -or $HostPort -gt 65535) { throw 'HostPort must be between 1 and 65535.' }
        if (-not (Get-ExactResource network $NetworkName).Exists) {
            Assert-SubnetAvailable
            Invoke-Docker network create --driver bridge --subnet $Subnet $NetworkName
        } else {
            $existingSubnet = (& docker network inspect $NetworkName --format '{{(index .IPAM.Config 0).Subnet}}').Trim()
            if ($existingSubnet -ne $Subnet) {
                throw "Existing exact network '$NetworkName' uses $existingSubnet instead of requested $Subnet."
            }
        }
        if (-not (Get-ExactResource volume $VolumeName).Exists) {
            Invoke-Docker volume create $VolumeName
        }
        if ((Get-ExactResource container $ContainerName).Exists) {
            Invoke-Docker rm --force $ContainerName
        }
        $credentials = Get-Credentials
        $goMemoryLimitMiB = [Math]::Floor($MemoryLimitMiB * 0.8)
        Invoke-Docker run --detach --name $ContainerName --network $NetworkName `
            --ip $ContainerIP `
            --restart unless-stopped `
            --memory "$MemoryLimitMiB`m" `
            --memory-swap "$MemoryLimitMiB`m" `
            --publish "127.0.0.1:$HostPort`:8333" `
            --volume "$VolumeName`:/data" `
            --env "AWS_ACCESS_KEY_ID=$($credentials.AccessKey)" `
            --env "AWS_SECRET_ACCESS_KEY=$($credentials.SecretKey)" `
            --env "S3_BUCKET=$BucketName" `
            --env "GOMEMLIMIT=$($goMemoryLimitMiB)MiB" `
            --health-cmd 'wget -q -O - http://127.0.0.1:9333/cluster/status >/dev/null || exit 1' `
            --health-interval 5s --health-timeout 3s --health-retries 24 `
            $image mini -dir=/data
        Connect-ClusterNodes
        Wait-Healthy
        Write-Host "SeaweedFS backup store is healthy at the loopback-only endpoint http://127.0.0.1:$HostPort."
    }
    'Stop' {
        if ((Get-ExactResource container $ContainerName).Exists) {
            Invoke-Docker rm --force $ContainerName
        }
        if ((Get-ExactResource network $NetworkName).Exists) {
            Invoke-Docker network rm $NetworkName
        }
        if ($PurgeData -and (Get-ExactResource volume $VolumeName).Exists) {
            Invoke-Docker volume rm $VolumeName
        }
        $retention = if ($PurgeData) { 'purged by explicit request' } else { 'preserved' }
        Write-Host "Stopped the exact SteadyState backup-store resources; backup volume $retention."
    }
    'Verify' {
        if (-not (Get-ExactResource container $ContainerName).Exists) { throw 'SeaweedFS container is absent.' }
        if (-not (Get-ExactResource network $NetworkName).Exists) { throw 'SeaweedFS network is absent.' }
        if (-not (Get-ExactResource volume $VolumeName).Exists) { throw 'SeaweedFS volume is absent.' }
        Wait-Healthy
        $runningImage = (& docker inspect $ContainerName --format '{{.Config.Image}}').Trim()
        if ($runningImage -ne $image) { throw 'SeaweedFS is not using the exact pinned image digest.' }
        $runningIP = (& docker inspect $ContainerName --format "{{(index .NetworkSettings.Networks `"$NetworkName`").IPAddress}}").Trim()
        if ($runningIP -ne $ContainerIP) { throw "SeaweedFS has IP $runningIP instead of $ContainerIP." }
        $mountedVolume = (& docker inspect $ContainerName --format '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}').Trim()
        if ($mountedVolume -ne $VolumeName) { throw 'SeaweedFS is not using the exact persistent backup volume.' }
        $expectedMemoryBytes = [int64]$MemoryLimitMiB * 1MB
        $memoryLimitBytes = [int64]((& docker inspect $ContainerName --format '{{.HostConfig.Memory}}').Trim())
        $memorySwapBytes = [int64]((& docker inspect $ContainerName --format '{{.HostConfig.MemorySwap}}').Trim())
        if ($memoryLimitBytes -ne $expectedMemoryBytes -or $memorySwapBytes -ne $expectedMemoryBytes) {
            throw "SeaweedFS memory is not capped at $MemoryLimitMiB MiB without swap growth."
        }
        $environmentNames = @(& docker inspect $ContainerName --format '{{range .Config.Env}}{{println .}}{{end}}' |
            ForEach-Object { ($_ -split '=', 2)[0] })
        foreach ($required in @('AWS_ACCESS_KEY_ID','AWS_SECRET_ACCESS_KEY','S3_BUCKET')) {
            if ($environmentNames -notcontains $required) { throw "SeaweedFS is missing required environment setting $required." }
        }
        $binding = & docker port $ContainerName 8333/tcp
        if ($binding -notmatch "^127\.0\.0\.1:$HostPort$") { throw "SeaweedFS is not bound only to 127.0.0.1:$HostPort." }
        Write-Host "SeaweedFS health, exact identity, persistent volume, loopback binding, and $MemoryLimitMiB MiB memory cap verified."
    }
    'Inventory' {
        if (-not (Get-ExactResource container $ContainerName).Exists) { throw 'SeaweedFS container is absent.' }
        Wait-Healthy
        $objects = @(Get-BucketObjectInventory)
        if ($objects.Count -eq 0) { throw "SeaweedFS bucket $BucketName contains no objects." }
        $objects | Write-Output
    }
    'Endpoint' {
        Write-Output "http://$ContainerName`:8333"
    }
}
