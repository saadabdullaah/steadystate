[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('doctor','tools','check-versions','lint','test','bootstrap','smoke','test-network-policy','diagnostics','destroy')]
    [string]$Command = 'doctor',
    [ValidateSet('minimal','standard','full')]
    [string]$Profile = $(if ($env:PROFILE) { $env:PROFILE } else { 'minimal' }),
    [string]$ClusterName = $(if ($env:CLUSTER_NAME) { $env:CLUSTER_NAME } else { 'steadystate' }),
    [int]$HttpPort = $(if ($env:HTTP_PORT) { [int]$env:HTTP_PORT } else { 8080 }),
    [int]$HttpsPort = $(if ($env:HTTPS_PORT) { [int]$env:HTTPS_PORT } else { 8443 })
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$IsWindowsHost = $env:OS -eq 'Windows_NT'
$Platform = if ($IsWindowsHost) { 'windows-amd64' } else { 'linux-amd64' }
$Exe = if ($IsWindowsHost) { '.exe' } else { '' }
$BinDir = Join-Path $Root ".tools/bin/$Platform"
$GoBinDir = Join-Path $Root '.tools/go/bin'
$env:PATH = "$GoBinDir$([IO.Path]::PathSeparator)$BinDir$([IO.Path]::PathSeparator)$env:PATH"
$env:GOCACHE = Join-Path $Root '.tools/cache/go-build'
$env:GOTMPDIR = Join-Path $Root '.tools/tmp'
New-Item -ItemType Directory -Force -Path $env:GOCACHE, $env:GOTMPDIR | Out-Null
if ($IsWindowsHost -and -not $env:DOCKER_CONTEXT) {
    $env:DOCKER_CONTEXT = 'desktop-linux'
}

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

function Invoke-External {
    param([Parameter(Mandatory)][string]$Executable, [Parameter(ValueFromRemainingArguments)][string[]]$Arguments)
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Executable exited with code $LASTEXITCODE" }
}

function Test-CommandAvailable([string]$Name) {
    return [bool](Get-Command $Name -ErrorAction SilentlyContinue)
}

