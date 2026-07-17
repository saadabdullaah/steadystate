[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('doctor','tools','check-versions','generate','manifests','verify-generated','lint','test','test-envtest','run','build-images','load-images','deploy-operator','test-operator','demo-self-heal','test-isolation','undeploy-operator','deploy-gitops','test-gitops','undeploy-gitops','verify-gitops','verify-progressive-delivery','test-progressive-delivery','bootstrap','smoke','test-network-policy','diagnostics','destroy')]
    [string]$Command = 'doctor',
    [ValidateSet('minimal','standard','full')]
    [string]$Profile = $(if ($env:PROFILE) { $env:PROFILE } else { 'minimal' }),
    [string]$ClusterName = $(if ($env:CLUSTER_NAME) { $env:CLUSTER_NAME } else { 'steadystate' }),
    [int]$HttpPort = $(if ($env:HTTP_PORT) { [int]$env:HTTP_PORT } else { 8080 }),
    [int]$HttpsPort = $(if ($env:HTTPS_PORT) { [int]$env:HTTPS_PORT } else { 8443 }),
    [string]$EvidencePath,
    [string]$GitRevision = $(if ($env:GIT_REVISION) { $env:GIT_REVISION } else { 'main' })
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$IsWindowsHost = $env:OS -eq 'Windows_NT'
$Platform = if ($IsWindowsHost) { 'windows-amd64' } else { 'linux-amd64' }
$Exe = if ($IsWindowsHost) { '.exe' } else { '' }
$BinDir = Join-Path $Root ".tools/bin/$Platform"
$GoBinDir = Join-Path $Root ".tools/go/$Platform/bin"
$env:PATH = "$GoBinDir$([IO.Path]::PathSeparator)$BinDir$([IO.Path]::PathSeparator)$env:PATH"
$env:GOCACHE = Join-Path $Root '.tools/cache/go-build'
$env:GOMODCACHE = Join-Path $Root ".tools/cache/go-mod/$Platform"
$env:GOPATH = Join-Path $Root ".tools/gopath/$Platform"
$env:GOTMPDIR = Join-Path $Root '.tools/tmp'
if ($IsWindowsHost) {
    $env:LOCALAPPDATA = Join-Path $Root ".tools/cache/localappdata/$Platform"
}
$cacheDirectories = @($env:GOCACHE, $env:GOMODCACHE, $env:GOPATH, $env:GOTMPDIR)
if ($IsWindowsHost) { $cacheDirectories += $env:LOCALAPPDATA }
New-Item -ItemType Directory -Force -Path $cacheDirectories | Out-Null
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

$VersionLock = Read-Versions
$OperatorImage = $VersionLock.OPERATOR_IMAGE
$DemoImage = $VersionLock.DEMO_IMAGE

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

function Assert-CodegenTools {
    Assert-Tools
    $missing = @('controller-gen','kustomize') | Where-Object { -not (Test-CommandAvailable $_) }
    if ($missing) { throw "Missing code-generation tools: $($missing -join ', '). Run '.\scripts\dev.ps1 tools'." }
}

function Assert-Docker {
    if (-not (Test-CommandAvailable 'docker')) { throw "Docker is missing. Run '.\scripts\dev.ps1 doctor'." }
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & docker info *> $null
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previousPreference
    if ($exitCode -ne 0) { throw 'Docker engine is not running.' }
}

function Assert-Cluster {
    Assert-Tools
    if ($ClusterName -notin (Get-ClusterNames)) { throw "kind cluster '$ClusterName' is absent. Run '.\scripts\dev.ps1 bootstrap'." }
}

function Invoke-Generate {
    Assert-CodegenTools
    # Explicit package paths avoid controller-gen's recursive-pattern expansion bug on Windows.
    Invoke-External controller-gen object:headerFile=hack/boilerplate.go.txt paths=./api/v1alpha1
}

function Invoke-Manifests {
    Assert-CodegenTools
    Invoke-External controller-gen "rbac:roleName=manager-role" crd:maxDescLen=0 webhook paths=./api/v1alpha1 paths=./internal/controller output:crd:artifacts:config=config/crd/bases
}

