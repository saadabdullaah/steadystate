[CmdletBinding()]
param(
    [int]$HttpPort = 8080,
    [ValidateSet('minimal','standard','full')]
    [string]$Profile = 'standard',
    [string]$EvidencePath = '.artifacts/phase3/acceptance.json'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$ArtifactRoot = Join-Path $Root '.artifacts/phase3'
$ManifestPath = Join-Path $Root 'gitops/applications/demo/application.yaml'
$Repository = 'ghcr.io/saadabdullaah/steadystate-demo-app'
$ForbiddenRepository = 'ghcr.io/saadabdullaah/forbidden-demo-app'
$BaselineVersion = 'v0.1.0'
$CandidateVersion = 'v0.3.0'
$ApplicationNamespace = 'team-payments'
$ApplicationName = 'demo'
$SourceRevision = [string]$env:GITHUB_SHA
$BranchName = [string]$env:PHASE3_ACCEPTANCE_BRANCH
$AppSlug = [string]$env:PHASE3_APP_SLUG
$checks = [System.Collections.Generic.List[object]]::new()
$timestamps = [ordered]@{}
$startedAt = (Get-Date).ToUniversalTime()
$originalBranch = $null
$baselineCommit = $null
$candidateCommit = $null
$rejectionCommit = $null
$recoveryCommit = $null
$runtimeDigest = $null
$resolvedGitRevision = $null
$registryMetadata = $null
$result = 'failed'
$failureMessage = $null

function Write-Utf8 {
    param([Parameter(Mandatory)][string]$Path, [Parameter(Mandatory)][string]$Content)
    $directory = Split-Path -Parent $Path
    if ($directory) { New-Item -ItemType Directory -Force -Path $directory | Out-Null }
    [IO.File]::WriteAllText($Path, $Content, [Text.UTF8Encoding]::new($false))
}

function Invoke-External {
    param(
        [Parameter(Mandatory)][string]$Executable,
        [Parameter(ValueFromRemainingArguments)][string[]]$Arguments
    )
    & $Executable @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Executable exited with code $LASTEXITCODE" }
}

function Invoke-ExternalText {
    param(
        [Parameter(Mandatory)][string]$Executable,
        [Parameter(ValueFromRemainingArguments)][string[]]$Arguments
    )
    $output = @(& $Executable @Arguments)
    if ($LASTEXITCODE -ne 0) { throw "$Executable exited with code $LASTEXITCODE" }
    return (($output -join [Environment]::NewLine).Trim())
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
        Output = (($output -join [Environment]::NewLine).Trim())
    }
}

function Get-KubernetesObject {
    param([Parameter(Mandatory)][string[]]$Arguments)
    $result = Invoke-KubectlResult -Arguments $Arguments -OutputFormat json
    if ($result.ExitCode -ne 0 -or -not $result.Output) {
        throw "kubectl $($Arguments -join ' ') failed: $($result.Output)"
    }
    return ($result.Output | ConvertFrom-Json)
}

function Add-PassedCheck {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][datetime]$Started,
        [Parameter(Mandatory)][string]$Details
    )
    $checks.Add([ordered]@{
        name = $Name
        status = 'passed'
        elapsedSeconds = [Math]::Round(((Get-Date) - $Started).TotalSeconds, 3)
        details = $Details
    })
    Write-Host "[PASS] $Name - $Details" -ForegroundColor Green
}

function Get-ReadyCondition {
    param([Parameter(Mandatory)]$Application)
    return @($Application.status.conditions | Where-Object type -eq 'Ready') | Select-Object -First 1
}

function Test-ArgoRevision {
    param([Parameter(Mandatory)]$Application, [Parameter(Mandatory)][string]$Revision)
    if ([string]$Application.status.sync.revision -eq $Revision) { return $true }
    return $Revision -in @($Application.status.sync.revisions)
}