function Wait-KubernetesResource {
    param(
        [Parameter(Mandatory)][string[]]$Arguments,
        [int]$TimeoutSeconds = 300
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        & kubectl get @Arguments *> $null
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($exitCode -eq 0) { return }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "Timed out waiting for Kubernetes resource: $($Arguments -join ' ')"
}

function Assert-Tools {
    $missing = @('go','kind','kubectl','helm') | Where-Object { -not (Test-CommandAvailable $_) }
    if ($missing) { throw "Missing tools: $($missing -join ', '). Run '.\scripts\dev.ps1 tools'." }
}

function Invoke-Doctor {
    $v = Read-Versions
    $problems = @()
    Write-Host "SteadyState environment doctor ($Platform)"
    Write-Host "Repository: $Root"
    foreach ($tool in @('git','docker','go','kind','kubectl','helm')) {
        if (Test-CommandAvailable $tool) { Write-Host "[PASS] $tool is available" -ForegroundColor Green }
        else { Write-Host "[FAIL] $tool is missing" -ForegroundColor Red; $problems += "$tool is missing" }
    }
    if (Test-CommandAvailable docker) {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $dockerJson = & docker info --format '{{json .}}' 2>$null
        $dockerExitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($dockerExitCode -eq 0) {
            Write-Host '[PASS] Docker engine is running' -ForegroundColor Green
            $dockerInfo = $dockerJson | ConvertFrom-Json
            if ([version]$dockerInfo.ServerVersion -lt [version]$v.MIN_DOCKER_VERSION) {
                Write-Host "[FAIL] Docker Engine $($dockerInfo.ServerVersion) is older than required $($v.MIN_DOCKER_VERSION)" -ForegroundColor Red
                $problems += 'Docker Engine must be upgraded'
            } else { Write-Host "[PASS] Docker Engine $($dockerInfo.ServerVersion)" -ForegroundColor Green }
            if ([string]$dockerInfo.CgroupVersion -ne [string]$v.REQUIRED_CGROUP_VERSION) {
                Write-Host "[FAIL] Docker uses cgroup v$($dockerInfo.CgroupVersion); v$($v.REQUIRED_CGROUP_VERSION) is required" -ForegroundColor Red
                $problems += 'Docker must use cgroup v2'
            } else { Write-Host '[PASS] Docker uses cgroup v2' -ForegroundColor Green }
        } else {
            Write-Host '[FAIL] Docker engine is not running' -ForegroundColor Red
            $problems += 'Docker engine is not running'
        }
    }
    foreach ($port in @($HttpPort, $HttpsPort)) {
        $listener = Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue
        if ($listener) { Write-Host "[FAIL] Port $port is already in use" -ForegroundColor Red; $problems += "Port $port is occupied" }
        else { Write-Host "[PASS] Port $port is available" -ForegroundColor Green }
    }
    if ($problems) { throw "Doctor found $($problems.Count) blocker(s): $($problems -join '; ')" }
}

function Invoke-CheckVersions {
    Assert-Tools
    $v = Read-Versions
    $actual = @{
        go = ((& go version) -split ' ')[2].TrimStart('go')
        kind = ((& kind version) -split ' ')[1].TrimStart('v')
        kubectl = (& kubectl version --client -o json | ConvertFrom-Json).clientVersion.gitVersion.TrimStart('v')
        helm = (& helm version --template '{{.Version}}').TrimStart('v')
    }
    $expected = @{go=$v.GO_VERSION; kind=$v.KIND_VERSION; kubectl=$v.KUBERNETES_VERSION; helm=$v.HELM_VERSION}
    foreach ($name in $expected.Keys) {
        if ($actual[$name] -ne $expected[$name]) { throw "$name version mismatch: expected $($expected[$name]), got $($actual[$name])" }
        Write-Host "[PASS] $name $($actual[$name])"
    }
}

function Get-ClusterNames {
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $output = @(& kind get clusters 2>$null)
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previousPreference
    if ($exitCode -eq 0) { return $output }
    return @()
}

function Invoke-Diagnostics {
    Assert-Tools
    $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
    $directory = Join-Path $Root ".artifacts/diagnostics/$stamp"
    New-Item -ItemType Directory -Force -Path $directory | Out-Null
    & kubectl get nodes -o wide *> (Join-Path $directory 'nodes.txt')
    & kubectl get pods -A -o wide *> (Join-Path $directory 'pods.txt')
    & kubectl get gatewayclass,gateway,httproute -A -o yaml *> (Join-Path $directory 'gateway-api.yaml')
    & kubectl get events -A --sort-by=.lastTimestamp *> (Join-Path $directory 'events.txt')
    & kind export logs (Join-Path $directory 'kind') --name $ClusterName *> (Join-Path $directory 'kind-export.txt')
    Write-Host "Diagnostics written to $directory"
}

function Invoke-Smoke {
    Assert-Tools
    Invoke-External kubectl wait -n steadystate-smoke --for=condition=Available deployment/echo --timeout=180s
    Invoke-External kubectl wait -n steadystate-smoke --for=condition=Programmed gateway/steadystate --timeout=180s
    $deadline = (Get-Date).AddMinutes(2)
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/healthz" -TimeoutSec 5
            if ($response.StatusCode -eq 200) { Write-Host "Smoke test passed at http://127.0.0.1:$HttpPort"; return }
        } catch { Start-Sleep -Seconds 3 }
    } while ((Get-Date) -lt $deadline)
    throw "Gateway smoke test failed at http://127.0.0.1:$HttpPort"
}

function Invoke-NetworkPolicyProof {
    Assert-Tools
    Invoke-External kubectl get daemonset calico-node -n calico-system
    Invoke-External kubectl delete namespace steadystate-network-test --ignore-not-found=true --wait=true
    $documents = Get-Content -Raw -LiteralPath (Join-Path $Root 'config/network-policy/proof.yaml')
    $parts = $documents -split "(?m)^---\s*$"
    $base = ($parts[0..3] -join "`n---`n")
    $policy = $parts[4]
    $base | & kubectl apply -f -
    if ($LASTEXITCODE -ne 0) { throw 'Failed to apply NetworkPolicy proof workloads' }
    Invoke-External kubectl wait -n steadystate-network-test --for=condition=Available deployment/server --timeout=180s
    Invoke-External kubectl wait -n steadystate-network-test --for=condition=Ready pod/client --timeout=180s
    $curlArguments = @('exec', '-n', 'steadystate-network-test', 'client', '--', 'curl', '--fail', '--silent', '--max-time', '5', 'http://server/healthz')
    Invoke-External -Executable kubectl -Arguments $curlArguments
    $policy | & kubectl apply -f -
    if ($LASTEXITCODE -ne 0) { throw 'Failed to apply deny policy' }
    Start-Sleep -Seconds 5
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & kubectl @curlArguments *> $null
    $curlExitCode = $LASTEXITCODE
    $ErrorActionPreference = $previousPreference
    if ($curlExitCode -eq 0) { throw 'NetworkPolicy did not block ingress; CNI enforcement is not working' }
    Invoke-External -Executable kubectl -Arguments @('exec', '-n', 'steadystate-network-test', 'client', '--', 'nslookup', 'kubernetes.default.svc.cluster.local')
    Write-Host 'NetworkPolicy proof passed: allowed before policy, denied after policy, DNS remained available.'
}

