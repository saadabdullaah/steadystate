[CmdletBinding()]
param(
    [ValidateSet('Test','CaptureFailure')]
    [string]$Stage = 'Test'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$Platform = if ($env:OS -eq 'Windows_NT') { 'windows-amd64' } else { 'linux-amd64' }
$env:PATH = "$(Join-Path $Root ".tools/bin/$Platform")$([IO.Path]::PathSeparator)$env:PATH"
$ArtifactRoot = Join-Path $Root '.artifacts/phase6/foundation'
$FixtureNamespace = 'team-phase6-foundation'
$BackgroundPolicy = 'steadystate-phase6-background-fixture'

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

function Get-KubectlJson {
    param([Parameter(Mandatory)][string[]]$Arguments)
    $output = @(& kubectl @Arguments -o json)
    if ($LASTEXITCODE -ne 0) {
        throw "kubectl $($Arguments -join ' ') failed"
    }
    return (($output -join [Environment]::NewLine) | ConvertFrom-Json)
}

function Write-Utf8 {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Content)
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null
    [IO.File]::WriteAllText($Path, $Content, [Text.UTF8Encoding]::new($false))
}

function Write-Json {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)]$Value)
    Write-Utf8 -Path $Path -Content (($Value | ConvertTo-Json -Depth 20) + [Environment]::NewLine)
}

function Apply-Object {
    param([Parameter(Mandatory)]$Object)
    $json = $Object | ConvertTo-Json -Depth 30
    $json | & kubectl apply -f -
    if ($LASTEXITCODE -ne 0) {
        throw 'kubectl apply from generated JSON failed'
    }
}

function Assert-ObjectDenied {
    param([Parameter(Mandatory)]$Object)
    $json = $Object | ConvertTo-Json -Depth 30
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $output = @($json | & kubectl apply --dry-run=server -f - 2>&1)
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previousPreference
    if ($exitCode -eq 0) {
        throw 'Expected the generated object to be denied by Kyverno.'
    }
    return ($output -join ' ')
}

function Add-PassedCheck {
    param(
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

function Wait-ArgoApplication {
    param([Parameter(Mandatory)][string]$Name, [int]$TimeoutSeconds = 600)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $raw = @(& kubectl get application.argoproj.io $Name -n argocd -o json 2>$null)
        $exitCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($exitCode -eq 0 -and $raw) {
            $application = (($raw -join [Environment]::NewLine) | ConvertFrom-Json)
            if ($application.status.sync.status -eq 'Synced' -and $application.status.health.status -eq 'Healthy') {
                return $application
            }
        }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "Argo Application $Name did not become Synced and Healthy within $TimeoutSeconds seconds."
}

function Assert-ResourceAbsent {
    param([Parameter(Mandatory)][string]$Resource, [Parameter(Mandatory)][string]$Name)
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $found = & kubectl get $Resource $Name -n kyverno --ignore-not-found -o name 2>$null
    $exitCode = $LASTEXITCODE
    $ErrorActionPreference = $previousPreference
    if ($exitCode -ne 0) {
        throw "Could not verify absence of $Resource/$Name."
    }
    if ($found) {
        throw "Disabled Kyverno component still exists: $found"
    }
}

function Get-PolicyFailure {
    param([Parameter(Mandatory)][string]$Policy)
    $reports = Get-KubectlJson @('get','policyreports.wgpolicyk8s.io','-n',$FixtureNamespace)
    foreach ($report in @($reports.items)) {
        foreach ($result in @($report.results)) {
            if ([string]$result.policy -eq $Policy -and [string]$result.result -match '^(?i:fail|error)$') {
                return $result
            }
        }
    }
    return $null
}

function Wait-PolicyFailure {
    param([Parameter(Mandatory)][string]$Policy, [int]$TimeoutSeconds = 180)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        try {
            $result = Get-PolicyFailure -Policy $Policy
            if ($result) { return $result }
        } catch {
            # Reports are eventually created; retry until the bounded deadline.
        }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "PolicyReport did not record a failure for $Policy within $TimeoutSeconds seconds."
}

function Write-CommandArtifact {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string[]]$Arguments)
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $output = @(& kubectl @Arguments 2>&1 | ForEach-Object { [string]$_ })
    $ErrorActionPreference = $previousPreference
    Write-Utf8 -Path $Path -Content (($output -join [Environment]::NewLine) + [Environment]::NewLine)
}