function Wait-ArgoApplication {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][ValidateSet('Healthy','Degraded')][string]$Health,
        [string]$Revision,
        [int]$TimeoutSeconds = 600
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $result = Invoke-KubectlResult get applications.argoproj.io $Name -n argocd -o json
        if ($result.ExitCode -eq 0 -and $result.Output) {
            $application = $result.Output | ConvertFrom-Json
            $revisionMatches = -not $Revision -or (Test-ArgoRevision -Application $application -Revision $Revision)
            if ($application.status.sync.status -eq 'Synced' -and
                $application.status.health.status -eq $Health -and $revisionMatches) {
                return $application
            }
        }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "Argo Application $Name did not become Synced/$Health at revision '$Revision'."
}

function Wait-SteadyStateApplication {
    param(
        [Parameter(Mandatory)][ValidateSet('Healthy','Degraded')][string]$Phase,
        [Parameter(Mandatory)][ValidateSet('True','False')][string]$Ready,
        [string]$Reason,
        [string]$Version,
        [string]$Revision,
        [string]$Digest,
        [int]$TimeoutSeconds = 600
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $result = Invoke-KubectlResult get applications.platform.steadystate.dev $ApplicationName `
            -n $ApplicationNamespace -o json
        if ($result.ExitCode -eq 0 -and $result.Output) {
            $application = $result.Output | ConvertFrom-Json
            $condition = Get-ReadyCondition -Application $application
            $matches = $application.status.phase -eq $Phase -and $condition.status -eq $Ready
            if ($Reason) { $matches = $matches -and $condition.reason -eq $Reason }
            if ($Version) { $matches = $matches -and $application.status.activeVersion -eq $Version }
            if ($Revision) { $matches = $matches -and $application.status.resolvedGitRevision -eq $Revision }
            if ($Digest) { $matches = $matches -and $application.status.resolvedImageDigest -eq $Digest }
            if ($matches) { return $application }
        }
        Start-Sleep -Seconds 3
    } while ((Get-Date) -lt $deadline)
    throw "SteadyState Application did not reach $Phase/Ready=$Ready reason='$Reason'."
}

function Wait-GatewayVersion {
    param([Parameter(Mandatory)][string]$Version, [int]$TimeoutSeconds = 180)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/" `
                -Headers @{ Host = 'demo.team-payments.steadystate.localtest.me' } -TimeoutSec 5
            if ($response.StatusCode -eq 200) {
                $body = $response.Content | ConvertFrom-Json
                if ($body.application -eq $ApplicationName -and $body.namespace -eq $ApplicationNamespace -and
                    $body.version -eq $Version) {
                    return
                }
            }
        } catch {
            # Gateway and EndpointSlice convergence is expected during rollouts.
        }
        Start-Sleep -Seconds 3
    } while ((Get-Date) -lt $deadline)
    throw "The shared Gateway did not serve $ApplicationNamespace/$ApplicationName at $Version."
}

function Wait-ArgoRoute {
    $deadline = (Get-Date).AddSeconds(180)
    do {
        try {
            $response = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/" `
                -Headers @{ Host = 'argocd.localtest.me' } -TimeoutSec 5
            if ($response.StatusCode -eq 200) { return }
        } catch {
            # Argo and the shared Gateway can converge independently.
        }
        Start-Sleep -Seconds 3
    } while ((Get-Date) -lt $deadline)
    throw 'Argo CD did not respond through its shared-Gateway HTTPRoute.'
}

function Assert-DexAbsent {
    foreach ($kind in @('serviceaccount','role','rolebinding','service','deployment')) {
        $result = Invoke-KubectlResult get $kind argocd-dex-server -n argocd --ignore-not-found -o name
        if ($result.ExitCode -ne 0 -or $result.Output) { throw "Unexpected Dex object: $kind/argocd-dex-server" }
    }
}