function Invoke-Envtest {
    if (-not $IsWindowsHost) {
        if (-not (Test-CommandAvailable 'setup-envtest')) { throw "setup-envtest is missing. Run './scripts/install-tools.sh'." }
        $v = Read-Versions
        $env:KUBEBUILDER_ASSETS = (& setup-envtest use $v.ENVTEST_K8S_VERSION -p path).Trim()
        if ($LASTEXITCODE -ne 0) { throw 'Failed to provision envtest assets' }
        Invoke-External go test -tags=envtest ./internal/controller/...
        return
    }

    if (-not (Test-CommandAvailable 'wsl.exe')) { throw 'WSL is required for envtest on Windows.' }
    $v = Read-Versions
    if ($Root -notmatch '^([A-Za-z]):\\(.*)$') { throw "Cannot map repository path '$Root' into WSL." }
    $drive = $Matches[1].ToLowerInvariant()
    $relativePath = $Matches[2].Replace('\', '/')
    $wslRoot = "/mnt/$drive/$relativePath"
    $assets = (& wsl.exe -d Ubuntu -- "$wslRoot/.tools/bin/linux-amd64/setup-envtest" use $v.ENVTEST_K8S_VERSION -p path).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $assets) { throw 'Failed to provision envtest assets in WSL Ubuntu.' }
    & wsl.exe -d Ubuntu -- env `
        "KUBEBUILDER_ASSETS=$assets" `
        "GOCACHE=$wslRoot/.tools/cache/go-build/linux-amd64" `
        "GOMODCACHE=$wslRoot/.tools/cache/go-mod/linux-amd64" `
        "GOPATH=$wslRoot/.tools/gopath/linux-amd64" `
        "XDG_CACHE_HOME=$wslRoot/.tools/cache/xdg/linux-amd64" `
        "$wslRoot/.tools/go/linux-amd64/bin/go" -C $wslRoot test -tags=envtest ./internal/controller/...
    if ($LASTEXITCODE -ne 0) { throw "WSL envtest exited with code $LASTEXITCODE" }
}

function Invoke-BuildImages {
    Assert-Docker
    $v = Read-Versions
    Invoke-External docker build --platform linux/amd64 --pull --build-arg "GO_BUILDER=$($v.GO_BUILDER_IMAGE)" --file Dockerfile --tag $OperatorImage .
    Invoke-External docker build --platform linux/amd64 --pull --build-arg "GO_BUILDER=$($v.GO_BUILDER_IMAGE)" --file apps/demo-app/Dockerfile --tag $DemoImage .
    Write-Host "Built $OperatorImage and $DemoImage"
}

function Invoke-LoadImages {
    Assert-Cluster
    Assert-Docker
    Invoke-External docker image inspect $OperatorImage
    Invoke-External docker image inspect $DemoImage
    Invoke-External kind load docker-image $OperatorImage $DemoImage --name $ClusterName
    Write-Host "Loaded operator and demo images into kind cluster '$ClusterName'."
}

function Invoke-DeployOperator {
    Assert-Cluster
    Invoke-External kubectl apply -k (Join-Path $Root 'config/default')
    Invoke-External kubectl rollout status deployment/steadystate-controller-manager -n steadystate-system --timeout=180s
    Write-Host 'SteadyState Application controller is available.'
}

function Wait-ApplicationReady {
    param([string]$Name = 'demo', [string]$Namespace = 'team-payments', [int]$TimeoutSeconds = 60)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $ready = & kubectl get application $Name -n $Namespace -o "jsonpath={.status.conditions[?(@.type=='Ready')].status}" 2>$null
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($exitCode -eq 0 -and $ready -eq 'True') { return }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw "Application $Namespace/$Name did not reach Ready=True within $TimeoutSeconds seconds."
}

function Wait-TeamReady {
    param([string]$Name = 'payments', [int]$TimeoutSeconds = 60)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $ready = & kubectl get team $Name -o "jsonpath={.status.conditions[?(@.type=='Ready')].status}" 2>$null
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($exitCode -eq 0 -and $ready -eq 'True') { return }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw "Team $Name did not reach Ready=True within $TimeoutSeconds seconds."
}

