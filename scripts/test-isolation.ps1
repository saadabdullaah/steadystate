[CmdletBinding()]
param(
    [int]$HttpPort = 8080,
    [ValidateSet('minimal','standard','full')]
    [string]$Profile = 'standard',
    [string]$EvidencePath
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$Platform = if ($env:OS -eq 'Windows_NT') { 'windows-amd64' } else { 'linux-amd64' }
$env:PATH = "$(Join-Path $Root ".tools/go/$Platform/bin")$([IO.Path]::PathSeparator)$(Join-Path $Root ".tools/bin/$Platform")$([IO.Path]::PathSeparator)$env:PATH"
$startedAt = Get-Date
$checks = @()

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

function Add-PassedCheck {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][double]$ElapsedSeconds,
        [Parameter(Mandatory)][string]$Details
    )
    $script:checks += [ordered]@{
        name = $Name
        status = 'passed'
        elapsedSeconds = [Math]::Round($ElapsedSeconds, 3)
        details = $Details
    }
    Write-Host "[PASS] $Name - $Details" -ForegroundColor Green
}

function Invoke-Kubectl {
    param([Parameter(ValueFromRemainingArguments)][string[]]$Arguments)
    & kubectl @Arguments
    if ($LASTEXITCODE -ne 0) { throw "kubectl exited with code $LASTEXITCODE" }
}

function Invoke-KubectlResult {
    [CmdletBinding(PositionalBinding=$false)]
    param(
        [Parameter(ValueFromRemainingArguments)][string[]]$Arguments,
        [Alias('o')][string]$OutputFormat
    )
    if ($PSBoundParameters.ContainsKey('OutputFormat')) { $Arguments += @('-o', $OutputFormat) }
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $output = @(& kubectl @Arguments 2>&1)
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previousPreference
    return [pscustomobject]@{
        ExitCode = $exitCode
        Output = (($output | Out-String).Trim())
    }
}

function Render-Fixture {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][hashtable]$Replacements
    )
    $source = Join-Path $Root "tests/isolation/$Name"
    $content = Get-Content -Raw -LiteralPath $source -Encoding UTF8
    foreach ($key in $Replacements.Keys) {
        $content = $content.Replace("__${key}__", [string]$Replacements[$key])
    }
    if ($content -match '__[A-Z0-9_]+__') {
        throw "Fixture $Name contains an unresolved placeholder: $($Matches[0])"
    }
    $targetDirectory = Join-Path $Root '.artifacts/phase2/rendered'
    New-Item -ItemType Directory -Force -Path $targetDirectory | Out-Null
    $target = Join-Path $targetDirectory $Name
    [IO.File]::WriteAllText($target, $content, [Text.UTF8Encoding]::new($false))
    return $target
}

function Wait-TeamReady {
    param([Parameter(Mandatory)][string]$Name, [int]$TimeoutSeconds = 90)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $result = Invoke-KubectlResult get team $Name -o "jsonpath={.status.conditions[?(@.type=='Ready')].status}"
        if ($result.ExitCode -eq 0 -and $result.Output -eq 'True') { return }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw "Team $Name did not reach Ready=True within $TimeoutSeconds seconds."
}

function Wait-ApplicationReady {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$Namespace,
        [int]$TimeoutSeconds = 90
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $result = Invoke-KubectlResult get application $Name -n $Namespace -o "jsonpath={.status.conditions[?(@.type=='Ready')].status}"
        if ($result.ExitCode -eq 0 -and $result.Output -eq 'True') { return }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw "Application $Namespace/$Name did not reach Ready=True within $TimeoutSeconds seconds."
}