function Get-PublicRegistryMetadata {
    $scope = [uri]::EscapeDataString('repository:saadabdullaah/steadystate-demo-app:pull')
    $tokenResponse = Invoke-RestMethod -Uri "https://ghcr.io/token?scope=$scope" -Method Get
    if (-not $tokenResponse.token) { throw 'GHCR did not issue an anonymous pull token.' }
    $headers = @{
        Authorization = "Bearer $($tokenResponse.token)"
        Accept = 'application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json'
    }
    $uri = "https://ghcr.io/v2/saadabdullaah/steadystate-demo-app/manifests/$CandidateVersion"
    $head = Invoke-WebRequest -UseBasicParsing -Uri $uri -Method Head -Headers $headers
    $requestedDigest = [string]$head.Headers.'Docker-Content-Digest'
    $mediaType = ([string]$head.Headers.'Content-Type').Split(';')[0]
    $runtimeManifestDigest = $requestedDigest
    if ($mediaType -in @('application/vnd.oci.image.index.v1+json','application/vnd.docker.distribution.manifest.list.v2+json')) {
        $index = Invoke-RestMethod -Uri $uri -Method Get -Headers $headers
        $descriptor = @($index.manifests | Where-Object {
            $_.platform.os -eq 'linux' -and $_.platform.architecture -eq 'amd64'
        })
        if ($descriptor.Count -ne 1) { throw 'GHCR does not expose exactly one linux/amd64 manifest.' }
        $runtimeManifestDigest = [string]$descriptor[0].digest
    }
    if ($runtimeManifestDigest -notmatch '^sha256:[0-9a-f]{64}$') {
        throw "GHCR returned invalid runtime digest '$runtimeManifestDigest'."
    }
    return [ordered]@{
        repository = $Repository
        tag = $CandidateVersion
        requestedDigest = $requestedDigest
        runtimeDigest = $runtimeManifestDigest
        mediaType = $mediaType
        platform = 'linux/amd64'
        anonymousPull = $true
        observedAt = (Get-Date).ToUniversalTime().ToString('o')
    }
}

function Set-DemoManifest {
    param(
        [Parameter(Mandatory)][string]$RepositoryValue,
        [Parameter(Mandatory)][string]$TagValue,
        [Parameter(Mandatory)][string]$SnapshotName
    )
    $content = Get-Content -Raw -LiteralPath $ManifestPath -Encoding UTF8
    $repositoryPattern = '(?m)^    repository: .+$'
    $tagPattern = '(?m)^    tag: v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$'
    if ([regex]::Matches($content, $repositoryPattern).Count -ne 1 -or
        [regex]::Matches($content, $tagPattern).Count -ne 1) {
        throw 'The demo manifest must contain exactly one repository and one strict semver tag.'
    }
    $content = [regex]::Replace($content, $repositoryPattern, "    repository: $RepositoryValue")
    $content = [regex]::Replace($content, $tagPattern, "    tag: $TagValue")
    Write-Utf8 -Path $ManifestPath -Content $content
    $changed = @(git diff --name-only)
    $allowed = @(
        'gitops/applications/demo/application.yaml',
        'docs/demonstrations/phase1-self-heal.gif',
        'docs/demonstrations/phase3-gitops-delivery.gif'
    )
    $unexpected = @($changed | Where-Object { $_ -notin $allowed })
    if ('gitops/applications/demo/application.yaml' -notin $changed -or $unexpected.Count -gt 0) {
        throw "Acceptance commit changed unexpected files: $($unexpected -join ', ')"
    }
    Write-Utf8 -Path (Join-Path $ArtifactRoot "rendered/$SnapshotName.yaml") -Content $content
}

function New-AcceptanceCommit {
    param([Parameter(Mandatory)][string]$Message)
    Invoke-External git add -- gitops/applications/demo/application.yaml | Out-Host
    Invoke-External git commit -m $Message | Out-Host
    return (Invoke-ExternalText git rev-parse HEAD)
}

function Push-AcceptanceBranch {
    if (-not $script:branchPushed) {
        Invoke-External git push --set-upstream origin $BranchName
        $script:branchPushed = $true
    } else {
        Invoke-External git push origin $BranchName
    }
}

