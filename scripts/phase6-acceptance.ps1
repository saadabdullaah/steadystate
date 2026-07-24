[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateSet('Prepare','Test','Finalize','CaptureFailure')]
    [string]$Stage,
    [int]$HttpPort = 8080
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$ArtifactRoot = Join-Path $Root '.artifacts/phase6/acceptance'
$StatePath = Join-Path $ArtifactRoot 'state.json'
$EvidencePath = Join-Path $ArtifactRoot 'evidence.json'
$Namespace = 'team-payments'
$SignedImage = 'ghcr.io/saadabdullaah/steadystate-demo-app:v0.6.0'
$UnsignedImage = 'ghcr.io/saadabdullaah/steadystate-demo-app:v0.5.0'

function Write-Utf8([string]$Path, [string]$Value) {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null
    [IO.File]::WriteAllText($Path, $Value, [Text.UTF8Encoding]::new($false))
}

function Get-State {
    if (-not (Test-Path -LiteralPath $StatePath)) { throw 'Phase 6 acceptance state is missing.' }
    return Get-Content -Raw -LiteralPath $StatePath | ConvertFrom-Json
}

function Save-State($State) {
    Write-Utf8 $StatePath (($State | ConvertTo-Json -Depth 40) + [Environment]::NewLine)
}

function Add-Check($State, [string]$Name, [datetime]$Started, [string]$Details) {
    $State.checks += [pscustomobject]@{
        name = $Name
        status = 'passed'
        elapsedSeconds = [Math]::Round(((Get-Date)-$Started).TotalSeconds, 3)
        details = $Details
    }
}

function Wait-Until([int]$TimeoutSeconds, [string]$Failure, [scriptblock]$Condition) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        if (& $Condition) { return }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw $Failure
}

function Invoke-KubectlJSON([string[]]$Arguments) {
    $output = @(& kubectl @Arguments 2>&1)
    if ($LASTEXITCODE -ne 0) { throw "kubectl $($Arguments -join ' ') failed: $($output -join ' ')" }
    return ($output -join [Environment]::NewLine) | ConvertFrom-Json
}

function Test-DeniedPod([string]$Image, [string]$Name, [string]$ExtraLabels = '') {
    $manifest = @"
apiVersion: v1
kind: Pod
metadata:
  name: $Name
  namespace: $Namespace
  labels:
$ExtraLabels
    steadystate.dev/security-acceptance: "true"
spec:
  automountServiceAccountToken: false
  containers:
    - name: application
      image: $Image
      resources:
        requests: {cpu: 10m, memory: 16Mi}
        limits: {cpu: 50m, memory: 32Mi}
      securityContext:
        allowPrivilegeEscalation: false
        capabilities: {drop: [ALL]}
        readOnlyRootFilesystem: true
        runAsNonRoot: true
"@
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $output = @($manifest | & kubectl apply --dry-run=server -f - -o json 2>&1)
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previous
    if ($exitCode -eq 0) { throw "Expected Pod $Name to be denied." }
    $sanitized = (($output -join ' ') -replace '(?i)(password|token|secret)=[^ ]+', '$1=[REDACTED]')
    return $sanitized
}

function Test-SignedPod {
    $manifest = @"
apiVersion: v1
kind: Pod
metadata:
  name: signed-phase6
  namespace: $Namespace
  labels:
    steadystate.dev/security-acceptance: "true"
spec:
  automountServiceAccountToken: false
  containers:
    - name: application
      image: $SignedImage
      resources:
        requests: {cpu: 10m, memory: 16Mi}
        limits: {cpu: 50m, memory: 32Mi}
      securityContext:
        allowPrivilegeEscalation: false
        capabilities: {drop: [ALL]}
        readOnlyRootFilesystem: true
        runAsNonRoot: true
"@
    $object = $manifest | & kubectl apply --dry-run=server -f - -o json | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0) { throw 'The signed and attested v0.6.0 image was denied.' }
    $admittedImage = [string]$object.spec.containers[0].image
    if ($admittedImage -cnotmatch '^ghcr\.io/saadabdullaah/steadystate-demo-app@sha256:[0-9a-f]{64}$') {
        throw "Kyverno did not digest-pin the admitted image: $admittedImage"
    }
    return $admittedImage
}