function Invoke-TestOperator {
    Assert-Cluster
    Invoke-External kubectl apply -f (Join-Path $Root 'config/samples/platform_v1alpha1_team.yaml')
    Wait-TeamReady -Name payments -TimeoutSeconds 60
    Invoke-External kubectl apply -f (Join-Path $Root 'config/samples/platform_v1alpha1_application.yaml')
    Wait-ApplicationReady -Name demo -Namespace team-payments -TimeoutSeconds 60
    $deadline = (Get-Date).AddSeconds(60)
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/" -Headers @{ Host = 'demo.team-payments.steadystate.localtest.me' } -TimeoutSec 5
            if ($response.StatusCode -eq 200) {
                $body = $response.Content | ConvertFrom-Json
                if ($body.application -eq 'demo' -and $body.version -eq 'v0.1.0') {
                    Write-Host 'Application operator test passed through the shared Gateway.'
                    return
                }
            }
        } catch { Start-Sleep -Seconds 2 }
    } while ((Get-Date) -lt $deadline)
    throw 'Application reached Ready=True but did not return the expected response through Envoy Gateway.'
}

function Invoke-UndeployOperator {
    Assert-Cluster
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & kubectl get customresourcedefinition applications.platform.steadystate.dev *> $null
    $applicationsExist = $LASTEXITCODE -eq 0
    & kubectl get customresourcedefinition teams.platform.steadystate.dev *> $null
    $teamsExist = $LASTEXITCODE -eq 0
    $ErrorActionPreference = $previousPreference
    if ($applicationsExist) {
        Invoke-External kubectl delete applications.platform.steadystate.dev --all --all-namespaces --ignore-not-found=true --wait=true --timeout=180s
    }
    if ($teamsExist) {
        Invoke-External kubectl delete teams.platform.steadystate.dev --all --ignore-not-found=true --wait=true --timeout=180s
    }
    Invoke-External kubectl delete namespace steadystate-unmanaged --ignore-not-found=true --wait=true --timeout=120s
    Invoke-External kubectl delete -k (Join-Path $Root 'config/default') --ignore-not-found=true --wait=true --timeout=180s
    Write-Host 'SteadyState Application controller is undeployed.'
}