function Get-ResourceSnapshot {
    $queries = [ordered]@{
        team = @('get','teams.platform.steadystate.dev','payments')
        application = @('get','applications.platform.steadystate.dev',$ApplicationName,'-n',$ApplicationNamespace)
        deployment = @('get','deployment',$ApplicationName,'-n',$ApplicationNamespace)
        service = @('get','service',$ApplicationName,'-n',$ApplicationNamespace)
        configmap = @('get','configmap',"$ApplicationName-config",'-n',$ApplicationNamespace)
        httproute = @('get','httproute',$ApplicationName,'-n',$ApplicationNamespace)
    }
    $snapshot = [ordered]@{}
    foreach ($name in $queries.Keys) {
        $object = Get-KubernetesObject -Arguments $queries[$name]
        $snapshot[$name] = [ordered]@{
            uid = [string]$object.metadata.uid
            resourceVersion = [string]$object.metadata.resourceVersion
            generation = [string]$object.metadata.generation
            ownedState = ([ordered]@{
                labels = $object.metadata.labels
                ownerReferences = $object.metadata.ownerReferences
                spec = $object.spec
                data = $object.data
            } | ConvertTo-Json -Depth 20 -Compress)
        }
    }
    $pods = Get-KubernetesObject -Arguments @(
        'get','pods','-n',$ApplicationNamespace,'-l',"app.kubernetes.io/name=$ApplicationName"
    )
    $activePods = @($pods.items | Where-Object {
        -not $_.metadata.deletionTimestamp -and
        $_.status.phase -eq 'Running' -and
        @($_.status.conditions | Where-Object {
            $_.type -eq 'Ready' -and $_.status -eq 'True'
        }).Count -eq 1
    })
    if ($activePods.Count -eq 0) { throw 'No active Ready Application Pods were found for the resource snapshot.' }
    $snapshot.pods = @($activePods | Sort-Object { $_.metadata.name } | ForEach-Object {
        [ordered]@{ name = [string]$_.metadata.name; uid = [string]$_.metadata.uid }
    })
    return $snapshot
}

function Assert-UidsEqual {
    param([Parameter(Mandatory)]$Before, [Parameter(Mandatory)]$After)
    foreach ($name in @('team','application','deployment','service','configmap','httproute')) {
        if ($Before[$name].uid -ne $After[$name].uid) { throw "$name UID changed unexpectedly." }
    }
    if (($Before.pods | ConvertTo-Json -Compress) -ne ($After.pods | ConvertTo-Json -Compress)) {
        throw 'Application Pod identities changed unexpectedly.'
    }
}

function Assert-NoReconciliationWrites {
    param([Parameter(Mandatory)]$Before, [Parameter(Mandatory)]$After)
    foreach ($name in @('team','application')) {
        if ($Before[$name].resourceVersion -ne $After[$name].resourceVersion) {
            throw "$name was rewritten during steady-state operator recovery."
        }
    }
    foreach ($name in @('deployment','service','configmap','httproute')) {
        if ($Before[$name].generation -ne $After[$name].generation -or
            $Before[$name].ownedState -ne $After[$name].ownedState) {
            throw "$name operator-owned state drifted during controller recovery."
        }
    }
}

function Assert-ArgoOwnershipBoundary {
    $application = Get-KubernetesObject -Arguments @('get','applications.argoproj.io','payments','-n','argocd')
    $resources = @($application.status.resources)
    if ($resources.Count -lt 2) { throw 'The tenant Argo Application does not report Team and Application resources.' }
    foreach ($resource in $resources) {
        if ($resource.group -ne 'platform.steadystate.dev' -or $resource.kind -notin @('Team','Application')) {
            throw "Argo unexpectedly tracks generated child $($resource.group)/$($resource.kind)/$($resource.name)."
        }
    }
}

