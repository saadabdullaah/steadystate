[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateSet('Deploy','Test','Undeploy','Verify')]
    [string]$Mode,
    [int]$HttpPort = 8080,
    [string]$GitRevision = 'main',
    [string]$EvidencePath,
    [ValidateSet('minimal','standard','full')][string]$Profile = 'standard'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$ArtifactRoot = Join-Path $Root '.artifacts/gitops'
$ChartPath = Join-Path $Root 'gitops/clusters/local'
$PlatformPath = Join-Path $Root 'gitops/platform'

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
    param(
        [Parameter(Mandatory)][string]$Executable,
        [Parameter(ValueFromRemainingArguments)][string[]]$Arguments
    )
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "$Executable exited with code $LASTEXITCODE"
    }
}

function Write-Utf8 {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Content)
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null
    [IO.File]::WriteAllText($Path, $Content, [Text.UTF8Encoding]::new($false))
}

function Get-ArgoManifest {
    $versions = Read-Versions
    $version = $versions.ARGO_CD_VERSION
    $expected = $versions.ARGO_CD_MANIFEST_SHA256
    if (-not $version -or -not $expected) {
        throw 'ARGO_CD_VERSION and ARGO_CD_MANIFEST_SHA256 must be pinned.'
    }
    New-Item -ItemType Directory -Force -Path $ArtifactRoot | Out-Null
    $path = Join-Path $ArtifactRoot "argocd-install-v$version.yaml"
    if (Test-Path -LiteralPath $path) {
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
        if ($actual -ne $expected) {
            $quarantine = "$path.invalid-$(Get-Date -Format 'yyyyMMddHHmmss')"
            Move-Item -LiteralPath $path -Destination $quarantine
        }
    }
    if (-not (Test-Path -LiteralPath $path)) {
        $uri = "https://raw.githubusercontent.com/argoproj/argo-cd/v$version/manifests/install.yaml"
        Invoke-WebRequest -UseBasicParsing -Uri $uri -OutFile $path
    }
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "Argo CD manifest checksum mismatch: expected $expected, got $actual"
    }
    return $path
}

function Assert-Commands {
    param([string[]]$Names)
    $missing = @($Names | Where-Object { -not (Get-Command $_ -ErrorAction SilentlyContinue) })
    if ($missing) {
        throw "Missing commands: $($missing -join ', ')"
    }
}

function Assert-ChartChecksum {
    param(
        [Parameter(Mandatory)][string]$Repository,
        [Parameter(Mandatory)][string]$Chart,
        [Parameter(Mandatory)][string]$Version,
        [Parameter(Mandatory)][string]$Expected
    )
    $chartDirectory = Join-Path $ArtifactRoot 'charts'
    New-Item -ItemType Directory -Force -Path $chartDirectory | Out-Null
    $path = Join-Path $chartDirectory "$Chart-$Version.tgz"
    if (-not (Test-Path -LiteralPath $path)) {
        Invoke-External helm pull $Chart --repo $Repository --version $Version --destination $chartDirectory
    }
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $path).Hash.ToLowerInvariant()
    if ($actual -ne $Expected) {
        throw "$Chart chart checksum mismatch: expected $Expected, got $actual"
    }
}

function Remove-Dex {
    foreach ($kind in @('serviceaccount','role','rolebinding','service','deployment')) {
        Invoke-External kubectl delete $kind argocd-dex-server -n argocd --ignore-not-found=true --wait=true
    }
}

function Assert-DexAbsent {
    foreach ($kind in @('serviceaccount','role','rolebinding','service','deployment')) {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $found = & kubectl get $kind argocd-dex-server -n argocd --ignore-not-found -o name 2>$null
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($exitCode -ne 0) {
            throw "Failed to verify Dex $kind absence."
        }
        if ($found) {
            throw "Unused Dex object remains: $found"
        }
    }
}