function Invoke-GitOpsCommand {
    param([Parameter(Mandatory)][ValidateSet('Deploy','Test','Undeploy','Verify')][string]$Mode)
    if ($Mode -ne 'Verify') { Assert-Cluster }
    & (Join-Path $PSScriptRoot 'gitops.ps1') `
        -Mode $Mode `
        -HttpPort $HttpPort `
        -GitRevision $GitRevision `
        -EvidencePath $EvidencePath `
        -Profile $Profile
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
    $developmentTools = @{
        'controller-gen' = $v.CONTROLLER_TOOLS_VERSION
        'kustomize' = $v.KUSTOMIZE_VERSION
        'setup-envtest' = $v.SETUP_ENVTEST_VERSION
        'golangci-lint' = $v.GOLANGCI_LINT_VERSION
    }
    foreach ($name in $developmentTools.Keys) {
        $marker = Join-Path $BinDir "$name.version"
        if (-not (Test-Path -LiteralPath $marker)) { continue }
        $actualVersion = (Get-Content -Raw -LiteralPath $marker).Trim()
        if ($actualVersion -ne $developmentTools[$name]) { throw "$name version mismatch: expected $($developmentTools[$name]), got $actualVersion" }
        Write-Host "[PASS] $name $actualVersion"
    }
    foreach ($tool in @('kubectl-argo-rollouts','k6')) {
        if (-not (Test-CommandAvailable $tool)) { throw "$tool is missing from the pinned toolchain." }
    }
    $rolloutsVersion = ((& kubectl-argo-rollouts version --short) -join "`n")
    if ($LASTEXITCODE -ne 0 -or $rolloutsVersion -notmatch [regex]::Escape("v$($v.ARGO_ROLLOUTS_VERSION)")) {
        throw "Rollouts CLI version mismatch: expected v$($v.ARGO_ROLLOUTS_VERSION), got $rolloutsVersion"
    }
    Write-Host "[PASS] kubectl-argo-rollouts $($v.ARGO_ROLLOUTS_VERSION)"
    $k6Version = ((& k6 version) -join "`n")
    if ($LASTEXITCODE -ne 0 -or $k6Version -notmatch [regex]::Escape("v$($v.K6_VERSION)")) {
        throw "k6 version mismatch: expected v$($v.K6_VERSION), got $k6Version"
    }
    Write-Host "[PASS] k6 $($v.K6_VERSION)"
    if (-not $IsWindowsHost -and (Test-CommandAvailable 'kubebuilder')) {
        $kubebuilderVersion = ((& kubebuilder version) -join "`n")
        if ($LASTEXITCODE -ne 0 -or $kubebuilderVersion -notmatch [regex]::Escape("v$($v.KUBEBUILDER_VERSION)")) {
            throw "kubebuilder version mismatch: expected v$($v.KUBEBUILDER_VERSION), got $kubebuilderVersion"
        }
        Write-Host "[PASS] kubebuilder $($v.KUBEBUILDER_VERSION)"
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
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        & kubectl get nodes -o wide *> (Join-Path $directory 'nodes.txt')
        & kubectl get pods -A -o wide *> (Join-Path $directory 'pods.txt')
        & kubectl get gatewayclass,gateway,httproute -A -o yaml *> (Join-Path $directory 'gateway-api.yaml')
        & kubectl get teams.platform.steadystate.dev -o yaml *> (Join-Path $directory 'teams.yaml')
        & kubectl get namespace,resourcequota,limitrange,serviceaccount,role.rbac.authorization.k8s.io,rolebinding.rbac.authorization.k8s.io,networkpolicy -A -l steadystate.dev/team -o yaml *> (Join-Path $directory 'team-boundaries.yaml')
        & kubectl get clusterrole steadystate-team-owner -o yaml *> (Join-Path $directory 'team-owner-clusterrole.yaml')
        & kubectl get applications.platform.steadystate.dev -A -o yaml *> (Join-Path $directory 'applications.yaml')
        & kubectl get applications.argoproj.io,appprojects.argoproj.io -n argocd -o yaml *> (Join-Path $directory 'argocd-applications.yaml')
        & kubectl get all,configmap,httproute -n argocd -o yaml *> (Join-Path $directory 'argocd-resources.yaml')
        & kubectl logs -n argocd -l app.kubernetes.io/part-of=argocd --all-containers --tail=500 --prefix=true *> (Join-Path $directory 'argocd.log')
        & kubectl get rollout,analysisrun,analysistemplate -A -o yaml *> (Join-Path $directory 'rollouts.yaml')
        & kubectl get prometheus,alertmanager,servicemonitor,prometheusrule -A -o yaml *> (Join-Path $directory 'monitoring.yaml')
        & kubectl get all,configmap -n argo-rollouts -o yaml *> (Join-Path $directory 'argo-rollouts-resources.yaml')
        & kubectl logs -n argo-rollouts deployment/argo-rollouts --all-containers --tail=500 *> (Join-Path $directory 'argo-rollouts.log')
        & kubectl get all -n monitoring -o yaml *> (Join-Path $directory 'monitoring-resources.yaml')
        & kubectl logs -n monitoring -l app.kubernetes.io/name=prometheus-operator --all-containers --tail=500 --prefix=true *> (Join-Path $directory 'prometheus-operator.log')
        & kubectl get deployment,service,configmap -A -l app.kubernetes.io/managed-by=steadystate -o yaml *> (Join-Path $directory 'application-children.yaml')
        & kubectl logs -n steadystate-system deployment/steadystate-controller-manager --all-containers --tail=500 *> (Join-Path $directory 'operator.log')
        & kubectl get events -A --sort-by=.lastTimestamp *> (Join-Path $directory 'events.txt')
        & kind export logs (Join-Path $directory 'kind') --name $ClusterName *> (Join-Path $directory 'kind-export.txt')
    } finally {
        $ErrorActionPreference = $previousPreference
    }
    Write-Host "Diagnostics written to $directory"
}