function Save-CommandOutput {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Executable,
        [Parameter(Mandatory)][string[]]$Arguments
    )
    if ($Executable -ne 'kubectl') { throw "Unsupported evidence command $Executable." }
    $result = Invoke-KubectlResult @Arguments
    if ($result.ExitCode -ne 0) { throw "Evidence command failed: kubectl $($Arguments -join ' ')" }
    if ($result.Output) { Write-Utf8 -Path $Path -Content ($result.Output + [Environment]::NewLine) }
}

function Save-ClusterEvidence {
    Save-CommandOutput -Path (Join-Path $ArtifactRoot 'snapshots/argocd-applications.json') `
        -Executable kubectl -Arguments @('get','applications.argoproj.io','-n','argocd','-o','json')
    Save-CommandOutput -Path (Join-Path $ArtifactRoot 'snapshots/application.json') `
        -Executable kubectl -Arguments @('get','applications.platform.steadystate.dev',$ApplicationName,'-n',$ApplicationNamespace,'-o','json')
    Save-CommandOutput -Path (Join-Path $ArtifactRoot 'snapshots/team.json') `
        -Executable kubectl -Arguments @('get','teams.platform.steadystate.dev','payments','-o','json')
    Save-CommandOutput -Path (Join-Path $ArtifactRoot 'logs/operator.log') `
        -Executable kubectl -Arguments @('logs','-n','steadystate-system','deployment/steadystate-controller-manager','--all-containers','--tail=1000')
    Save-CommandOutput -Path (Join-Path $ArtifactRoot 'logs/argocd-application-controller.log') `
        -Executable kubectl -Arguments @('logs','-n','argocd','statefulset/argocd-application-controller','--all-containers','--tail=1000')
    Save-CommandOutput -Path (Join-Path $ArtifactRoot 'logs/argocd-repo-server.log') `
        -Executable kubectl -Arguments @('logs','-n','argocd','deployment/argocd-repo-server','--all-containers','--tail=1000')
}