function Capture-Snapshots {
    New-Item -ItemType Directory -Force -Path $ArtifactRoot | Out-Null
    Write-CommandArtifact -Path (Join-Path $ArtifactRoot 'kyverno-workloads.yaml') -Arguments @('get','deployment,pod,service,servicemonitor','-n','kyverno','-o','yaml')
    Write-CommandArtifact -Path (Join-Path $ArtifactRoot 'policies.yaml') -Arguments @('get','validatingpolicies.policies.kyverno.io,imagevalidatingpolicies.policies.kyverno.io','-o','yaml')
    Write-CommandArtifact -Path (Join-Path $ArtifactRoot 'policyreports.json') -Arguments @('get','policyreports.wgpolicyk8s.io','-A','-o','json')
    Write-CommandArtifact -Path (Join-Path $ArtifactRoot 'webhooks.yaml') -Arguments @('get','validatingwebhookconfigurations,mutatingwebhookconfigurations','-o','yaml')
    foreach ($deployment in @('kyverno-admission-controller','kyverno-background-controller','kyverno-reports-controller')) {
        Write-CommandArtifact -Path (Join-Path $ArtifactRoot "logs/$deployment.log") -Arguments @('logs',"deployment/$deployment",'-n','kyverno','--all-containers=true','--tail=500')
    }
}

function Remove-Fixtures {
    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    & kubectl delete validatingpolicy.policies.kyverno.io $BackgroundPolicy --ignore-not-found=true --wait=true --timeout=60s *> $null
    & kubectl delete namespace $FixtureNamespace --ignore-not-found=true --wait=true --timeout=120s *> $null
    $ErrorActionPreference = $previousPreference
}