function Wait-ArgoWorkloads {
    Invoke-External kubectl rollout status statefulset/argocd-application-controller -n argocd --timeout=600s
    foreach ($name in @(
        'argocd-applicationset-controller',
        'argocd-notifications-controller',
        'argocd-redis',
        'argocd-repo-server',
        'argocd-server'
    )) {
        Invoke-External kubectl rollout status "deployment/$name" -n argocd --timeout=600s
    }
}

function Render-RootTemplate {
    param(
        [Parameter(Mandatory)][string]$Template,
        [Parameter(Mandatory)][string]$Path,
        [switch]$BootstrapRoot
    )
    $arguments = @(
        'template', 'steadystate-root', $ChartPath,
        '--namespace', 'argocd',
        '--set-string', "gitRevision=$GitRevision",
        '--show-only', $Template
    )
    if ($BootstrapRoot) {
        $arguments += @(
            '--set', 'bootstrapRoot=true',
            '--set-string', "rootTargetRevision=$GitRevision"
        )
    }
    $rendered = @(& helm @arguments)
    if ($LASTEXITCODE -ne 0) {
        throw "helm template failed for $Template"
    }
    Write-Utf8 -Path $Path -Content (($rendered -join [Environment]::NewLine) + [Environment]::NewLine)
}

function Get-KubernetesObject {
    param([Parameter(Mandatory)][string[]]$Arguments)
    $json = @(& kubectl @Arguments -o json)
    if ($LASTEXITCODE -ne 0) {
        throw "kubectl $($Arguments -join ' ') failed"
    }
    return (($json -join [Environment]::NewLine) | ConvertFrom-Json)
}

function Wait-ArgoApplication {
    param([Parameter(Mandatory)][string]$Name, [int]$TimeoutSeconds = 600)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $json = @(& kubectl get application.argoproj.io $Name -n argocd -o json 2>$null)
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($exitCode -eq 0 -and $json) {
            $application = (($json -join [Environment]::NewLine) | ConvertFrom-Json)
            if ($application.status.sync.status -eq 'Synced' -and $application.status.health.status -eq 'Healthy') {
                return $application
            }
        }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "Argo Application $Name did not become Synced and Healthy within $TimeoutSeconds seconds."
}

function Wait-SteadyStateReady {
    param([Parameter(Mandatory)][ValidateSet('team','application')][string]$Kind, [Parameter(Mandatory)][string]$Name, [string]$Namespace)
    $deadline = (Get-Date).AddSeconds(600)
    do {
        $resource = if ($Kind -eq 'application') { 'applications.platform.steadystate.dev' } else { 'teams.platform.steadystate.dev' }
        $arguments = @('get', $resource, $Name)
        if ($Namespace) { $arguments += @('-n', $Namespace) }
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $ready = & kubectl @arguments -o "jsonpath={.status.conditions[?(@.type=='Ready')].status}" 2>$null
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($exitCode -eq 0 -and $ready -eq 'True') { return }
        Start-Sleep -Seconds 3
    } while ((Get-Date) -lt $deadline)
    throw "$Kind $Name did not reach Ready=True."
}

function Wait-ArgoRoute {
    $deadline = (Get-Date).AddSeconds(180)
    do {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $json = @(& kubectl get httproute argocd -n argocd -o json 2>$null)
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($exitCode -eq 0 -and $json) {
            $route = (($json -join [Environment]::NewLine) | ConvertFrom-Json)
            $conditions = @($route.status.parents.conditions)
            $accepted = $conditions | Where-Object { $_.type -eq 'Accepted' -and $_.status -eq 'True' }
            $resolved = $conditions | Where-Object { $_.type -eq 'ResolvedRefs' -and $_.status -eq 'True' }
            if ($accepted -and $resolved) { return }
        }
        Start-Sleep -Seconds 3
    } while ((Get-Date) -lt $deadline)
    throw 'The Argo CD HTTPRoute was not accepted with resolved references.'
}

function Test-ArgoHttp {
    $deadline = (Get-Date).AddSeconds(120)
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/" -Headers @{ Host = 'argocd.localtest.me' } -TimeoutSec 5
            if ($response.StatusCode -eq 200) { return }
        } catch {
            Start-Sleep -Seconds 3
        }
    } while ((Get-Date) -lt $deadline)
    throw 'Argo CD did not respond through the shared Gateway HTTPRoute.'
}