function Wait-ApplicationCondition {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$Namespace,
        [Parameter(Mandatory)][string]$Type,
        [Parameter(Mandatory)][string]$Status,
        [Parameter(Mandatory)][string]$Reason,
        [int]$TimeoutSeconds = 60
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $result = Invoke-KubectlResult get application $Name -n $Namespace -o json
        if ($result.ExitCode -eq 0) {
            $application = $result.Output | ConvertFrom-Json
            $condition = @($application.status.conditions | Where-Object {
                $_.type -eq $Type -and $_.status -eq $Status -and $_.reason -eq $Reason -and $_.observedGeneration -eq $application.metadata.generation
            })
            if ($condition.Count -eq 1) { return $condition[0] }
        }
        Start-Sleep -Seconds 1
    } while ((Get-Date) -lt $deadline)
    throw "Application $Namespace/$Name did not report $Type=$Status reason=$Reason within $TimeoutSeconds seconds."
}

function Assert-NoApplicationChildren {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$Namespace
    )
    foreach ($child in @(
        @('deployment', $Name),
        @('service', $Name),
        @('configmap', "$Name-config"),
        @('httproute', $Name)
    )) {
        $result = Invoke-KubectlResult get $child[0] $child[1] -n $Namespace
        if ($result.ExitCode -eq 0) { throw "Rejected Application unexpectedly created $($child[0])/$($child[1])." }
    }
}

function Wait-GatewayApplication {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$Namespace,
        [Parameter(Mandatory)][string]$Version,
        [int]$TimeoutSeconds = 60
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/" -Headers @{ Host = "$Name.$Namespace.steadystate.localtest.me" } -TimeoutSec 5
            if ($response.StatusCode -eq 200) {
                $body = $response.Content | ConvertFrom-Json
                if ($body.application -eq $Name -and $body.namespace -eq $Namespace -and $body.version -eq $Version) { return }
            }
        } catch {
            # The shared Gateway and EndpointSlice can converge after Ready is first observed.
        }
        Start-Sleep -Seconds 2
    } while ((Get-Date) -lt $deadline)
    throw "Application $Namespace/$Name did not return the expected response through the shared Gateway."
}

$versions = Read-Versions
$demoImage = $versions.DEMO_IMAGE
$clientImage = $versions.ISOLATION_CLIENT_IMAGE
if (-not $demoImage -or $demoImage -notmatch '^(.+):([^/:@]+)$') {
    throw 'DEMO_IMAGE must contain an explicit repository and tag.'
}
$demoRepository = $Matches[1]
$demoTag = $Matches[2]
if (-not $clientImage -or $clientImage -notmatch '@sha256:[0-9a-f]{64}$') {
    throw 'ISOLATION_CLIENT_IMAGE must be pinned by sha256 digest.'
}
$replacements = @{
    DEMO_REPOSITORY = $demoRepository
    DEMO_TAG = $demoTag
    ISOLATION_CLIENT_IMAGE = $clientImage
}

$calicoStarted = Get-Date
$calicoResult = Invoke-KubectlResult get daemonset calico-node -n calico-system -o json
if ($calicoResult.ExitCode -ne 0) { throw 'Calico is absent; isolation results would be invalid.' }
$calico = $calicoResult.Output | ConvertFrom-Json
if ([int]$calico.status.desiredNumberScheduled -lt 1 -or [int]$calico.status.numberReady -ne [int]$calico.status.desiredNumberScheduled) {
    throw 'Calico is not fully ready; isolation results would be invalid.'
}
Add-PassedCheck -Name 'calico-enforcement-present' -ElapsedSeconds ((Get-Date) - $calicoStarted).TotalSeconds -Details "$($calico.status.numberReady) Calico nodes are ready."

Invoke-Kubectl delete team payments orders --ignore-not-found=true --wait=true --timeout=180s
Invoke-Kubectl delete namespace steadystate-unmanaged --ignore-not-found=true --wait=true --timeout=120s

$teamsPath = Render-Fixture -Name 'teams.yaml' -Replacements $replacements
$workloadsPath = Render-Fixture -Name 'workloads.yaml' -Replacements $replacements
$repositoryRejectedPath = Render-Fixture -Name 'repository-rejected.yaml' -Replacements $replacements
$unmanagedPath = Render-Fixture -Name 'unmanaged-application.yaml' -Replacements $replacements
$quotaPath = Render-Fixture -Name 'quota-violation.yaml' -Replacements $replacements