function Invoke-Smoke {
    Assert-Tools
    Invoke-External kubectl wait -n steadystate-smoke --for=condition=Available deployment/echo --timeout=180s
    Invoke-External kubectl wait -n steadystate-system --for=condition=Programmed gateway/steadystate --timeout=180s
    $deadline = (Get-Date).AddMinutes(2)
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/healthz" -Headers @{ Host = 'smoke.steadystate.localtest.me' } -TimeoutSec 5
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
    $connectivityDeadline = (Get-Date).AddSeconds(60)
    do {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        & kubectl @curlArguments *> $null
        $curlExitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($curlExitCode -eq 0) { break }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $connectivityDeadline)
    if ($curlExitCode -ne 0) {
        throw 'Positive connectivity was not established before applying the deny policy'
    }
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
        Invoke-External kubectl delete gateway steadystate -n steadystate-smoke --ignore-not-found=true --wait=true
        Invoke-External kubectl delete envoyproxy steadystate-proxy -n steadystate-smoke --ignore-not-found=true --wait=true
        Invoke-External kubectl apply -k (Join-Path $Root 'config/gateway')
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
        'generate' { Invoke-Generate }
        'manifests' { Invoke-Manifests }
        'verify-generated' {
            Invoke-Generate
            Invoke-Manifests
            & (Join-Path $PSScriptRoot 'check-vendored.ps1')
            Invoke-External git diff --exit-code -- api config/crd config/rbac
        }
        'lint' {
            & (Join-Path $PSScriptRoot 'check-private-files.ps1')
            & (Join-Path $PSScriptRoot 'check-text.ps1')
            & (Join-Path $PSScriptRoot 'check-vendored.ps1')
            Assert-Tools
            $goFiles = @(Get-ChildItem -Path $Root -Recurse -File -Filter '*.go' | Where-Object {
                $_.FullName -notlike "$(Join-Path $Root '.tools')*" -and
                $_.FullName -notlike "$(Join-Path $Root '.git')*"
            } | ForEach-Object { $_.FullName })
            $unformatted = if ($goFiles) { @(& gofmt -l @goFiles) } else { @() }
            if ($LASTEXITCODE -ne 0) { throw 'gofmt failed' }
            if ($unformatted) { throw "Go files require formatting: $($unformatted -join ', ')" }
            Invoke-External go vet ./...
            if (-not (Test-CommandAvailable 'kustomize')) { throw "kustomize is missing. Run '.\scripts\dev.ps1 tools'." }
            foreach ($overlay in @('config/default','config/gateway','config/samples','gitops/platform','gitops/teams/payments','gitops/applications/demo')) {
                & kustomize build $overlay *> $null
                if ($LASTEXITCODE -ne 0) { throw "Kustomize rendering failed for $overlay" }
            }
            if (-not (Test-CommandAvailable 'golangci-lint')) { throw "golangci-lint is missing. Run '.\scripts\dev.ps1 tools'." }
            Invoke-External golangci-lint run ./...
        }
        'test' { Assert-Tools; Invoke-External go test ./... }
        'test-envtest' { Invoke-Envtest }
        'run' { Assert-Tools; Invoke-External go run ./cmd }
        'build-images' { Invoke-BuildImages }
        'load-images' { Invoke-LoadImages }
        'deploy-operator' { Invoke-DeployOperator }
        'test-operator' { Invoke-TestOperator }
        'demo-self-heal' {
            Assert-Cluster
            & (Join-Path $PSScriptRoot 'demo-self-heal.ps1') -HttpPort $HttpPort -EvidencePath $EvidencePath
        }
        'test-isolation' {
            Assert-Cluster
            & (Join-Path $PSScriptRoot 'test-isolation.ps1') -HttpPort $HttpPort -Profile $Profile -EvidencePath $EvidencePath
        }
        'undeploy-operator' { Invoke-UndeployOperator }
        'bootstrap' { Invoke-Bootstrap }
        'deploy-gitops' { Invoke-GitOpsCommand -Mode Deploy }
        'test-gitops' { Invoke-GitOpsCommand -Mode Test }
        'undeploy-gitops' { Invoke-GitOpsCommand -Mode Undeploy }
        'verify-gitops' { Invoke-GitOpsCommand -Mode Verify }
        'verify-progressive-delivery' { & (Join-Path $PSScriptRoot 'progressive-delivery.ps1') -Mode Verify }
        'test-progressive-delivery' { Assert-Cluster; & (Join-Path $PSScriptRoot 'progressive-delivery.ps1') -Mode Test -HttpPort $HttpPort -EvidencePath $EvidencePath -Profile $Profile }
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