function Invoke-Deploy {
    Assert-Commands @('kubectl','helm')
    if (-not $GitRevision) { throw 'GitRevision must not be empty.' }
    $manifest = Get-ArgoManifest
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & kubectl get namespace argocd *> $null
    $namespaceExists = $LASTEXITCODE -eq 0
    $ErrorActionPreference = $previousPreference
    if (-not $namespaceExists) {
        Invoke-External kubectl create namespace argocd
    }

    Invoke-External kubectl apply --server-side --force-conflicts -n argocd -f $manifest
    Remove-Dex
    Assert-DexAbsent
    Invoke-External kubectl apply --server-side --force-conflicts -k $PlatformPath
    Invoke-External kubectl rollout restart deployment/argocd-server -n argocd
    Wait-ArgoWorkloads

    $projects = Join-Path $ArtifactRoot 'bootstrap-projects.yaml'
    $rootApplication = Join-Path $ArtifactRoot 'bootstrap-root-application.yaml'
    Render-RootTemplate -Template 'templates/projects.yaml.tpl' -Path $projects
    Render-RootTemplate -Template 'templates/root-application.yaml.tpl' -Path $rootApplication -BootstrapRoot
    Invoke-External kubectl apply -f $projects
    Invoke-External kubectl apply -f $rootApplication
    Write-Host "Argo CD and the SteadyState GitOps root are deployed at revision '$GitRevision'."
}

function Add-PassedCheck {
    param(
        [AllowEmptyCollection()]
        [Parameter(Mandatory)][System.Collections.Generic.List[object]]$Checks,
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][datetime]$Started,
        [Parameter(Mandatory)][string]$Details
    )
    $Checks.Add([ordered]@{
        name = $Name
        status = 'passed'
        elapsedSeconds = [Math]::Round(((Get-Date) - $Started).TotalSeconds, 3)
        details = $Details
    })
}