$boundaryStarted = Get-Date
Invoke-Kubectl apply -f $teamsPath
Wait-TeamReady -Name payments
Wait-TeamReady -Name orders
foreach ($namespace in @('team-payments', 'team-orders')) {
    foreach ($policy in @('steadystate-default-deny', 'steadystate-allow-dns', 'steadystate-allow-envoy-gateway')) {
        Invoke-Kubectl get networkpolicy $policy -n $namespace
    }
}
Add-PassedCheck -Name 'team-boundaries-ready' -ElapsedSeconds ((Get-Date) - $boundaryStarted).TotalSeconds -Details 'Both Team namespaces report Ready with quota, RBAC, and all three NetworkPolicies.'

$concurrentStarted = Get-Date
Invoke-Kubectl apply -f $workloadsPath
Wait-ApplicationReady -Name payments-api -Namespace team-payments
Wait-ApplicationReady -Name orders-api -Namespace team-orders
Invoke-Kubectl wait pod/isolation-client -n team-orders --for=condition=Ready --timeout=120s
Add-PassedCheck -Name 'concurrent-applications-ready' -ElapsedSeconds ((Get-Date) - $concurrentStarted).TotalSeconds -Details 'Payments and orders Applications are simultaneously Ready within independent quotas.'

Start-Sleep -Seconds 5
$networkStarted = Get-Date
$clusterIPResult = Invoke-KubectlResult get service payments-api -n team-payments -o 'jsonpath={.spec.clusterIP}'
if ($clusterIPResult.ExitCode -ne 0 -or -not $clusterIPResult.Output) { throw 'payments-api Service has no ClusterIP.' }
$directResult = Invoke-KubectlResult exec -n team-orders isolation-client '--' curl --fail --silent --show-error --connect-timeout 3 --max-time 5 "http://$($clusterIPResult.Output)/healthz"
if ($directResult.ExitCode -ne 28) {
    throw "Cross-team Service traffic was not denied by timeout (exit=$($directResult.ExitCode)): $($directResult.Output)"
}
Add-PassedCheck -Name 'cross-team-service-denied' -ElapsedSeconds ((Get-Date) - $networkStarted).TotalSeconds -Details 'The orders Pod timed out when connecting directly to the payments Service ClusterIP.'

$gatewayStarted = Get-Date
Wait-GatewayApplication -Name payments-api -Namespace team-payments -Version $demoTag
Wait-GatewayApplication -Name orders-api -Namespace team-orders -Version $demoTag
Add-PassedCheck -Name 'gateway-route-succeeds' -ElapsedSeconds ((Get-Date) - $gatewayStarted).TotalSeconds -Details 'Both isolated Applications returned their exact identities through the shared Envoy Gateway.'

$rbacStarted = Get-Date
$ordersIdentity = 'system:serviceaccount:team-orders:steadystate-team-owner'
$ownSecrets = Invoke-KubectlResult auth can-i get secrets -n team-orders --as $ordersIdentity
$paymentSecrets = Invoke-KubectlResult auth can-i get secrets -n team-payments --as $ordersIdentity
if ($ownSecrets.ExitCode -ne 0 -or $ownSecrets.Output -ne 'yes') { throw 'The orders Team owner cannot read Secrets in its own namespace.' }
if ($paymentSecrets.Output -ne 'no') { throw 'The orders Team owner can read Secrets in the payments namespace.' }
Add-PassedCheck -Name 'cross-team-rbac-denied' -ElapsedSeconds ((Get-Date) - $rbacStarted).TotalSeconds -Details 'The orders identity can read own-namespace Secrets but receives no access in team-payments.'

$repositoryStarted = Get-Date
Invoke-Kubectl apply -f $repositoryRejectedPath
Wait-ApplicationCondition -Name forbidden-payments-image -Namespace team-orders -Type ConfigurationReady -Status False -Reason RepositoryNotAllowed | Out-Null
Assert-NoApplicationChildren -Name forbidden-payments-image -Namespace team-orders
Add-PassedCheck -Name 'repository-authorization-rejected' -ElapsedSeconds ((Get-Date) - $repositoryStarted).TotalSeconds -Details 'The orders Team rejected a payments repository and created no child resources.'

