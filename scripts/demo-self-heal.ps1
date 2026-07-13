[CmdletBinding()]
param(
    [int]$HttpPort = 8080,
    [string]$ApplicationName = 'demo',
    [string]$Namespace = 'steadystate-demo'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$Platform = if ($env:OS -eq 'Windows_NT') { 'windows-amd64' } else { 'linux-amd64' }
$env:PATH = "$(Join-Path $Root ".tools/go/$Platform/bin")$([IO.Path]::PathSeparator)$(Join-Path $Root ".tools/bin/$Platform")$([IO.Path]::PathSeparator)$env:PATH"

function Invoke-Kubectl {
    param([Parameter(ValueFromRemainingArguments)][string[]]$Arguments)
    & kubectl @Arguments
    if ($LASTEXITCODE -ne 0) { throw "kubectl exited with code $LASTEXITCODE" }
}

function Get-ResourceUid([string]$Kind, [string]$Name) {
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $uid = & kubectl get $Kind $Name -n $Namespace -o 'jsonpath={.metadata.uid}' 2>$null
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previousPreference
    if ($exitCode -ne 0) { return $null }
    return [string]$uid
}

function Wait-Recreated([string]$Kind, [string]$Name, [string]$PreviousUid, [int]$TimeoutSeconds = 30) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $uid = Get-ResourceUid $Kind $Name
        if ($uid -and $uid -ne $PreviousUid) { return $uid }
        Start-Sleep -Milliseconds 500
    } while ((Get-Date) -lt $deadline)
    throw "$Kind/$Name was not recreated within $TimeoutSeconds seconds."
}

function Wait-ApplicationReady([int]$TimeoutSeconds = 60) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $ready = & kubectl get application $ApplicationName -n $Namespace -o "jsonpath={.status.conditions[?(@.type=='Ready')].status}" 2>$null
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($exitCode -eq 0 -and $ready -eq 'True') { return }
        Start-Sleep -Seconds 1
    } while ((Get-Date) -lt $deadline)
    throw "Application did not return to Ready=True within $TimeoutSeconds seconds."
}

function Wait-RouteReady([int]$TimeoutSeconds = 60) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $json = & kubectl get httproute $ApplicationName -n $Namespace -o json 2>$null
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($exitCode -eq 0) {
            $route = $json | ConvertFrom-Json
            $conditions = @($route.status.parents | ForEach-Object { $_.conditions } | ForEach-Object { $_ })
            $accepted = $conditions | Where-Object { $_.type -eq 'Accepted' -and $_.status -eq 'True' }
            $resolved = $conditions | Where-Object { $_.type -eq 'ResolvedRefs' -and $_.status -eq 'True' }
            if ($accepted -and $resolved) { return }
        }
        Start-Sleep -Seconds 1
    } while ((Get-Date) -lt $deadline)
    throw 'Recreated HTTPRoute did not become Accepted=True and ResolvedRefs=True.'
}

function Wait-ChildrenAbsent([int]$TimeoutSeconds = 60) {
    $children = @(
        @('deployment', $ApplicationName),
        @('service', $ApplicationName),
        @('configmap', "$ApplicationName-config"),
        @('httproute', $ApplicationName)
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $remaining = @($children | Where-Object { Get-ResourceUid $_[0] $_[1] })
        if ($remaining.Count -eq 0) { return }
        Start-Sleep -Seconds 1
    } while ((Get-Date) -lt $deadline)
    throw 'Owned children remained after deleting the Application.'
}

Wait-ApplicationReady
Write-Host 'Application starts Ready=True.' -ForegroundColor Green

foreach ($child in @(
    @('deployment', $ApplicationName),
    @('service', $ApplicationName),
    @('configmap', "$ApplicationName-config"),
    @('httproute', $ApplicationName)
)) {
    $kind = $child[0]
    $name = $child[1]
    $uid = Get-ResourceUid $kind $name
    if (-not $uid) { throw "$kind/$name is missing before the self-heal test." }
    Invoke-Kubectl delete $kind $name -n $Namespace --wait=true
    $newUid = Wait-Recreated $kind $name $uid
    if ($kind -eq 'deployment') {
        Invoke-Kubectl rollout status "deployment/$ApplicationName" -n $Namespace --timeout=60s
    }
    if ($kind -eq 'httproute') { Wait-RouteReady }
    Start-Sleep -Seconds 1
    Wait-ApplicationReady
    Write-Host ("[PASS] {0}/{1} self-healed ({2} -> {3})" -f $kind, $name, $uid, $newUid) -ForegroundColor Green
}

$started = Get-Date
Invoke-Kubectl scale "deployment/$ApplicationName" -n $Namespace --replicas=7
$deadline = $started.AddSeconds(10)
do {
    $replicas = & kubectl get deployment $ApplicationName -n $Namespace -o 'jsonpath={.spec.replicas}'
    if ($LASTEXITCODE -eq 0 -and [int]$replicas -eq 1) { break }
    Start-Sleep -Milliseconds 250
} while ((Get-Date) -lt $deadline)
if ([int]$replicas -ne 1) { throw 'Deployment replica drift was not repaired within 10 seconds.' }
$repairTime = ((Get-Date) - $started).TotalSeconds
Write-Host ("[PASS] Deployment replica drift repaired in {0:n2} seconds" -f $repairTime) -ForegroundColor Green

$response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/" -Headers @{ Host = "$ApplicationName.$Namespace.steadystate.localtest.me" } -TimeoutSec 5
if ($response.StatusCode -ne 200) { throw 'Application did not respond through the shared Gateway after self-healing.' }
Write-Host '[PASS] Application remains reachable through Envoy Gateway.' -ForegroundColor Green

Invoke-Kubectl delete application $ApplicationName -n $Namespace --wait=true --timeout=120s
Wait-ChildrenAbsent
Write-Host '[PASS] Application deletion released the finalizer and garbage-collected every child.' -ForegroundColor Green
Write-Host 'SteadyState Application self-heal demonstration passed.' -ForegroundColor Cyan