function Invoke-Bootstrap {
    Assert-Tools
    $started = Get-Date
    $v = Read-Versions
    try {
        $clusters = Get-ClusterNames
        if ($ClusterName -notin $clusters) {
            if ($IsWindowsHost) {
                foreach ($port in @($HttpPort, $HttpsPort)) {
                    if (Get-NetTCPConnection -LocalPort $port -State Listen -ErrorAction SilentlyContinue) { throw "Port $port is already in use" }
                }
            }
            $template = Get-Content -Raw -LiteralPath (Join-Path $Root "hack/kind-$Profile.yaml.tmpl")
            $rendered = $template.Replace('__CLUSTER_NAME__', $ClusterName).Replace('__NODE_IMAGE__', $v.KIND_NODE_IMAGE).Replace('__HTTP_PORT__', [string]$HttpPort).Replace('__HTTPS_PORT__', [string]$HttpsPort)
            $configPath = Join-Path $Root ".artifacts/kind-$Profile.yaml"
            New-Item -ItemType Directory -Force -Path (Split-Path $configPath) | Out-Null
            [IO.File]::WriteAllText($configPath, $rendered, [Text.UTF8Encoding]::new($false))
            Invoke-External kind create cluster --name $ClusterName --config $configPath --wait 60s
        } else {
            Write-Host "Cluster '$ClusterName' already exists; reconciling add-ons."
        }
        Invoke-External kubectl apply --server-side -f "https://raw.githubusercontent.com/projectcalico/calico/v$($v.CALICO_VERSION)/manifests/operator-crds.yaml"
        Invoke-External kubectl apply --server-side -f "https://raw.githubusercontent.com/projectcalico/calico/v$($v.CALICO_VERSION)/manifests/tigera-operator.yaml"
        Invoke-External kubectl apply -f "https://raw.githubusercontent.com/projectcalico/calico/v$($v.CALICO_VERSION)/manifests/custom-resources.yaml"
        Invoke-External kubectl wait --for=condition=Available deployment/tigera-operator -n tigera-operator --timeout=300s
        Wait-KubernetesResource -Arguments @('daemonset/calico-node', '-n', 'calico-system')
        Invoke-External kubectl rollout status daemonset/calico-node -n calico-system --timeout=300s
        Invoke-External kubectl wait nodes --all --for=condition=Ready --timeout=300s

        Invoke-External helm upgrade --install envoy-gateway oci://docker.io/envoyproxy/gateway-helm --version "v$($v.ENVOY_GATEWAY_VERSION)" --namespace envoy-gateway-system --create-namespace --wait --timeout 5m
        Invoke-External kubectl apply -f (Join-Path $Root 'config/smoke/smoke.yaml')
        Invoke-Smoke
        Invoke-NetworkPolicyProof
        $elapsed = (Get-Date) - $started
        Write-Host ("SteadyState {0} profile is ready in {1:n1} minutes." -f $Profile, $elapsed.TotalMinutes)
    } catch {
        Write-Warning $_.Exception.Message
        if ($ClusterName -in (Get-ClusterNames)) { Invoke-Diagnostics }
        else { Write-Warning 'Cluster creation failed before diagnostics could be collected.' }
        throw
    }
}

Push-Location $Root
try {
    switch ($Command) {
        'doctor' { Invoke-Doctor }
        'tools' { & (Join-Path $PSScriptRoot 'install-tools.ps1'); if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE } }
        'check-versions' { Invoke-CheckVersions }
        'lint' {
            & (Join-Path $PSScriptRoot 'check-private-files.ps1')
            & (Join-Path $PSScriptRoot 'check-text.ps1')
            Assert-Tools
            $goFiles = @(Get-ChildItem -Path $Root -Recurse -File -Filter '*.go' | Where-Object {
                $_.FullName -notlike "$(Join-Path $Root '.tools')*" -and
                $_.FullName -notlike "$(Join-Path $Root '.git')*"
            } | ForEach-Object { $_.FullName })
            $unformatted = if ($goFiles) { @(& gofmt -l @goFiles) } else { @() }
            if ($LASTEXITCODE -ne 0) { throw 'gofmt failed' }
            if ($unformatted) { throw "Go files require formatting: $($unformatted -join ', ')" }
            Invoke-External go vet ./...
        }
        'test' { Assert-Tools; Invoke-External go test ./... }
        'bootstrap' { Invoke-Bootstrap }
        'smoke' { Invoke-Smoke }
        'test-network-policy' { Invoke-NetworkPolicyProof }
        'diagnostics' { Invoke-Diagnostics }
        'destroy' {
            Assert-Tools
            if ($ClusterName -in (Get-ClusterNames)) { Invoke-External kind delete cluster --name $ClusterName }
            else { Write-Host "Cluster '$ClusterName' is already absent." }
        }
    }
} finally {
    Pop-Location
}