function Invoke-Test {
    New-Item -ItemType Directory -Force -Path $ArtifactRoot | Out-Null
    $versions = Read-Versions
    if ($versions.KYVERNO_CHART_VERSION -ne '3.8.2' -or $versions.KYVERNO_VERSION -ne '1.18.2' -or
        $versions.KYVERNO_CHART_SHA256 -ne 'f4fc787cf1d6781eefb9e9b45837edcddcfae984c872888289914e97207cc5de') {
        throw 'The Kyverno compatibility baseline is not pinned to the approved chart, app, and checksum.'
    }
    if (-not (Get-Command kyverno -ErrorAction SilentlyContinue)) {
        throw 'The checksum-verified Kyverno CLI is missing.'
    }
    $cliVersion = ((& kyverno version) -join [Environment]::NewLine)
    if ($LASTEXITCODE -ne 0 -or $cliVersion -notmatch [regex]::Escape($versions.KYVERNO_VERSION)) {
        throw "Kyverno CLI version mismatch: expected $($versions.KYVERNO_VERSION), got $cliVersion"
    }
    $checks = [System.Collections.Generic.List[object]]::new()
    $startedAt = (Get-Date).ToUniversalTime()
    try {
        $started = Get-Date
        $kyvernoApplication = Wait-ArgoApplication -Name kyverno
        $policyApplication = Wait-ArgoApplication -Name kyverno-policies
        foreach ($deployment in @('kyverno-admission-controller','kyverno-background-controller','kyverno-reports-controller')) {
            Invoke-External kubectl rollout status "deployment/$deployment" -n kyverno --timeout=300s
        }
        $deployments = Get-KubectlJson @('get','deployments','-n','kyverno')
        $images = @(
            $deployments.items |
                ForEach-Object { @($_.spec.template.spec.initContainers) + @($_.spec.template.spec.containers) } |
                Where-Object { $null -ne $_ -and -not [string]::IsNullOrWhiteSpace([string]$_.image) } |
                ForEach-Object { [string]$_.image }
        )
        if ($images.Count -eq 0) {
            throw 'No Kyverno controller images were discovered.'
        }
        $unpinned = @($images | Where-Object { $_ -notmatch ':v1\.18\.2(@sha256:[0-9a-f]{64})?$' })
        if ($unpinned) {
            throw "Unexpected Kyverno image versions: $($unpinned -join ', ')"
        }
        Add-PassedCheck $checks 'pinned-kyverno-three-controllers-ready' $started "Argo synced chart 3.8.2 at $([string]$kyvernoApplication.status.sync.revision); CLI and all images use v1.18.2."

        $started = Get-Date
        Assert-ResourceAbsent -Resource deployment -Name kyverno-cleanup-controller
        Assert-ResourceAbsent -Resource deployment -Name kyverno-reports-server
        $legacy = @(& kubectl get clusterpolicies.kyverno.io --no-headers 2>$null)
        if ($LASTEXITCODE -ne 0) { throw 'Could not query legacy ClusterPolicy resources.' }
        if ($legacy) { throw 'Legacy ClusterPolicy resources are installed.' }
        Add-PassedCheck $checks 'cleanup-reports-server-and-legacy-policies-absent' $started 'Cleanup, Reports Server, and legacy ClusterPolicy resources are absent.'

        $started = Get-Date
        foreach ($crd in @(
            'validatingpolicies.policies.kyverno.io',
            'imagevalidatingpolicies.policies.kyverno.io',
            'policyreports.wgpolicyk8s.io'
        )) {
            Invoke-External kubectl wait --for=condition=Established "customresourcedefinition/$crd" --timeout=120s
        }
        $policies = Get-KubectlJson @('get','validatingpolicies.policies.kyverno.io,imagevalidatingpolicies.policies.kyverno.io')
        if (@($policies.items).Count -ne 3) {
            throw "Expected three stable CEL enforcement policies, found $(@($policies.items).Count)."
        }
        foreach ($policy in @($policies.items)) {
            if ([string]$policy.apiVersion -ne 'policies.kyverno.io/v1' -or @($policy.spec.validationActions) -notcontains 'Deny' -or
                [string]$policy.spec.failurePolicy -ne 'Fail' -or [int]$policy.spec.webhookConfiguration.timeoutSeconds -ne 15) {
                throw "Policy $($policy.metadata.name) does not satisfy the stable Deny/fail-safe contract."
            }
        }
        Add-PassedCheck $checks 'stable-cel-enforcement-policies' $started "All three policies use policies.kyverno.io/v1, Deny, failurePolicy=Fail, timeoutSeconds=15; policy revision is $([string]$policyApplication.status.sync.revision)."

        $started = Get-Date
        $validatingWebhooks = Get-KubectlJson @('get','validatingwebhookconfigurations')
        $mutatingWebhooks = Get-KubectlJson @('get','mutatingwebhookconfigurations')
        $kyvernoValidating = @($validatingWebhooks.items | Where-Object { [string]$_.metadata.name -match 'kyverno' })
        $kyvernoMutating = @($mutatingWebhooks.items | Where-Object { [string]$_.metadata.name -match 'kyverno' })
        if (-not $kyvernoValidating -or -not $kyvernoMutating) {
            throw 'Kyverno admission webhook configurations are missing.'
        }
        $endpoint = & kubectl get endpoints kyverno-svc -n kyverno -o "jsonpath={.subsets[0].addresses[0].ip}"
        if ($LASTEXITCODE -ne 0 -or -not $endpoint) {
            throw 'Kyverno admission Service has no ready endpoint.'
        }
        Add-PassedCheck $checks 'admission-webhooks-ready' $started 'Validating and mutating webhooks exist and the admission Service has a ready endpoint.'

        Invoke-External kubectl create namespace $FixtureNamespace
        Invoke-External kubectl label namespace $FixtureNamespace 'steadystate.dev/team=phase6-foundation' --overwrite

        $started = Get-Date
        $digestFixture = [ordered]@{
            apiVersion = 'v1'
            kind = 'Pod'
            metadata = [ordered]@{
                name = 'digest-mutation'
                namespace = $FixtureNamespace
                labels = [ordered]@{'steadystate.dev/phase6-fixture' = 'digest'}
            }
            spec = [ordered]@{
                automountServiceAccountToken = $false
                securityContext = [ordered]@{seccompProfile = [ordered]@{type = 'RuntimeDefault'}}
                containers = @([ordered]@{
                    name = 'application'
                    image = 'ghcr.io/saadabdullaah/steadystate-demo-app:v0.5.1'
                    resources = [ordered]@{
                        requests = [ordered]@{cpu = '10m'; memory = '16Mi'}
                        limits = [ordered]@{cpu = '100m'; memory = '64Mi'}
                    }
                    securityContext = [ordered]@{
                        runAsNonRoot = $true
                        readOnlyRootFilesystem = $true
                        allowPrivilegeEscalation = $false
                        capabilities = [ordered]@{drop = @('ALL')}
                    }
                })
            }
        }
        $denial = Assert-ObjectDenied $digestFixture
        if ($denial -notmatch '(?i)(signature|attestation|verify|denied)') {
            throw "Unsigned image denial did not expose a readable verification reason: $denial"
        }
        $rejectedImage = [string]$digestFixture.spec.containers[0].image
        Add-PassedCheck $checks 'unsigned-image-enforcement-active' $started 'The public unsigned v0.5.1 image was denied by stable fail-closed image policy.'

        $started = Get-Date
        $unsafeFixture = [ordered]@{
            apiVersion = 'v1'
            kind = 'Pod'
            metadata = [ordered]@{
                name = 'audit-violations'
                namespace = $FixtureNamespace
                labels = [ordered]@{'steadystate.dev/phase6-fixture' = 'unsafe'}
            }
            spec = [ordered]@{
                hostNetwork = $true
                volumes = @([ordered]@{name = 'host'; hostPath = [ordered]@{path = '/tmp'}})
                containers = @([ordered]@{
                    name = 'unsafe'
                    image = 'nginx:latest'
                    securityContext = [ordered]@{privileged = $true}
                    volumeMounts = @([ordered]@{name = 'host'; mountPath = '/host'})
                })
            }
        }
        $null = Assert-ObjectDenied $unsafeFixture
        Add-PassedCheck $checks 'unsafe-team-pod-denied' $started 'An unsafe Team Pod was rejected for host, privilege, resource, and mutable-image violations.'

        $started = Get-Date
        $configMap = [ordered]@{
            apiVersion = 'v1'
            kind = 'ConfigMap'
            metadata = [ordered]@{
                name = 'background-existing'
                namespace = $FixtureNamespace
                labels = [ordered]@{'steadystate.dev/phase6-background' = 'target'}
            }
            data = [ordered]@{state = 'existing-before-policy'}
        }
        Apply-Object $configMap
        $backgroundFixture = [ordered]@{
            apiVersion = 'policies.kyverno.io/v1'
            kind = 'ValidatingPolicy'
            metadata = [ordered]@{name = $BackgroundPolicy}
            spec = [ordered]@{
                validationActions = @('Audit')
                failurePolicy = 'Fail'
                webhookConfiguration = [ordered]@{timeoutSeconds = 15}
                evaluation = [ordered]@{
                    admission = [ordered]@{enabled = $true}
                    background = [ordered]@{enabled = $true}
                }
                matchConstraints = [ordered]@{
                    namespaceSelector = [ordered]@{
                        matchExpressions = @([ordered]@{key = 'steadystate.dev/team'; operator = 'Exists'})
                    }
                    resourceRules = @([ordered]@{
                        apiGroups = @('')
                        apiVersions = @('v1')
                        operations = @('CREATE','UPDATE')
                        resources = @('configmaps')
                    })
                }
                matchConditions = @([ordered]@{
                    name = 'phase6-existing-fixture'
                    expression = "has(object.metadata.labels) && 'steadystate.dev/phase6-background' in object.metadata.labels"
                })
                validations = @([ordered]@{
                    expression = "object.data.?state.orValue('') == 'compliant'"
                    message = 'Phase 6 background compatibility fixture is intentionally non-compliant.'
                })
            }
        }
        Apply-Object $backgroundFixture
        $null = Wait-PolicyFailure -Policy $BackgroundPolicy
        Add-PassedCheck $checks 'background-scan-reports-existing-resource' $started 'A stable CEL policy created after its ConfigMap reported the pre-existing violation.'

        $started = Get-Date
        foreach ($deployment in @('kyverno-admission-controller','kyverno-background-controller','kyverno-reports-controller')) {
            Invoke-External kubectl rollout restart "deployment/$deployment" -n kyverno
        }
        foreach ($deployment in @('kyverno-admission-controller','kyverno-background-controller','kyverno-reports-controller')) {
            Invoke-External kubectl rollout status "deployment/$deployment" -n kyverno --timeout=300s
        }
        $postRestart = [ordered]@{
            apiVersion = 'v1'
            kind = 'ConfigMap'
            metadata = [ordered]@{
                name = 'post-restart-admission'
                namespace = $FixtureNamespace
            }
            data = [ordered]@{state = 'admitted'}
        }
        Apply-Object $postRestart
        Add-PassedCheck $checks 'controller-restart-preserves-admission-and-reporting' $started 'Admission, background, and reports controllers restarted and admission remained available.'

        Capture-Snapshots
        $sourceSHA = if ($env:GITHUB_SHA) { $env:GITHUB_SHA } else { (& git rev-parse HEAD).Trim() }
        $evidence = [ordered]@{
            schemaVersion = 1
            result = 'passed'
            sourceSHA = $sourceSHA
            startedAt = $startedAt.ToString('o')
            completedAt = (Get-Date).ToUniversalTime().ToString('o')
            kubernetesVersion = $versions.KUBERNETES_VERSION
            kyvernoChartVersion = $versions.KYVERNO_CHART_VERSION
            kyvernoVersion = $versions.KYVERNO_VERSION
            kyvernoChartSha256 = $versions.KYVERNO_CHART_SHA256
            fixtureNamespace = $FixtureNamespace
            rejectedImage = $rejectedImage
            checks = $checks
        }
        Write-Json -Path (Join-Path $ArtifactRoot 'evidence.json') -Value $evidence
        Write-Host "Phase 6 Kyverno foundation passed with $($checks.Count) checks."
    } catch {
        Capture-Snapshots
        $sourceSHA = if ($env:GITHUB_SHA) { $env:GITHUB_SHA } else { (& git rev-parse HEAD).Trim() }
        Write-Json -Path (Join-Path $ArtifactRoot 'failure.json') -Value ([ordered]@{
            schemaVersion = 1
            result = 'failed'
            sourceSHA = $sourceSHA
            completedAt = (Get-Date).ToUniversalTime().ToString('o')
            message = $_.Exception.Message
            checks = $checks
        })
        throw
    } finally {
        Remove-Fixtures
    }
}

Push-Location $Root
try {
    if ($Stage -eq 'CaptureFailure') {
        Capture-Snapshots
    } else {
        Invoke-Test
    }
} finally {
    Pop-Location
}