$unmanagedStarted = Get-Date
Invoke-Kubectl apply -f $unmanagedPath
Wait-ApplicationCondition -Name unmanaged-api -Namespace steadystate-unmanaged -Type ConfigurationReady -Status False -Reason NamespaceNotManaged | Out-Null
Assert-NoApplicationChildren -Name unmanaged-api -Namespace steadystate-unmanaged
Add-PassedCheck -Name 'unmanaged-namespace-rejected' -ElapsedSeconds ((Get-Date) - $unmanagedStarted).TotalSeconds -Details 'An Application outside a verified Team namespace was rejected without child mutation.'

$quotaStarted = Get-Date
$quotaResult = Invoke-KubectlResult apply -f $quotaPath
if ($quotaResult.ExitCode -eq 0) { throw 'The quota-violating Pod was admitted.' }
if ($quotaResult.Output -notmatch '(?i)exceeded quota') { throw "Quota rejection did not come from ResourceQuota admission: $($quotaResult.Output)" }
$quotaPod = Invoke-KubectlResult get pod quota-violation -n team-orders
if ($quotaPod.ExitCode -eq 0) { throw 'The quota-violating Pod exists despite rejected admission.' }
Add-PassedCheck -Name 'quota-admission-rejected' -ElapsedSeconds ((Get-Date) - $quotaStarted).TotalSeconds -Details 'ResourceQuota admission rejected a Pod requesting twice the Team CPU and memory ceiling.'

$deletionStarted = Get-Date
$paymentsUID = (Invoke-KubectlResult get namespace team-payments -o 'jsonpath={.metadata.uid}').Output
if (-not $paymentsUID) { throw 'The payments namespace is absent before Team deletion proof.' }
Invoke-Kubectl delete team orders --wait=true --timeout=180s
$ordersNamespace = Invoke-KubectlResult get namespace team-orders
if ($ordersNamespace.ExitCode -eq 0) { throw 'Deleting Team orders did not remove team-orders.' }
$currentPaymentsUID = (Invoke-KubectlResult get namespace team-payments -o 'jsonpath={.metadata.uid}').Output
if ($currentPaymentsUID -ne $paymentsUID) { throw 'Deleting Team orders replaced or removed the payments namespace.' }
Wait-TeamReady -Name payments
Wait-ApplicationReady -Name payments-api -Namespace team-payments
Wait-GatewayApplication -Name payments-api -Namespace team-payments -Version $demoTag
Add-PassedCheck -Name 'team-deletion-isolated' -ElapsedSeconds ((Get-Date) - $deletionStarted).TotalSeconds -Details 'Deleting orders removed its namespace while payments retained the same Namespace UID and remained reachable.'

if ($EvidencePath) {
    $resolvedEvidencePath = if ([IO.Path]::IsPathRooted($EvidencePath)) { [IO.Path]::GetFullPath($EvidencePath) } else { [IO.Path]::GetFullPath((Join-Path $Root $EvidencePath)) }
    $evidenceDirectory = Split-Path -Parent $resolvedEvidencePath
    if ($evidenceDirectory) { New-Item -ItemType Directory -Force -Path $evidenceDirectory | Out-Null }
    $evidence = [ordered]@{
        schemaVersion = 1
        result = 'passed'
        sourceRevision = if ($env:GITHUB_SHA) { $env:GITHUB_SHA } else { $null }
        profile = $Profile
        teams = @('payments', 'orders')
        startedAt = $startedAt.ToUniversalTime().ToString('o')
        completedAt = (Get-Date).ToUniversalTime().ToString('o')
        checks = $checks
    }
    $json = $evidence | ConvertTo-Json -Depth 6
    [IO.File]::WriteAllText($resolvedEvidencePath, "$json`n", [Text.UTF8Encoding]::new($false))
    Write-Host "Phase 2 evidence written to $resolvedEvidencePath" -ForegroundColor Green
}

Write-Host 'SteadyState Phase 2 cross-team isolation acceptance passed.' -ForegroundColor Cyan