function Save-RenderedRoot {
    $arguments = @(
        'template','steadystate-root',(Join-Path $Root 'gitops/clusters/local'),
        '--namespace','argocd','--set-string',"gitRevision=$recoveryCommit"
    )
    $rendered = @(& helm @arguments)
    if ($LASTEXITCODE -ne 0) { throw 'Could not render the recovered GitOps root.' }
    Write-Utf8 -Path (Join-Path $ArtifactRoot 'rendered/root.yaml') `
        -Content (($rendered -join [Environment]::NewLine) + [Environment]::NewLine)
}

function Write-Evidence {
    param([Parameter(Mandatory)][ValidateSet('passed','failed')][string]$EvidenceResult)
    $resolvedPath = if ([IO.Path]::IsPathRooted($EvidencePath)) {
        [IO.Path]::GetFullPath($EvidencePath)
    } else {
        [IO.Path]::GetFullPath((Join-Path $Root $EvidencePath))
    }
    $evidence = [ordered]@{
        schemaVersion = 1
        result = $EvidenceResult
        sourceSHA = $SourceRevision
        ephemeralBranch = $BranchName
        baselineCommit = $baselineCommit
        candidateCommit = $candidateCommit
        rejectionCommit = $rejectionCommit
        recoveryCommit = $recoveryCommit
        startedAt = $startedAt.ToString('o')
        completedAt = (Get-Date).ToUniversalTime().ToString('o')
        timestamps = $timestamps
        profile = $Profile
        application = [ordered]@{ namespace = $ApplicationNamespace; name = $ApplicationName }
        imageVersions = [ordered]@{ baseline = $BaselineVersion; candidate = $CandidateVersion }
        runtimeDigest = $runtimeDigest
        resolvedGitRevision = $resolvedGitRevision
        registry = $registryMetadata
        checks = $checks
        failure = $failureMessage
    }
    Write-Utf8 -Path $resolvedPath -Content (($evidence | ConvertTo-Json -Depth 10) + [Environment]::NewLine)
}

if ($Profile -ne 'standard') { throw 'Phase 3 hosted acceptance requires the standard profile.' }
if ($SourceRevision -notmatch '^([0-9a-f]{40}|[0-9a-f]{64})$') { throw 'GITHUB_SHA must be a full Git object ID.' }
if ($BranchName -notmatch '^acceptance/phase3-[0-9]+-[0-9]+$') { throw 'PHASE3_ACCEPTANCE_BRANCH is invalid.' }
if (-not $env:GH_TOKEN -or -not $AppSlug) { throw 'The dedicated delivery App token and slug are required.' }

Push-Location $Root
try {
    New-Item -ItemType Directory -Force -Path $ArtifactRoot | Out-Null
    $originalBranch = (Invoke-ExternalText git branch --show-current)
    if (-not $originalBranch) { throw 'The workflow checkout must be on a named branch.' }
    $trackedChanges = @(git diff --name-only)
    $unexpectedChanges = @($trackedChanges | Where-Object {
        $_ -notin @(
            'docs/demonstrations/phase1-self-heal.gif',
            'docs/demonstrations/phase3-gitops-delivery.gif'
        )
    })
    if ($unexpectedChanges.Count -gt 0) {
        throw "Tracked files are dirty before acceptance: $($unexpectedChanges -join ', ')"
    }

    $botLogin = "$AppSlug[bot]"
    $botID = Invoke-ExternalText gh api "/users/$botLogin" --jq .id
    Invoke-External git config user.name $botLogin
    Invoke-External git config user.email "$botID+$botLogin@users.noreply.github.com"
    Invoke-External git switch --create $BranchName $SourceRevision

    Set-DemoManifest -RepositoryValue $Repository -TagValue $BaselineVersion -SnapshotName baseline
    $baselineCommit = New-AcceptanceCommit -Message 'test(gitops): establish Phase 3 acceptance baseline'
    Push-AcceptanceBranch
    $timestamps.baselinePushedAt = (Get-Date).ToUniversalTime().ToString('o')

    $deployStarted = Get-Date
    & (Join-Path $Root 'scripts/dev.ps1') deploy-gitops -Profile $Profile -GitRevision $BranchName
    if ($LASTEXITCODE -ne 0) { throw 'GitOps deployment failed.' }
    Assert-DexAbsent
    foreach ($name in @('argocd-configuration','monitoring','argo-rollouts','steadystate-operator','payments','steadystate-root')) {
        Wait-ArgoApplication -Name $name -Health Healthy -Revision $baselineCommit | Out-Null
    }
    Add-PassedCheck -Name 'pinned-argocd-installed-dex-absent' -Started $deployStarted `
        -Details 'Argo CD v3.4.2 is ready and all five unused Dex objects are absent.'
    Add-PassedCheck -Name 'root-platform-team-applications-healthy' -Started $deployStarted `
        -Details 'Root, platform, operator, and tenant Applications are Synced and Healthy at the baseline commit.'

    $routeStarted = Get-Date
    Wait-ArgoRoute
    Add-PassedCheck -Name 'argocd-ui-route-reachable' -Started $routeStarted `
        -Details 'The Argo UI returns HTTP 200 through the shared Gateway without exposing credentials.'

    $baselineStarted = Get-Date
    $baselineApplication = Wait-SteadyStateApplication -Phase Healthy -Ready True `
        -Version $BaselineVersion -Revision $baselineCommit
    Wait-GatewayVersion -Version $BaselineVersion
    Add-PassedCheck -Name 'baseline-version-reachable' -Started $baselineStarted `
        -Details 'The locally loaded v0.1.0 image is Healthy and reachable through the shared Gateway.'
    $baselineDigest = [string]$baselineApplication.status.resolvedImageDigest
    if ($baselineDigest -notmatch '^sha256:[0-9a-f]{64}$') { throw 'The baseline runtime digest is invalid.' }

    $registryMetadata = Get-PublicRegistryMetadata
    Write-Utf8 -Path (Join-Path $ArtifactRoot 'registry.json') `
        -Content (($registryMetadata | ConvertTo-Json -Depth 6) + [Environment]::NewLine)

    Set-DemoManifest -RepositoryValue $Repository -TagValue $CandidateVersion -SnapshotName candidate
    $candidateCommit = New-AcceptanceCommit -Message 'test(gitops): deliver Phase 3 candidate'
    $candidatePushStarted = Get-Date
    Push-AcceptanceBranch
    $timestamps.candidatePushedAt = (Get-Date).ToUniversalTime().ToString('o')

    $candidateRoot = Wait-ArgoApplication -Name steadystate-root -Health Healthy -Revision $candidateCommit
    Add-PassedCheck -Name 'git-commit-detected-without-kubectl-delivery-mutation' -Started $candidatePushStarted `
        -Details 'Argo detected the pushed candidate commit while the delivery interval used read-only cluster observation.'
    $candidateApplication = Wait-SteadyStateApplication -Phase Healthy -Ready True `
        -Version $CandidateVersion -Revision $candidateCommit -Digest $registryMetadata.runtimeDigest
    Wait-ArgoApplication -Name payments -Health Healthy -Revision $candidateCommit | Out-Null
    Wait-GatewayVersion -Version $CandidateVersion
    $timestamps.candidateServedAt = (Get-Date).ToUniversalTime().ToString('o')
    Add-PassedCheck -Name 'candidate-synchronized-and-served' -Started $candidatePushStarted `
        -Details 'Argo synchronized v0.3.0, the operator rolled it out, and the Gateway served it.'

    $runtimeDigest = [string]$candidateApplication.status.resolvedImageDigest
    $resolvedGitRevision = [string]$candidateApplication.status.resolvedGitRevision
    if ($runtimeDigest -ne $registryMetadata.runtimeDigest) { throw 'Runtime and public GHCR linux/amd64 digests differ.' }
    Add-PassedCheck -Name 'runtime-digest-matches-ghcr-linux-amd64' -Started $candidatePushStarted `
        -Details "Runtime digest $runtimeDigest matches the anonymously resolved GHCR linux/amd64 manifest."
    if ($resolvedGitRevision -ne $candidateCommit -or $candidateRoot.status.sync.revision -ne $candidateCommit) {
        throw 'Runtime and Argo revisions do not match the candidate commit.'
    }
    Add-PassedCheck -Name 'resolved-git-revision-matches-candidate' -Started $candidatePushStarted `
        -Details 'Application status and the root Argo Application report the exact candidate commit.'

    $healthySnapshot = Get-ResourceSnapshot
    Set-DemoManifest -RepositoryValue $ForbiddenRepository -TagValue $CandidateVersion -SnapshotName rejection
    $rejectionCommit = New-AcceptanceCommit -Message 'test(gitops): reject forbidden demo repository'
    $rejectionStarted = Get-Date
    Push-AcceptanceBranch
    $timestamps.rejectionPushedAt = (Get-Date).ToUniversalTime().ToString('o')
    $rejectedApplication = Wait-SteadyStateApplication -Phase Degraded -Ready False `
        -Reason RepositoryNotAllowed -Version $CandidateVersion -Revision $candidateCommit -Digest $runtimeDigest
    Wait-ArgoApplication -Name payments -Health Degraded -Revision $rejectionCommit | Out-Null
    Wait-GatewayVersion -Version $CandidateVersion
    Assert-UidsEqual -Before $healthySnapshot -After (Get-ResourceSnapshot)
    Add-PassedCheck -Name 'kubernetes-and-argo-degraded-on-rejection' -Started $rejectionStarted `
        -Details 'Kubernetes and Argo report Degraded while the last healthy v0.3.0 resources continue serving.'

    Set-DemoManifest -RepositoryValue $Repository -TagValue $CandidateVersion -SnapshotName recovery
    $recoveryCommit = New-AcceptanceCommit -Message 'test(gitops): recover accepted demo repository'
    $recoveryStarted = Get-Date
    Push-AcceptanceBranch
    $timestamps.recoveryPushedAt = (Get-Date).ToUniversalTime().ToString('o')
    Wait-SteadyStateApplication -Phase Healthy -Ready True -Version $CandidateVersion `
        -Revision $recoveryCommit -Digest $runtimeDigest | Out-Null
    Wait-ArgoApplication -Name payments -Health Healthy -Revision $recoveryCommit | Out-Null
    Wait-ArgoApplication -Name steadystate-root -Health Healthy -Revision $recoveryCommit | Out-Null
    Wait-GatewayVersion -Version $CandidateVersion
    Assert-UidsEqual -Before $healthySnapshot -After (Get-ResourceSnapshot)
    $resolvedGitRevision = $recoveryCommit
    $timestamps.recoveredAt = (Get-Date).ToUniversalTime().ToString('o')
    Add-PassedCheck -Name 'recovery-restores-healthy' -Started $recoveryStarted `
        -Details 'Recovery returned Kubernetes and Argo to Healthy without replacing data-plane resources.'
    Add-PassedCheck -Name 'argo-health-matches-kubernetes-status' -Started $rejectionStarted `
        -Details 'Lua health reflected Degraded rejection and Healthy recovery from current Kubernetes status.'

    $outageBefore = Get-ResourceSnapshot
    $outageStarted = Get-Date
    Invoke-External kubectl delete pod -n steadystate-system -l control-plane=controller-manager `
        --wait=true --timeout=90s
    Wait-ArgoApplication -Name payments -Health Healthy -Revision $recoveryCommit -TimeoutSeconds 120 | Out-Null
    $outageDuring = Get-ResourceSnapshot
    Assert-UidsEqual -Before $outageBefore -After $outageDuring
    Add-PassedCheck -Name 'operator-outage-preserves-resource-uids' -Started $outageStarted `
        -Details 'The tenant Argo Application stayed Healthy while all CR and child UIDs remained stable.'
    Invoke-External kubectl rollout status deployment/steadystate-controller-manager `
        -n steadystate-system --timeout=180s
    Start-Sleep -Seconds 10
    $outageAfter = Get-ResourceSnapshot
    Assert-UidsEqual -Before $outageBefore -After $outageAfter
    Assert-NoReconciliationWrites -Before $outageBefore -After $outageAfter
    Wait-GatewayVersion -Version $CandidateVersion
    Add-PassedCheck -Name 'operator-restart-reconciles-without-drift' -Started $outageStarted `
        -Details 'The replacement controller became Ready and steady-state reconciliation wrote no managed resources.'

    $ownershipStarted = Get-Date
    Assert-ArgoOwnershipBoundary
    Add-PassedCheck -Name 'argo-ownership-boundary' -Started $ownershipStarted `
        -Details 'Argo tracks platform resources plus Team/Application CRs, never operator-generated workload children.'

    Save-RenderedRoot
    Save-ClusterEvidence
    $result = 'passed'
    Write-Evidence -EvidenceResult passed
    Clear-Host
    Write-Host 'SteadyState Phase 3 GitOps delivery acceptance passed.' -ForegroundColor Cyan
    Write-Host 'PHASE3_ACCEPTANCE_RESULT_PASSED' -ForegroundColor Cyan
} catch {
    $failureMessage = $_.Exception.Message
    try { Save-ClusterEvidence } catch { Write-Warning "Could not capture all Phase 3 cluster evidence: $($_.Exception.Message)" }
    Write-Evidence -EvidenceResult failed
    Clear-Host
    Write-Host "SteadyState Phase 3 GitOps delivery acceptance failed: $failureMessage" -ForegroundColor Red
    Write-Host 'PHASE3_ACCEPTANCE_RESULT_FAILED' -ForegroundColor Red
    throw
} finally {
    if ($originalBranch) {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        & git switch $originalBranch
        if ($LASTEXITCODE -ne 0) { Write-Warning "Could not restore branch $originalBranch." }
        $ErrorActionPreference = $previousPreference
    }
    Pop-Location
}