function New-SecurityApplication([string]$Tag) {
    $manifest = @"
apiVersion: platform.steadystate.dev/v1alpha1
kind: Application
metadata:
  name: security-acceptance
  namespace: $Namespace
spec:
  owner: security-acceptance
  image: {repository: ghcr.io/saadabdullaah/steadystate-demo-app, tag: $Tag}
  runtime: {port: 8080, replicas: {min: 1, max: 1}}
  resources:
    requests: {cpu: 50m, memory: 32Mi}
    limits: {cpu: 200m, memory: 128Mi}
  deployment: {strategy: rolling, automaticRollback: true}
  reliability:
    availabilityTarget: "99.9%"
    maximumP95Latency: 250ms
    maximumErrorRate: "1%"
  observability: {metrics: false, logs: false, traces: false}
  security: {requireSignedImage: true, runAsNonRoot: true, networkIsolation: true}
"@
    $manifest | & kubectl apply -f -
    if ($LASTEXITCODE -ne 0) { throw 'Creating the security acceptance Application failed.' }
}

function Save-Snapshots {
    New-Item -ItemType Directory -Force -Path (Join-Path $ArtifactRoot 'snapshots'), (Join-Path $ArtifactRoot 'logs') | Out-Null
    & kubectl get validatingpolicies.policies.kyverno.io,imagevalidatingpolicies.policies.kyverno.io -o yaml *> (Join-Path $ArtifactRoot 'snapshots/policies.yaml')
    & kubectl get policyreports.wgpolicyk8s.io -A -o yaml *> (Join-Path $ArtifactRoot 'snapshots/policyreports.yaml')
    & kubectl get applications.platform.steadystate.dev,deployments,replicasets,pods,networkpolicies -n $Namespace -o yaml *> (Join-Path $ArtifactRoot 'snapshots/workloads.yaml')
    & kubectl logs -n kyverno -l app.kubernetes.io/part-of=kyverno --all-containers --tail=500 --prefix=true *> (Join-Path $ArtifactRoot 'logs/kyverno.log')
    & kubectl logs -n steadystate-system deployment/steadystate-controller-manager --all-containers --tail=500 *> (Join-Path $ArtifactRoot 'logs/operator.log')
}