function Invoke-Test {
    Assert-Commands @('kubectl')
    $manifest = Get-ArgoManifest
    $checks = [System.Collections.Generic.List[object]]::new()
    $startedAt = (Get-Date).ToUniversalTime()

    $started = Get-Date
    Wait-ArgoWorkloads
    Assert-DexAbsent
    Add-PassedCheck $checks 'pinned-argocd-ready-dex-absent' $started "Pinned manifest $(Split-Path -Leaf $manifest) is running and all five Dex objects are absent."

    $applications = @{}
    foreach ($name in @('argocd-configuration','monitoring','argo-rollouts','loki','tempo','otel-collector','alloy','kyverno','kyverno-policies','steadystate-operator','payments','steadystate-root')) {
        $started = Get-Date
        $applications[$name] = Wait-ArgoApplication -Name $name
        Add-PassedCheck $checks "argocd-application-$name-healthy" $started "$name is Synced and Healthy."
    }

    $started = Get-Date
    Wait-SteadyStateReady -Kind team -Name payments
    Wait-SteadyStateReady -Kind application -Name demo -Namespace team-payments
    Add-PassedCheck $checks 'gitops-team-and-application-ready' $started 'The Team became healthy before its namespaced Application.'

    $started = Get-Date
    Wait-ArgoRoute
    Test-ArgoHttp
    Add-PassedCheck $checks 'argocd-ui-gateway-reachable' $started 'Argo CD returned HTTP 200 through the shared Gateway without exposing credentials.'

    $rootApplication = $applications['steadystate-root']
    $resolvedRevision = [string]$rootApplication.status.sync.revision
    if ($resolvedRevision -notmatch '^([0-9a-f]{40}|[0-9a-f]{64})$') {
        throw "Root Application reported invalid resolved revision '$resolvedRevision'."
    }
    $application = Get-KubernetesObject @('get','applications.platform.steadystate.dev','demo','-n','team-payments')
    $annotatedRevision = [string]$application.metadata.annotations.'steadystate.dev/source-revision'
    if ($annotatedRevision -ne $resolvedRevision -or $application.status.resolvedGitRevision -ne $resolvedRevision) {
        throw 'The SteadyState Application annotation/status revision does not match the root resolved commit.'
    }
    if ([string]$application.status.resolvedImageDigest -notmatch '^sha256:[0-9a-f]{64}$') {
        throw 'The SteadyState Application did not report a canonical runtime digest.'
    }
    Add-PassedCheck $checks 'runtime-provenance-matches-root-revision' (Get-Date) 'Runtime digest is canonical and resolvedGitRevision matches the root commit.'

    $tracking = & kubectl get configmap argocd-cm -n argocd -o "jsonpath={.data.application\.resourceTrackingMethod}"
    if ($LASTEXITCODE -ne 0 -or $tracking -ne 'annotation') {
        throw 'Argo CD annotation resource tracking is not frozen.'
    }
    Add-PassedCheck $checks 'annotation-resource-tracking' (Get-Date) 'Argo CD resource tracking is explicitly annotation-based.'

    $versions = Read-Versions
    $evidence = [ordered]@{
        schemaVersion = 1
        result = 'passed'
        startedAt = $startedAt.ToString('o')
        completedAt = (Get-Date).ToUniversalTime().ToString('o')
        profile = $Profile
        argoCDVersion = $versions.ARGO_CD_VERSION
        argoManifestSha256 = $versions.ARGO_CD_MANIFEST_SHA256
        requestedGitRevision = $GitRevision
        resolvedGitRevision = $resolvedRevision
        application = 'team-payments/demo'
        resolvedImageDigest = [string]$application.status.resolvedImageDigest
        checks = $checks
    }
    if (-not $EvidencePath) {
        $EvidencePath = Join-Path $ArtifactRoot 'baseline.json'
    }
    Write-Utf8 -Path $EvidencePath -Content (($evidence | ConvertTo-Json -Depth 8) + [Environment]::NewLine)
    Write-Host "GitOps baseline passed with $($checks.Count) checks. Evidence: $EvidencePath"
}

function Invoke-Verify {
    Assert-Commands @('go','helm','kustomize')
    $versions = Read-Versions
    Assert-ChartChecksum 'https://grafana-community.github.io/helm-charts' 'loki' $versions.LOKI_CHART_VERSION $versions.LOKI_CHART_SHA256
    Assert-ChartChecksum 'https://grafana.github.io/helm-charts' 'alloy' $versions.ALLOY_CHART_VERSION $versions.ALLOY_CHART_SHA256
    Assert-ChartChecksum 'https://grafana.github.io/helm-charts' 'tempo' $versions.TEMPO_CHART_VERSION $versions.TEMPO_CHART_SHA256
    Assert-ChartChecksum 'https://open-telemetry.github.io/opentelemetry-helm-charts' 'opentelemetry-collector' $versions.OTEL_COLLECTOR_CHART_VERSION $versions.OTEL_COLLECTOR_CHART_SHA256
    Assert-ChartChecksum 'https://kyverno.github.io/kyverno/' 'kyverno' $versions.KYVERNO_CHART_VERSION $versions.KYVERNO_CHART_SHA256
    Push-Location $Root
    try {
        Invoke-External go test ./tests/gitops/...
    } finally {
        Pop-Location
    }
    Write-Host 'GitOps Helm and Kustomize rendering contracts are verified.'
}