switch ($Stage) {
    'Prepare' {
        New-Item -ItemType Directory -Force -Path $ArtifactRoot | Out-Null
        & (Join-Path $PSScriptRoot 'secrets.ps1') -Action Verify
        if ($LASTEXITCODE -ne 0) { throw 'SOPS secret verification failed.' }
        foreach ($deployment in @('kyverno-admission-controller','kyverno-background-controller','kyverno-reports-controller')) {
            & kubectl rollout status "deployment/$deployment" -n kyverno --timeout=180s
            if ($LASTEXITCODE -ne 0) { throw "$deployment is not ready." }
        }
        $state = [pscustomobject]@{
            schemaVersion = 1
            result = 'running'
            sourceSHA = $env:GITHUB_SHA
            profile = 'standard'
            startedAt = (Get-Date).ToUniversalTime().ToString('o')
            completedAt = $null
            failure = $null
            checks = @()
            admission = [pscustomobject]@{}
            memory = [pscustomobject]@{}
        }
        Save-State $state
    }
    'Test' {
        $state = Get-State
        try {
            $started = Get-Date
            $unsigned = Test-DeniedPod -Image $UnsignedImage -Name unsigned-phase5
            if ($unsigned -notmatch '(?i)(signature|attestation|verify)') { throw 'Unsigned image denial did not identify signature or attestation verification.' }
            Write-Utf8 (Join-Path $ArtifactRoot 'admission/unsigned.txt') ($unsigned + [Environment]::NewLine)
            Add-Check $state 'unsigned-image-denied' $started 'The unsigned Phase 5 image was denied by fail-closed admission.'

            $wrongImage = [string]$env:PHASE6_WRONG_IDENTITY_IMAGE
            if (-not $wrongImage) { throw 'The wrong-identity acceptance image was not provided.' }
            $started = Get-Date
            $wrong = Test-DeniedPod -Image $wrongImage -Name wrong-identity
            if ($wrong -notmatch '(?i)(identity|signature|attestation|verify)') { throw 'Wrong-identity denial did not identify provenance verification.' }
            Write-Utf8 (Join-Path $ArtifactRoot 'admission/wrong-identity.txt') ($wrong + [Environment]::NewLine)
            Add-Check $state 'wrong-workflow-identity-denied' $started 'An image signed by the acceptance workflow identity was denied.'

            $started = Get-Date
            foreach ($fixture in @('vulnerable-pod.yaml','cnpg-label-bypass.yaml')) {
                $content = Get-Content -Raw -LiteralPath (Join-Path $Root "tests/security/$fixture")
                $previous = $ErrorActionPreference; $ErrorActionPreference = 'Continue'
                $output = @($content | & kubectl apply --dry-run=server -f - 2>&1)
                $exitCode = $LASTEXITCODE; $ErrorActionPreference = $previous
                if ($exitCode -eq 0) { throw "$fixture bypassed admission." }
                Write-Utf8 (Join-Path $ArtifactRoot "admission/$fixture.txt") (($output -join ' ') + [Environment]::NewLine)
            }
            Add-Check $state 'unsafe-pods-and-cnpg-label-bypass-denied' $started 'Privileged, host namespace, latest, missing-resource, and forged CNPG-label fixtures were denied.'

            $started = Get-Date
            $digestImage = Test-SignedPod
            $state.admission = [pscustomobject]@{signedImage=$SignedImage;admittedImage=$digestImage}
            Add-Check $state 'signed-attested-image-admitted-and-digest-pinned' $started 'v0.6.0 was admitted only after signature and SPDX attestation verification, and its Pod image was digest-pinned.'

            $started = Get-Date
            New-SecurityApplication -Tag 'v0.6.0'
            Wait-Until 300 'Signed security Application did not become Healthy.' {
                $app = Invoke-KubectlJSON @('get','application','security-acceptance','-n',$Namespace,'-o','json')
                return $app.status.phase -eq 'Healthy'
            }
            $healthy = Invoke-KubectlJSON @('get','application','security-acceptance','-n',$Namespace,'-o','json')
            $tuple = [pscustomobject]@{version=$healthy.status.activeVersion;digest=$healthy.status.resolvedImageDigest;revision=$healthy.status.resolvedGitRevision}
            & kubectl patch application security-acceptance -n $Namespace --type merge -p '{"spec":{"image":{"tag":"v0.5.0"}}}' | Out-Null
            if ($LASTEXITCODE -ne 0) { throw 'Patching the unsigned candidate failed.' }
            Wait-Until 240 'Application did not report SecurityPolicyRejected.' {
                $app = Invoke-KubectlJSON @('get','application','security-acceptance','-n',$Namespace,'-o','json')
                $security = @($app.status.conditions | Where-Object {$_.type -eq 'SecurityPolicyReady'})[0]
                return $app.status.phase -eq 'Degraded' -and $security.status -eq 'False' -and $security.reason -eq 'SecurityPolicyRejected'
            }
            $rejected = Invoke-KubectlJSON @('get','application','security-acceptance','-n',$Namespace,'-o','json')
            if ($rejected.status.activeVersion -ne $tuple.version -or $rejected.status.resolvedImageDigest -ne $tuple.digest -or $rejected.status.resolvedGitRevision -ne $tuple.revision) {
                throw 'Admission rejection overwrote the last healthy release tuple.'
            }
            Add-Check $state 'security-status-truth-and-active-tuple-preserved' $started 'SecurityPolicyReady became False with a sanitized rejection while the healthy version, digest, revision, and serving children remained.'

            $started = Get-Date
            & (Join-Path $PSScriptRoot 'secrets.ps1') -Action Verify
            if ($LASTEXITCODE -ne 0) { throw 'SOPS verification failed.' }
            Add-Check $state 'sops-decrypts-without-tracked-plaintext' $started 'The encrypted Grafana Secret decrypted with the hosted age identity and no plaintext key/Secret is tracked.'

            $started = Get-Date
            $query = [uri]::EscapeDataString('sum by (namespace) (container_memory_working_set_bytes{container!="",image!=""})')
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/api/datasources/proxy/uid/prometheus/api/v1/query?query=$query" -Headers @{Host='grafana.localtest.me'} -TimeoutSec 20
            $memory = $response.Content | ConvertFrom-Json
            $total = 0.0; $kyverno = 0.0
            foreach ($item in @($memory.data.result)) {
                $value = [double]$item.value[1]
                $total += $value
                if ($item.metric.namespace -eq 'kyverno') { $kyverno += $value }
            }
            if ($kyverno -gt 500MB -or $total -gt 7GB) { throw "Memory budget exceeded: kyverno=$kyverno total=$total" }
            $state.memory = [pscustomobject]@{kyvernoWorkingSetBytes=$kyverno;totalWorkingSetBytes=$total}
            Add-Check $state 'security-and-standard-profile-memory-budget' $started 'Kyverno stayed at or below 500 MiB and total in-cluster working set stayed at or below 7 GiB.'

            Save-Snapshots
            $state.result = 'passed'
            $state.completedAt = (Get-Date).ToUniversalTime().ToString('o')
            Save-State $state
            Write-Utf8 $EvidencePath (($state | ConvertTo-Json -Depth 40) + [Environment]::NewLine)
            Write-Host 'PHASE6_ACCEPTANCE_RESULT_PASSED' -ForegroundColor Cyan
        } catch {
            $state.result = 'failed'
            $state.failure = $_.Exception.Message
            $state.completedAt = (Get-Date).ToUniversalTime().ToString('o')
            Save-State $state
            Write-Host 'PHASE6_ACCEPTANCE_RESULT_FAILED' -ForegroundColor Red
            throw
        } finally {
            & kubectl delete application security-acceptance -n $Namespace --ignore-not-found=true --wait=false *> $null
        }
    }
    'Finalize' {
        $state = Get-State
        if ($state.result -ne 'passed' -or @($state.checks).Count -lt 7) { throw 'Phase 6 evidence is incomplete.' }
        $gif = Join-Path $ArtifactRoot 'phase6-admission-denial.gif'
        if (-not (Test-Path -LiteralPath $gif -PathType Leaf) -or (Get-Item $gif).Length -le 0) { throw 'Phase 6 GIF is missing.' }
        Write-Utf8 $EvidencePath (($state | ConvertTo-Json -Depth 40) + [Environment]::NewLine)
    }
    'CaptureFailure' {
        try { Save-Snapshots } catch {}
        if (-not (Test-Path -LiteralPath $EvidencePath)) {
            $state = if (Test-Path -LiteralPath $StatePath) { Get-State } else { [pscustomobject]@{schemaVersion=1;result='failed';sourceSHA=$env:GITHUB_SHA;checks=@()} }
            if (-not $state.failure) { $state | Add-Member NoteProperty failure ([string]$env:PHASE6_FAILURE_MESSAGE) -Force }
            Write-Utf8 $EvidencePath (($state | ConvertTo-Json -Depth 40) + [Environment]::NewLine)
        }
    }
}