function Invoke-Undeploy {
    Assert-Commands @('kubectl')
    $manifest = Get-ArgoManifest
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & kubectl get customresourcedefinition applications.argoproj.io *> $null
    $argoApplicationsExist = $LASTEXITCODE -eq 0
    & kubectl get customresourcedefinition applications.platform.steadystate.dev *> $null
    $steadyStateApplicationsExist = $LASTEXITCODE -eq 0
    & kubectl get customresourcedefinition teams.platform.steadystate.dev *> $null
    $teamsExist = $LASTEXITCODE -eq 0
    $ErrorActionPreference = $previousPreference

    if ($argoApplicationsExist) {
        Invoke-External kubectl delete application.argoproj.io steadystate-root -n argocd --ignore-not-found=true --wait=true --timeout=60s
        Invoke-External kubectl delete application.argoproj.io payments kyverno-policies alloy otel-collector tempo loki monitoring argo-rollouts -n argocd --ignore-not-found=true --wait=true --timeout=180s
        Invoke-External kubectl delete application.argoproj.io kyverno -n argocd --ignore-not-found=true --wait=true --timeout=180s
    }
    if ($steadyStateApplicationsExist) {
        Invoke-External kubectl delete applications.platform.steadystate.dev --all --all-namespaces --ignore-not-found=true --wait=true --timeout=180s
    }
    if ($teamsExist) {
        Invoke-External kubectl delete teams.platform.steadystate.dev --all --ignore-not-found=true --wait=true --timeout=180s
    }
    Invoke-External kubectl delete namespace steadystate-unmanaged --ignore-not-found=true --wait=true --timeout=120s
    if ($argoApplicationsExist) {
        Invoke-External kubectl delete application.argoproj.io argocd-configuration steadystate-operator -n argocd --ignore-not-found=true --wait=true --timeout=60s
    }
    Invoke-External kubectl delete -k (Join-Path $Root 'config/default') --ignore-not-found=true --wait=true --timeout=180s
    Invoke-External kubectl delete validatingwebhookconfiguration,mutatingwebhookconfiguration -l app.kubernetes.io/part-of=kyverno --ignore-not-found=true --wait=true --timeout=60s
    Invoke-External kubectl delete namespace monitoring argo-rollouts kyverno --ignore-not-found=true --wait=true --timeout=180s
    Invoke-External kubectl delete customresourcedefinition `
        rollouts.argoproj.io analysisruns.argoproj.io analysistemplates.argoproj.io clusteranalysistemplates.argoproj.io experiments.argoproj.io `
        alertmanagerconfigs.monitoring.coreos.com alertmanagers.monitoring.coreos.com podmonitors.monitoring.coreos.com probes.monitoring.coreos.com `
        prometheusagents.monitoring.coreos.com prometheuses.monitoring.coreos.com prometheusrules.monitoring.coreos.com scrapeconfigs.monitoring.coreos.com `
        servicemonitors.monitoring.coreos.com thanosrulers.monitoring.coreos.com `
        cleanuppolicies.kyverno.io clustercleanuppolicies.kyverno.io clusterephemeralreports.reports.kyverno.io clusterpolicies.kyverno.io `
        clusterpolicyreports.wgpolicyk8s.io deletingpolicies.policies.kyverno.io ephemeralreports.reports.kyverno.io generatingpolicies.policies.kyverno.io `
        globalcontextentries.kyverno.io imagevalidatingpolicies.policies.kyverno.io mutatingpolicies.policies.kyverno.io `
        namespaceddeletingpolicies.policies.kyverno.io namespacedgeneratingpolicies.policies.kyverno.io namespacedimagevalidatingpolicies.policies.kyverno.io `
        namespacedmutatingpolicies.policies.kyverno.io namespacedvalidatingpolicies.policies.kyverno.io policies.kyverno.io `
        policyexceptions.kyverno.io policyexceptions.policies.kyverno.io policyreports.wgpolicyk8s.io updaterequests.kyverno.io validatingpolicies.policies.kyverno.io `
        --ignore-not-found=true --wait=true --timeout=180s
    Invoke-External kubectl delete -n argocd -f $manifest --ignore-not-found=true --wait=false
    Invoke-External kubectl delete namespace argocd --ignore-not-found=true --wait=true --timeout=180s
    Write-Host 'Argo CD and SteadyState GitOps-managed demo resources are undeployed.'
}

Push-Location $Root
try {
    switch ($Mode) {
        'Deploy' { Invoke-Deploy }
        'Test' { Invoke-Test }
        'Undeploy' { Invoke-Undeploy }
        'Verify' { Invoke-Verify }
    }
} finally {
    Pop-Location
}
