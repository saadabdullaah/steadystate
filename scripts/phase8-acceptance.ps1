[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateSet('Prepare','Stabilize','AwaitFoundation','Test','Finalize','CaptureFailure')]
    [string]$Stage,
    [int]$HttpPort = 8080
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$ArtifactRoot = Join-Path $Root '.artifacts/phase8/acceptance'
$StatePath = Join-Path $ArtifactRoot 'state.json'
$EvidencePath = Join-Path $ArtifactRoot 'evidence.json'
$TranscriptPath = Join-Path $ArtifactRoot 'transcript.txt'
$CliPath = Join-Path $Root ".artifacts/platformctl$(if ($env:OS -eq 'Windows_NT') { '.exe' })"
$Repository = if ($env:GITHUB_REPOSITORY) { $env:GITHUB_REPOSITORY } else { 'saadabdullaah/steadystate' }
$Namespace = 'team-xyz'

function Write-Utf8([string]$Path, [string]$Value) {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null
    [IO.File]::WriteAllText($Path, $Value, [Text.UTF8Encoding]::new($false))
}

function Write-Transcript([string]$Value) {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $TranscriptPath) | Out-Null
    [IO.File]::AppendAllText($TranscriptPath, "$Value$([Environment]::NewLine)", [Text.UTF8Encoding]::new($false))
}

function Convert-TraceHexToBase64([string]$TraceID) {
    if ($TraceID -cnotmatch '^[0-9a-f]{32}$') { throw 'Trace ID is not canonical lowercase hexadecimal.' }
    $bytes = [byte[]]::new(16)
    for ($index = 0; $index -lt $bytes.Length; $index++) {
        $bytes[$index] = [Convert]::ToByte($TraceID.Substring($index * 2, 2), 16)
    }
    return [Convert]::ToBase64String($bytes)
}

function Get-State {
    if (-not (Test-Path -LiteralPath $StatePath)) { throw 'Phase 8 acceptance state is missing.' }
    return Get-Content -Raw -LiteralPath $StatePath | ConvertFrom-Json
}

function Save-State($State) {
    Write-Utf8 $StatePath (($State | ConvertTo-Json -Depth 60) + [Environment]::NewLine)
}

function Add-Check($State, [string]$Name, [datetime]$Started, [string]$Details) {
    $elapsed = [Math]::Round(((Get-Date) - $Started).TotalSeconds, 3)
    $State.checks += [pscustomobject]@{name=$Name;status='passed';elapsedSeconds=$elapsed;details=$Details}
    Save-State $State
    Write-Transcript "PASS $Name elapsedSeconds=$elapsed"
}

function Set-AcceptanceStage($State, [string]$Name) {
    $State.currentStage = $Name
    $State.stageStartedAt = (Get-Date).ToUniversalTime().ToString('o')
    Save-State $State
    Write-Transcript "STAGE $($State.stageStartedAt) $Name"
    Write-Host "PHASE8_STAGE $Name"
}

function Wait-Until([int]$TimeoutSeconds, [string]$Failure, [scriptblock]$Condition) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        if (& $Condition) { return }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw $Failure
}

function Invoke-Platformctl([string[]]$Arguments, [string]$OutputPath, [switch]$AllowFailure) {
    if (-not (Test-Path -LiteralPath $CliPath -PathType Leaf)) { throw 'The exact-revision platformctl binary is missing.' }
    $started = Get-Date
    $lines = @(& $CliPath --output json --no-color --timeout 2m @Arguments 2>&1)
    $code = $LASTEXITCODE
    Write-Utf8 $OutputPath (($lines -join [Environment]::NewLine) + [Environment]::NewLine)
    if ($code -ne 0 -and -not $AllowFailure) { throw "platformctl $($Arguments -join ' ') failed with exit code $code." }
    return [pscustomobject]@{exitCode=$code;elapsedMilliseconds=[Math]::Round(((Get-Date)-$started).TotalMilliseconds,3);output=$lines}
}

function Invoke-PlatformctlUntilMatch(
    [string[]]$Arguments,
    [string]$OutputPath,
    [string]$Pattern,
    [string]$Failure,
    [int]$TimeoutSeconds = 180
) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $result = Invoke-Platformctl $Arguments $OutputPath -AllowFailure
        if ($result.exitCode -eq 0) {
            $content = Get-Content -Raw -LiteralPath $OutputPath
            if ($content -match $Pattern) { return $content }
        }
        Start-Sleep -Seconds 10
    } while ((Get-Date) -lt $deadline)
    throw $Failure
}

function Wait-Ready([string]$Resource, [string]$Name, [string]$Namespace, [int]$TimeoutSeconds = 600) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $apiFailures = 0
    do {
        $raw = @(& kubectl --request-timeout=10s get $Resource $Name -n $Namespace -o json 2>$null)
        if ($LASTEXITCODE -eq 0 -and $raw) {
            $apiFailures = 0
            $object = ($raw -join [Environment]::NewLine) | ConvertFrom-Json
            if (@($object.status.conditions | Where-Object {
                $_.type -eq 'Ready' -and $_.status -eq 'True' -and
                [int64]$_.observedGeneration -eq [int64]$object.metadata.generation
            }).Count -eq 1) { return }
        } else {
            $readyz = @(& kubectl --request-timeout=10s get --raw='/readyz' 2>$null)
            if ($LASTEXITCODE -eq 0 -and ($readyz -join '').Trim() -eq 'ok') {
                # A healthy API with an absent object is ordinary GitOps convergence,
                # not an API outage. Only consecutive readyz failures fail fast.
                $apiFailures = 0
            } else {
                $apiFailures++
                if ($apiFailures -ge 6) {
                    throw "Kubernetes API became unavailable while waiting for $Resource $Namespace/$Name."
                }
            }
        }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "$Resource $Namespace/$Name did not become current-generation Ready."
}

function Set-HostedControlPlanePriority {
    if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_OS -ne 'Linux') {
        throw 'Hosted kind resource stabilization is restricted to the Linux Phase 8 workflow.'
    }
    $expected = @('steadystate-control-plane','steadystate-worker','steadystate-worker2')
    $actual = @(& docker ps --filter 'label=io.x-k8s.kind.cluster=steadystate' --format '{{.Names}}' | Sort-Object)
    if ($LASTEXITCODE -ne 0 -or ($actual -join "`n") -cne (($expected | Sort-Object) -join "`n")) {
        throw "Refusing to tune unexpected kind containers: $($actual -join ', ')."
    }
    # Use relative scheduling weights without hard quotas. The control plane
    # receives half of contended CPU while a busy worker can borrow capacity
    # from an idle peer. Per-worker quotas previously stranded that capacity.
    & docker update --cpus 0 --cpu-shares 2048 --memory-reservation 2g steadystate-control-plane | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not reserve hosted control-plane resources.' }
    foreach ($worker in @('steadystate-worker','steadystate-worker2')) {
        & docker update --cpus 0 --cpu-shares 1024 $worker | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "Could not set hosted worker scheduling weight for $worker." }
    }
    $configuration = @(& docker inspect @($expected))
    if ($LASTEXITCODE -ne 0 -or -not $configuration) { throw 'Could not inspect hosted kind resource stabilization.' }
    $containers = @((($configuration -join [Environment]::NewLine) | ConvertFrom-Json))
    $controlPlane = @($containers | Where-Object { $_.Name -eq '/steadystate-control-plane' }) | Select-Object -First 1
    $workers = @($containers | Where-Object { $_.Name -in @('/steadystate-worker','/steadystate-worker2') })
    if (-not $controlPlane -or [int64]$controlPlane.HostConfig.NanoCpus -ne 0 -or
        [int64]$controlPlane.HostConfig.CpuShares -ne 2048 -or
        [int64]$controlPlane.HostConfig.MemoryReservation -ne 2147483648 -or
        $workers.Count -ne 2 -or @($workers | Where-Object {
            [int64]$_.HostConfig.NanoCpus -ne 0 -or [int64]$_.HostConfig.CpuShares -ne 1024
        }).Count -ne 0) {
        throw 'Hosted kind resource stabilization differs from the exact expected contract.'
    }
    $snapshot = @(& docker inspect @($expected) --format '{{.Name}} nanoCpus={{.HostConfig.NanoCpus}} cpuShares={{.HostConfig.CpuShares}} memoryReservation={{.HostConfig.MemoryReservation}}')
    if ($LASTEXITCODE -ne 0) { throw 'Could not verify hosted kind resource stabilization.' }
    Write-Utf8 (Join-Path $ArtifactRoot 'snapshots/hosted-kind-resources.txt') (($snapshot -join [Environment]::NewLine) + [Environment]::NewLine)
}

function Capture-RenderedGitOps([ValidateSet('baseline','retiring','finalized')][string]$Name, [string]$Revision) {
    if ($Revision -cnotmatch '^[0-9a-f]{40}$') { throw "Cannot render GitOps evidence for invalid revision $Revision." }
    $directory = Join-Path $ArtifactRoot 'rendered'
    New-Item -ItemType Directory -Force -Path $directory | Out-Null
    $arguments = @(
        'template','steadystate-root',(Join-Path $Root 'gitops/clusters/local'),
        '--namespace','argocd',
        '--set-string',"gitRevision=$Revision",
        '--set','enableDataFoundation=true',
        '--set-string','tenantFilter=xyz'
    )
    $rendered = @(& helm @arguments)
    if ($LASTEXITCODE -ne 0 -or -not $rendered) { throw "Could not render $Name GitOps evidence." }
    Write-Utf8 (Join-Path $directory "$Name.yaml") (($rendered -join [Environment]::NewLine) + [Environment]::NewLine)
    Copy-Item -LiteralPath (Join-Path $Root 'gitops/clusters/local/catalog/tenants.yaml') -Destination (Join-Path $directory "$Name-catalog.yaml") -Force
}

function Assert-TenantFilterIsolation {
    $queries = @(
        @('applications.argoproj.io','payments','-n','argocd'),
        @('teams.platform.steadystate.dev','payments'),
        @('namespaces','team-payments')
    )
    $observed = @()
    foreach ($query in $queries) {
        $value = @(& kubectl --request-timeout=10s get @query --ignore-not-found -o name 2>$null)
        if ($LASTEXITCODE -ne 0) { throw "Could not verify tenant isolation for $($query[0])/$($query[1])." }
        $observed += @($value | Where-Object { $_ })
    }
    if ($observed.Count -ne 0) { throw "The Phase 8 tenant filter admitted unselected resources: $($observed -join ', ')." }
    Write-Utf8 (Join-Path $ArtifactRoot 'snapshots/tenant-filter-isolation.json') (([ordered]@{
        selectedTenant='xyz';excludedTenant='payments';excludedArgoApplicationAbsent=$true
        excludedTeamAbsent=$true;excludedNamespaceAbsent=$true
    } | ConvertTo-Json) + [Environment]::NewLine)
}

function Wait-PlatformFoundationReady([int]$TimeoutSeconds = 1200) {
    $required = @(
        'argocd-configuration','monitoring','argo-rollouts','loki','tempo','otel-collector','alloy',
        'kyverno','kyverno-policies','data-namespaces','local-path-storage','cert-manager',
        'cloudnative-pg','barman-cloud','steadystate-operator'
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $pending = @($required)
    $lastStates = @{}
    $apiFailures = 0
    do {
        $raw = @(& kubectl --request-timeout=10s get --raw='/apis/argoproj.io/v1alpha1/namespaces/argocd/applications' 2>$null)
        if ($LASTEXITCODE -eq 0 -and $raw) {
            $apiFailures = 0
            $collection = ($raw -join [Environment]::NewLine) | ConvertFrom-Json
            $states = @{}
            foreach ($application in @($collection.items)) {
                $states[[string]$application.metadata.name] = [pscustomobject]@{
                    sync = [string]$application.status.sync.status
                    health = [string]$application.status.health.status
                    message = [string]$application.status.health.message
                }
            }
            $lastStates = $states
            Write-Utf8 (Join-Path $ArtifactRoot 'snapshots/platform-foundation-progress.json') (([ordered]@{
                capturedAt=(Get-Date).ToUniversalTime().ToString('o')
                applications=@($required | ForEach-Object {
                    $current = if ($states.ContainsKey($_)) { $states[$_] } else { [pscustomobject]@{sync='Missing';health='Missing';message='Application has not been created.'} }
                    [ordered]@{name=$_;sync=$current.sync;health=$current.health;message=$current.message}
                })
            } | ConvertTo-Json -Depth 8) + [Environment]::NewLine)
            $pending = @($required | Where-Object {
                -not $states.ContainsKey($_) -or $states[$_].sync -ne 'Synced' -or $states[$_].health -ne 'Healthy'
            })
            if ($pending.Count -eq 0) {
                Write-Utf8 (Join-Path $ArtifactRoot 'snapshots/platform-foundation-applications.json') (($collection | ConvertTo-Json -Depth 30) + [Environment]::NewLine)
                return
            }
        } else {
            $readyz = @(& kubectl --request-timeout=10s get --raw='/readyz' 2>$null)
            if ($LASTEXITCODE -eq 0 -and ($readyz -join '').Trim() -eq 'ok') { $apiFailures = 0 } else { $apiFailures++ }
            if ($apiFailures -ge 6) {
                throw "Kubernetes API became unavailable during platform foundation readiness; pending: $($pending -join ', ')."
            }
        }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    $details = @($pending | ForEach-Object {
        if ($lastStates.ContainsKey($_)) { "$_(sync=$($lastStates[$_].sync),health=$($lastStates[$_].health),message=$($lastStates[$_].message))" }
        else { "$_(missing)" }
    })
    throw "Full-profile platform foundation did not settle before product acceptance: $($details -join '; ')."
}

function Wait-ControlPlaneStable([int]$TimeoutSeconds = 300) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    $consecutive = 0
    do {
        $previousPreference = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $ready = @(& kubectl --request-timeout=10s get --raw='/readyz' 2>$null)
        $readyCode = $LASTEXITCODE
        $allowed = @(& kubectl --request-timeout=10s auth can-i get pods --all-namespaces 2>$null)
        $allowedCode = $LASTEXITCODE
        $ErrorActionPreference = $previousPreference
        if ($readyCode -eq 0 -and ($ready -join '') -eq 'ok' -and $allowedCode -eq 0 -and ($allowed -join '').Trim() -eq 'yes') {
            $consecutive++
            if ($consecutive -ge 3) {
                Write-Utf8 (Join-Path $ArtifactRoot 'snapshots/control-plane-stability.txt') "readyz=ok$([Environment]::NewLine)clusterAdminAuthorization=yes$([Environment]::NewLine)consecutiveChecks=$consecutive$([Environment]::NewLine)"
                return
            }
        } else {
            $consecutive = 0
        }
        Start-Sleep -Seconds 5
    } while ((Get-Date) -lt $deadline)
    throw "Kubernetes API storage and authorization did not remain stable for three consecutive checks within $TimeoutSeconds seconds."
}

function Wait-ArgoRevision([string]$Revision, [int]$TimeoutSeconds = 420) {
    Wait-Until $TimeoutSeconds "Argo tenant xyz did not adopt revision $Revision." {
        $raw = @(& kubectl --request-timeout=10s get applications.argoproj.io xyz -n argocd -o json 2>$null)
        if ($LASTEXITCODE -ne 0 -or -not $raw) { return $false }
        $app = ($raw -join [Environment]::NewLine) | ConvertFrom-Json
        $revisions = if ($app.status.sync.revisions) { @($app.status.sync.revisions) } else { @($app.status.sync.revision) }
        return $app.status.health.status -eq 'Healthy' -and $app.status.sync.status -eq 'Synced' -and
            $revisions.Count -gt 0 -and @($revisions | Where-Object { $_ -ne $Revision }).Count -eq 0
    }
}

function New-Proposal([string]$Operation, $Parameters, [string]$BaseSHA, [string]$RequestID) {
    $request = [ordered]@{
        apiVersion='cli.steadystate.dev/v1alpha1';kind='ChangeRequest';schemaVersion='v1alpha1'
        requestID=$RequestID;baseSHA=$BaseSHA;rendererVersion='v0.8.0';operation=$Operation;parameters=$Parameters
    }
    $json = $request | ConvertTo-Json -Depth 20 -Compress
    return [pscustomobject]@{request=$request;encoded=[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($json))}
}

function Get-AppBotLogin {
    if ([string]::IsNullOrWhiteSpace($env:APP_SLUG) -or $env:APP_SLUG -cnotmatch '^[a-z0-9][a-z0-9-]*$') {
        throw 'The GitHub App slug is missing or invalid.'
    }
    return "$($env:APP_SLUG)[bot]"
}

function Get-PullRequestEvidence([int]$Number) {
    $raw = @(& gh api "repos/$Repository/pulls/$Number" 2>&1)
    $code = $LASTEXITCODE
    if ($code -ne 0 -or -not $raw) { throw "Could not read pull request #$Number through the GitHub REST API." }
    $pull = ($raw -join [Environment]::NewLine) | ConvertFrom-Json
    $filesRaw = @(& gh api --paginate "repos/$Repository/pulls/$Number/files?per_page=100" 2>&1)
    $code = $LASTEXITCODE
    if ($code -ne 0 -or -not $filesRaw) { throw "Could not read files for pull request #$Number." }
    $files = ($filesRaw -join [Environment]::NewLine) | ConvertFrom-Json
    return [pscustomobject]@{
        number = [int]$pull.number
        url = [string]$pull.html_url
        title = [string]$pull.title
        state = if ($pull.merged_at) { 'MERGED' } else { ([string]$pull.state).ToUpperInvariant() }
        author = [pscustomobject]@{login=[string]$pull.user.login;type=[string]$pull.user.type}
        mergedAt = [string]$pull.merged_at
        mergedBy = [pscustomobject]@{login=[string]$pull.merged_by.login}
        mergeCommit = [pscustomobject]@{oid=[string]$pull.merge_commit_sha}
        baseRefName = [string]$pull.base.ref
        headRefName = [string]$pull.head.ref
        files = @($files | ForEach-Object {
            [pscustomobject]@{path=[string]$_.filename;additions=[int]$_.additions;deletions=[int]$_.deletions;changeType=[string]$_.status}
        })
    }
}

function Assert-AppAuthoredPullRequest($PullRequest, [string]$Failure) {
    $expected = Get-AppBotLogin
    if ($PullRequest.author.type -cne 'Bot' -or $PullRequest.author.login -cne $expected) {
        throw $Failure
    }
}

function Assert-AcceptanceAutoMerge($State) {
    if ($env:GITHUB_ACTIONS -ne 'true' -or $env:PHASE8_ACCEPTANCE_AUTOMERGE -ne 'true') {
        throw 'Acceptance auto-merge is available only inside the Phase 8 hosted workflow.'
    }
    if ([string]$State.branch -cnotmatch '^acceptance/phase8-[0-9]+-[0-9]+$') {
        throw 'Acceptance auto-merge refuses non-acceptance branches.'
    }
    if ([string]$State.branch -eq 'main' -or [string]$State.branch -notlike 'acceptance/phase8-*') {
        throw 'Acceptance auto-merge can never target main or a normal branch.'
    }
}

function Invoke-AcceptancePR($State, [string]$Operation, $Parameters, [string]$RequestID) {
    Assert-AcceptanceAutoMerge $State
    $baseSHA = [string]$State.currentRevision
    $proposal = New-Proposal $Operation $Parameters $baseSHA $RequestID
    if ($proposal.encoded.Length -gt 49152) { throw 'Acceptance proposal exceeds the broker limit.' }
    $safe = $Operation.Replace('.', '-')
    $proposalPath = Join-Path $ArtifactRoot "proposals/$safe-$RequestID.json"
    Write-Utf8 $proposalPath (($proposal.request | ConvertTo-Json -Depth 20) + [Environment]::NewLine)

    git fetch origin "$($State.branch):refs/remotes/origin/$($State.branch)" | Out-Null
    if ($LASTEXITCODE -ne 0 -or (git rev-parse "origin/$($State.branch)").Trim() -cne $baseSHA) {
        throw 'Acceptance proposal base is stale.'
    }
    git switch --detach $baseSHA | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not check out the acceptance proposal base.' }
    $validationJSON = & $CliPath broker validate --proposal $proposal.encoded
    if ($LASTEXITCODE -ne 0) { throw 'Acceptance proposal validation failed.' }
    Write-Utf8 (Join-Path $ArtifactRoot "proposals/$safe-$RequestID-validation.json") ($validationJSON + [Environment]::NewLine)
    $validation = $validationJSON | ConvertFrom-Json
    $appliedJSON = & $CliPath broker apply --proposal $proposal.encoded
    if ($LASTEXITCODE -ne 0) { throw 'Acceptance proposal application failed.' }
    Write-Utf8 (Join-Path $ArtifactRoot "proposals/$safe-$RequestID-applied.json") ($appliedJSON + [Environment]::NewLine)
    $applied = $appliedJSON | ConvertFrom-Json
    if ($validation.renderDigest -cne $applied.renderDigest) { throw 'Acceptance render changed between validation and application.' }
    $expected = @($applied.files | ForEach-Object path | Sort-Object -Unique)
    $actual = @(git status --porcelain=v1 --untracked-files=all | ForEach-Object { $_.Substring(3) } | Where-Object { $_ -notlike '.artifacts/*' } | Sort-Object -Unique)
    if (($expected -join "`n") -cne ($actual -join "`n")) { throw 'Acceptance worktree differs from the renderer allowlist.' }

    $requestBranch = "automation/platform/acceptance/phase8-$env:GITHUB_RUN_ID-$env:GITHUB_RUN_ATTEMPT/$safe-$RequestID"
    git switch -c acceptance-request | Out-Null
    git add -- $expected | Out-Null
    git commit -m "$Operation`n`nRequest-ID: $RequestID`nProposal-Digest: $($validation.proposalDigest)`nRender-Digest: $($validation.renderDigest)" | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not commit the acceptance proposal.' }
    git push --set-upstream origin "HEAD:$requestBranch" | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not push the acceptance proposal branch.' }
    $body = "Phase 8 acceptance-only typed lifecycle proposal.`n`n- Request ID: $RequestID`n- Operation: $Operation`n- Base SHA: $baseSHA`n- Proposal digest: $($validation.proposalDigest)`n- Render digest: $($validation.renderDigest)`n- Workflow: https://github.com/$Repository/actions/runs/$env:GITHUB_RUN_ID`n`n> Auto-merge is restricted to the disposable $($State.branch) base."
    $url = gh pr create --repo $Repository --base $State.branch --head $requestBranch --title "test(acceptance): $Operation $($RequestID.Substring(0,8))" --body $body
    if ($LASTEXITCODE -ne 0 -or -not $url) { throw 'Could not create the acceptance pull request.' }
    $number = [int]($url -replace '^.*/','')
    $metadata = Get-PullRequestEvidence $number
    Assert-AppAuthoredPullRequest $metadata 'Acceptance pull request identity or base branch is invalid.'
    if ($metadata.baseRefName -cne $State.branch -or $metadata.state -cne 'OPEN') {
        throw 'Acceptance pull request identity or base branch is invalid.'
    }
    gh pr merge $number --repo $Repository --squash --delete-branch | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not auto-merge the acceptance-only pull request.' }
    $merged = Get-PullRequestEvidence $number
    Assert-AppAuthoredPullRequest $merged 'Acceptance pull request identity changed after merge.'
    if ($merged.state -ne 'MERGED' -or -not $merged.mergeCommit.oid) { throw 'Acceptance pull request did not merge.' }
    Write-Utf8 (Join-Path $ArtifactRoot "pull-requests/$safe-$RequestID.json") (($merged | ConvertTo-Json -Depth 10) + [Environment]::NewLine)
    $State.pullRequests += [pscustomobject]@{operation=$Operation;requestID=$RequestID;proposalDigest=$validation.proposalDigest;renderDigest=$validation.renderDigest;number=$number;url=$merged.url;author=$merged.author.login;mergeActor=$merged.mergedBy.login;mergeCommit=$merged.mergeCommit.oid}
    $State.currentRevision = [string]$merged.mergeCommit.oid
    Save-State $State
    git fetch origin "$($State.branch):refs/remotes/origin/$($State.branch)" | Out-Null
    git switch --detach $State.currentRevision | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not advance to the merged acceptance revision.' }
    git branch -D acceptance-request | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Could not remove the temporary local acceptance branch.' }
    return $merged
}

function Test-KubernetesAPI {
    $ready = @(& kubectl --request-timeout=5s get --raw='/readyz' 2>$null)
    $available = $LASTEXITCODE -eq 0 -and ($ready -join '').Trim() -eq 'ok'
    $global:LASTEXITCODE = 0
    return $available
}

function Capture-Cluster([string]$Prefix) {
    $snapshot = Join-Path $ArtifactRoot "snapshots/$Prefix"
    New-Item -ItemType Directory -Force -Path $snapshot | Out-Null
    if (-not (Test-KubernetesAPI)) {
        Write-Utf8 (Join-Path $snapshot 'api-unavailable.txt') "Kubernetes API unavailable; use the direct host journal and CRI evidence.$([Environment]::NewLine)"
        return
    }
    foreach ($item in @(
        @{name='argo';path='/apis/argoproj.io/v1alpha1/namespaces/argocd/applications'},
        @{name='teams';path='/apis/platform.steadystate.dev/v1alpha1/teams'},
        @{name='applications';path='/apis/platform.steadystate.dev/v1alpha1/applications'},
        @{name='databases';path='/apis/platform.steadystate.dev/v1alpha1/databases'},
        @{name='rollouts';path='/apis/argoproj.io/v1alpha1/rollouts'},
        @{name='analysisruns';path='/apis/argoproj.io/v1alpha1/analysisruns'},
        @{name='routes';path='/apis/gateway.networking.k8s.io/v1/httproutes'},
        @{name='pods';path='/api/v1/pods'},
        @{name='services';path='/api/v1/services'}
    )) {
        $lines = @(& kubectl --request-timeout=5s get --raw=$($item.path) 2>&1)
        Write-Utf8 (Join-Path $snapshot "$($item.name).json") (($lines -join [Environment]::NewLine) + [Environment]::NewLine)
        $global:LASTEXITCODE = 0
    }
}

function Capture-Host([string]$Prefix) {
    $snapshot = Join-Path $ArtifactRoot "snapshots/$Prefix-host"
    New-Item -ItemType Directory -Force -Path $snapshot | Out-Null
    $names = @(& docker ps -a --filter 'label=io.x-k8s.kind.cluster=steadystate' --format '{{.Names}}' 2>&1)
    Write-Utf8 (Join-Path $snapshot 'kind-containers.txt') (($names -join [Environment]::NewLine) + [Environment]::NewLine)
    $stats = @(& docker stats --no-stream --format '{{.Name}} cpu={{.CPUPerc}} memory={{.MemUsage}} pids={{.PIDs}}' @($names) 2>&1)
    Write-Utf8 (Join-Path $snapshot 'docker-stats.txt') (($stats -join [Environment]::NewLine) + [Environment]::NewLine)
    foreach ($name in @($names | Where-Object { $_ -match '^steadystate-(control-plane|worker[0-9]*)$' })) {
        $inspect = @(& docker inspect $name 2>&1)
        Write-Utf8 (Join-Path $snapshot "$name-inspect.json") (($inspect -join [Environment]::NewLine) + [Environment]::NewLine)
        $logs = @(& docker logs --tail 500 $name 2>&1)
        Write-Utf8 (Join-Path $snapshot "$name.log") (($logs -join [Environment]::NewLine) + [Environment]::NewLine)
        $journal = @(& docker exec $name journalctl --no-pager -n 1000 -u kubelet.service -u containerd.service 2>&1)
        Write-Utf8 (Join-Path $snapshot "$name-journal.log") (($journal -join [Environment]::NewLine) + [Environment]::NewLine)
        $runtime = @(& docker exec $name crictl ps -a 2>&1)
        Write-Utf8 (Join-Path $snapshot "$name-crictl.txt") (($runtime -join [Environment]::NewLine) + [Environment]::NewLine)
    }
    $global:LASTEXITCODE = 0
}

function Capture-Logs([string]$Prefix) {
    $directory = Join-Path $ArtifactRoot 'logs'
    New-Item -ItemType Directory -Force -Path $directory | Out-Null
    if (-not (Test-KubernetesAPI)) {
        Write-Utf8 (Join-Path $directory "$Prefix-api-unavailable.txt") "Kubernetes API unavailable; component logs could not be queried safely.$([Environment]::NewLine)"
        return
    }
    foreach ($target in @(
        @{name='operator';namespace='steadystate-system';selector='control-plane=controller-manager'},
        @{name='argo';namespace='argocd';selector='app.kubernetes.io/name=argocd-application-controller'},
        @{name='cnpg';namespace='cnpg-system';selector='app.kubernetes.io/name=cloudnative-pg'},
        @{name='xyz-web';namespace='team-xyz';selector='app.kubernetes.io/instance=xyz'},
        @{name='xyz-api';namespace='team-xyz';selector='app.kubernetes.io/instance=xyz-api'}
    )) {
        $lines = @(& kubectl --request-timeout=20s logs -n $target.namespace -l $target.selector --all-containers --tail=500 2>&1)
        Write-Utf8 (Join-Path $directory "$Prefix-$($target.name).log") (($lines -join [Environment]::NewLine) + [Environment]::NewLine)
        $global:LASTEXITCODE = 0
    }
}

function Assert-NoSecrets {
    $forbidden = '(?i)(-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----|AGE-SECRET-KEY-|github_pat_|gh[pousr]_[A-Za-z0-9_]{20,}|postgresql://[^\[]+:[^\[]+@)'
    foreach ($file in Get-ChildItem $ArtifactRoot -Recurse -File -ErrorAction SilentlyContinue) {
        if ($file.Extension -eq '.gif') { continue }
        if ((Get-Content -Raw -LiteralPath $file.FullName -ErrorAction SilentlyContinue) -match $forbidden) {
            throw "Secret-like material was detected in $($file.FullName)."
        }
    }
}

switch ($Stage) {
    'Prepare' {
        New-Item -ItemType Directory -Force -Path $ArtifactRoot | Out-Null
        Write-Utf8 $TranscriptPath "SteadyState Phase 8 - developer golden path and safe retirement$([Environment]::NewLine)"
        $sourceSHA = if ($env:PHASE8_SOURCE_SHA) { $env:PHASE8_SOURCE_SHA } elseif ($env:GITHUB_SHA) { $env:GITHUB_SHA } else { (git rev-parse HEAD).Trim() }
        if ($sourceSHA -cnotmatch '^[0-9a-f]{40}$') { throw 'Phase 8 source SHA is invalid.' }
        $branch = "acceptance/phase8-$env:GITHUB_RUN_ID-$env:GITHUB_RUN_ATTEMPT"
        if ($branch -cnotmatch '^acceptance/phase8-[0-9]+-[0-9]+$') { throw 'Phase 8 branch is invalid.' }
        gh api --method POST "repos/$Repository/git/refs" -f ref="refs/heads/$branch" -f sha=$sourceSHA | Out-Null
        if ($LASTEXITCODE -ne 0) { throw 'Could not create the Phase 8 acceptance branch.' }
        $humanPRs = @()
        foreach ($number in @(65,67)) {
            $pr = Get-PullRequestEvidence $number
            Assert-AppAuthoredPullRequest $pr "Human-reviewed Phase 8 PR #$number is missing or was not App-authored."
            if ($pr.state -ne 'MERGED' -or -not $pr.mergedAt -or -not $pr.mergeCommit.oid) {
                throw "Human-reviewed Phase 8 PR #$number is missing or was not App-authored."
            }
            $humanPRs += $pr
        }
        Write-Utf8 (Join-Path $ArtifactRoot 'pull-requests/human-scaffold-and-activation.json') (($humanPRs | ConvertTo-Json -Depth 20) + [Environment]::NewLine)
        $state = [pscustomobject]@{
            schemaVersion=1;phase='8';result='running';sourceSHA=$sourceSHA;branch=$branch;currentRevision=$sourceSHA
            startedAt=(Get-Date).ToUniversalTime().ToString('o');completedAt=$null;failedAt=$null;lastError=$null;failureMessage=$null;currentStage='prepared';stageStartedAt=$null
            humanPullRequests=@($humanPRs | ForEach-Object { [pscustomobject]@{number=$_.number;url=$_.url;author=$_.author.login;mergeActor=$_.mergedBy.login;mergeCommit=$_.mergeCommit.oid} })
            pullRequests=@();checks=@();requestID=$null;requestTraceID=$null;ordersChecksum=$null;retainedObjectCount=0
            cli=[pscustomobject]@{version='v0.8.0';binaryBytes=0;statusLatencyMilliseconds=0}
        }
        Save-State $state
        Write-Transcript "BRANCH $branch"
        Write-Host "PHASE8_ACCEPTANCE_BRANCH=$branch"
    }
    'Stabilize' {
        Set-HostedControlPlanePriority
        Write-Host 'Hosted kind control-plane resource priority is verified.'
    }
    'AwaitFoundation' {
        $state = Get-State
        Set-AcceptanceStage $state 'platform-foundation-readiness'
        Wait-PlatformFoundationReady 1200
        Wait-ControlPlaneStable 300
        Write-Host 'Full-profile platform foundation and control plane are stable.'
    }
    'Test' {
        $state = Get-State
        try {
            Set-AcceptanceStage $state 'cli-and-live-golden-path'
            Wait-ControlPlaneStable 300
            $started = Get-Date
            $version = Invoke-Platformctl @('version') (Join-Path $ArtifactRoot 'cli/version.json')
            $null = Invoke-Platformctl @('cluster','status') (Join-Path $ArtifactRoot 'cli/cluster-status.json')
            Wait-Ready 'databases.platform.steadystate.dev' 'xyz' $Namespace 900
            Wait-Ready 'applications.platform.steadystate.dev' 'xyz' $Namespace 600
            Wait-Ready 'applications.platform.steadystate.dev' 'xyz-api' $Namespace 600
            Assert-TenantFilterIsolation
            Capture-RenderedGitOps 'baseline' $state.currentRevision
            $null = Invoke-Platformctl @('team','status','xyz') (Join-Path $ArtifactRoot 'cli/team-status.json')
            $null = Invoke-Platformctl @('database','status','xyz','-n',$Namespace) (Join-Path $ArtifactRoot 'cli/database-status.json')
            $null = Invoke-Platformctl @('database','backups','xyz','-n',$Namespace) (Join-Path $ArtifactRoot 'cli/database-backups.json')
            $null = Invoke-Platformctl @('app','status','xyz','-n',$Namespace) (Join-Path $ArtifactRoot 'cli/web-status.json')
            $status = Invoke-Platformctl @('app','status','xyz-api','-n',$Namespace) (Join-Path $ArtifactRoot 'cli/api-status.json')
            $state.cli.binaryBytes = (Get-Item $CliPath).Length
            $state.cli.statusLatencyMilliseconds = $status.elapsedMilliseconds
            Save-State $state
            Add-Check $state 'exact-cli-and-full-stack-healthy' $started 'The exact-revision CLI observed Team, Database, web, API, backups, and cluster health.'
            $started = Get-Date
            Add-Check $state 'tenant-filter-isolation' $started 'The live root rendered only xyz; the payments Argo child, Team, and namespace were absent.'

            Set-AcceptanceStage $state 'frontend-api-and-database'
            $started = Get-Date
            $requestID = "phase8-$([guid]::NewGuid().ToString('N'))"
            $headers = @{Host='xyz.team-xyz.steadystate.localtest.me';'X-Request-ID'=$requestID}
            $front = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/" -Headers $headers -TimeoutSec 20
            $created = Invoke-WebRequest -UseBasicParsing -Method Post -Uri "http://127.0.0.1:$HttpPort/api/orders" -Headers $headers -ContentType 'application/json' -Body '{"item":"phase-eight","quantity":8}' -TimeoutSec 20
            $listed = Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HttpPort/api/orders" -Headers $headers -TimeoutSec 20
            if ($front.StatusCode -ne 200 -or $created.StatusCode -notin @(200,201) -or $listed.StatusCode -ne 200 -or $listed.Content -notmatch 'phase-eight') {
                throw 'Frontend, same-origin API, or PostgreSQL behavior failed.'
            }
            $orders = @($listed.Content | ConvertFrom-Json | Sort-Object id)
            $canonical = ($orders | ConvertTo-Json -Depth 10 -Compress)
            $hash = [Security.Cryptography.SHA256]::Create()
            try { $checksum = ([BitConverter]::ToString($hash.ComputeHash([Text.Encoding]::UTF8.GetBytes($canonical))) -replace '-','').ToLowerInvariant() } finally { $hash.Dispose() }
            $state.requestID = $requestID
            $state.ordersChecksum = $checksum
            Save-State $state
            Write-Utf8 (Join-Path $ArtifactRoot 'snapshots/orders.json') (($orders | ConvertTo-Json -Depth 10) + [Environment]::NewLine)
            Add-Check $state 'frontend-same-origin-api-and-postgresql' $started 'The React frontend served successfully and its same-origin API persisted and read PostgreSQL data.'

            Set-AcceptanceStage $state 'provenance-rollout-telemetry-and-policy'
            $started = Get-Date
            $null = Invoke-Platformctl @('app','provenance','xyz','-n',$Namespace) (Join-Path $ArtifactRoot 'cli/web-provenance.json')
            $null = Invoke-Platformctl @('app','provenance','xyz-api','-n',$Namespace) (Join-Path $ArtifactRoot 'cli/api-provenance.json')
            $null = Invoke-Platformctl @('app','rollout','xyz-api','-n',$Namespace) (Join-Path $ArtifactRoot 'cli/api-rollout.json')
            $logsPath = Join-Path $ArtifactRoot 'telemetry/logs.json'
            $logJSON = Invoke-PlatformctlUntilMatch `
                @('app','logs','xyz-api','-n',$Namespace,'--historical','--since','15m','--tail','100') `
                $logsPath ([regex]::Escape($requestID)) 'The request ID did not appear in Loki within four minutes.' 240
            $logResponse = $logJSON | ConvertFrom-Json
            $logLines = @($logResponse.data.result | ForEach-Object { $_.values | ForEach-Object { [string]$_[1] } })
            $requestLog = @($logLines | Where-Object { $_ -like "*$requestID*" }) | Select-Object -First 1
            if (-not $requestLog) { throw 'The request ID was not present in Loki.' }
            $logRecord = $requestLog | ConvertFrom-Json
            $traceID = [string]$logRecord.trace_id
            if ($traceID -cnotmatch '^[0-9a-f]{32}$') { throw 'The correlated Loki record has no valid trace ID.' }
            $state.requestTraceID = $traceID
            Save-State $state
            $tracesPath = Join-Path $ArtifactRoot 'telemetry/traces.json'
            # Tempo's OTLP/protobuf JSON represents bytes fields as Base64 even
            # though its query path and application logs use canonical hex.
            $tempoTraceID = Convert-TraceHexToBase64 $traceID
            $null = Invoke-PlatformctlUntilMatch `
                @('app','traces','xyz-api','-n',$Namespace,'--trace-id',$traceID) `
                $tracesPath ([regex]::Escape($tempoTraceID)) 'Tempo did not return the correlated trace ID within four minutes.' 240
            $null = Invoke-Platformctl @('app','slo','xyz-api','-n',$Namespace) (Join-Path $ArtifactRoot 'telemetry/slo.json')
            $null = Invoke-Platformctl @('app','policy','xyz-api','-n',$Namespace) (Join-Path $ArtifactRoot 'security/policy.json')
            $null = Invoke-Platformctl @('app','doctor','xyz-api','-n',$Namespace) (Join-Path $ArtifactRoot 'doctor/healthy.json')
            & go test -v ./internal/platformctl -run 'TestApplicationDoctorFailureFixtures|TestBreakGlass' -count=1 2>&1 |
                Set-Content -Encoding utf8 (Join-Path $ArtifactRoot 'doctor/failure-and-break-glass-fixtures.txt')
            if ($LASTEXITCODE -ne 0) { throw 'Hosted diagnosis or break-glass contract fixtures failed.' }
            $rejected = Invoke-Platformctl @('app','abort','xyz-api','-n',$Namespace,'--reason','acceptance rejection proof','--confirm','wrong-name') (Join-Path $ArtifactRoot 'audit/rejected-confirmation.json') -AllowFailure
            if ($rejected.exitCode -ne 2) { throw 'Break-glass exact-name confirmation did not fail with exit code 2.' }
            Capture-Cluster 'healthy'
            Add-Check $state 'canary-provenance-telemetry-policy-and-diagnosis' $started 'Canary state, provenance, correlated telemetry, SLO, policy, nine-step doctor, four failure fixtures, and break-glass rejection were proven.'

            Set-AcceptanceStage $state 'resource-budget'
            $started = Get-Date
            $query = [Uri]::EscapeDataString('sum(container_memory_working_set_bytes{container!="",image!=""}) / 1024 / 1024')
            $raw = @(& kubectl --request-timeout=20s get --raw "/api/v1/namespaces/monitoring/services/http:monitoring-kube-prometheus-prometheus:9090/proxy/api/v1/query?query=$query")
            if ($LASTEXITCODE -ne 0 -or -not $raw) { throw 'Could not measure full-profile memory.' }
            $memory = (($raw -join '') | ConvertFrom-Json).data.result
            $totalMiB = if (@($memory).Count -eq 1) { [double]$memory[0].value[1] } else { 0 }
            if ($totalMiB -le 0 -or $totalMiB -gt 8192) { throw "Full-profile memory $totalMiB MiB violates the 8 GiB budget." }
            Write-Utf8 (Join-Path $ArtifactRoot 'benchmarks/resource-usage.json') (([ordered]@{totalMiB=[Math]::Round($totalMiB,3);cliBinaryBytes=$state.cli.binaryBytes;cliStatusLatencyMilliseconds=$state.cli.statusLatencyMilliseconds} | ConvertTo-Json) + [Environment]::NewLine)
            Add-Check $state 'full-profile-and-cli-resource-budget' $started 'The full profile stayed within 8 GiB and CLI size/latency were recorded without an in-cluster CLI component.'

            Set-AcceptanceStage $state 'two-stage-reviewed-retirement'
            $started = Get-Date
            $beforeInventory = @(& (Join-Path $PSScriptRoot 'backup-store.ps1') -Action Inventory)
            $approvalID = [guid]::NewGuid().ToString()
            $approval = Invoke-AcceptancePR $state 'service.retire' ([ordered]@{team='xyz';name='xyz'}) $approvalID
            Wait-ArgoRevision $state.currentRevision 480
            Wait-Until 240 'Retirement annotations or the tenant Argo cascade finalizer were not visible.' {
                $resources = @(
                    @('team','xyz','',''),
                    @('applications.platform.steadystate.dev','xyz','-n',$Namespace),
                    @('applications.platform.steadystate.dev','xyz-api','-n',$Namespace),
                    @('databases.platform.steadystate.dev','xyz','-n',$Namespace)
                )
                foreach ($entry in $resources) {
                    $args = @('get',$entry[0],$entry[1]) + @($entry[2..3] | Where-Object { $_ }) + @('-o',"jsonpath={.metadata.annotations.steadystate\.dev/deletion-request}")
                    $value = @(& kubectl --request-timeout=10s @args 2>$null)
                    if ($LASTEXITCODE -ne 0 -or ($value -join '') -cne $approvalID) { return $false }
                }
                $argoRaw = @(& kubectl --request-timeout=10s get applications.argoproj.io xyz -n argocd -o json 2>$null)
                if ($LASTEXITCODE -ne 0 -or -not $argoRaw) { return $false }
                $argoApplication = ($argoRaw -join [Environment]::NewLine) | ConvertFrom-Json
                if (-not (@($argoApplication.metadata.finalizers) -contains 'resources-finalizer.argocd.argoproj.io')) { return $false }
                return $true
            }
            Capture-RenderedGitOps 'retiring' $state.currentRevision
            Capture-Cluster 'retiring'
            $finalizeID = [guid]::NewGuid().ToString()
            $null = Invoke-AcceptancePR $state 'service.finalize' ([ordered]@{team='xyz';name='xyz';deletionRequest=$approvalID;approvalRevision=$approval.mergeCommit.oid}) $finalizeID
            Capture-RenderedGitOps 'finalized' $state.currentRevision
            Wait-Until 900 'Finalizer-driven Team retirement did not remove every live resource.' {
                $team = @(& kubectl --request-timeout=10s get team xyz --ignore-not-found -o name 2>$null)
                $namespace = @(& kubectl --request-timeout=10s get namespace $Namespace --ignore-not-found -o name 2>$null)
                $argo = @(& kubectl --request-timeout=10s get applications.argoproj.io xyz -n argocd --ignore-not-found -o name 2>$null)
                $apps = @(& kubectl --request-timeout=10s get applications.platform.steadystate.dev -n $Namespace --ignore-not-found -o name 2>$null)
                $databases = @(& kubectl --request-timeout=10s get databases.platform.steadystate.dev -n $Namespace --ignore-not-found -o name 2>$null)
                return $LASTEXITCODE -eq 0 -and ($team.Count+$namespace.Count+$argo.Count+$apps.Count+$databases.Count) -eq 0
            }
            $afterInventory = @(& (Join-Path $PSScriptRoot 'backup-store.ps1') -Action Inventory)
            if ($LASTEXITCODE -ne 0 -or $afterInventory.Count -le $beforeInventory.Count) { throw 'Final Database backup was not retained externally.' }
            $state.retainedObjectCount = $afterInventory.Count
            Save-State $state
            Write-Utf8 (Join-Path $ArtifactRoot 'backup/retained-object-inventory.txt') (($afterInventory -join [Environment]::NewLine) + [Environment]::NewLine)
            Capture-Cluster 'retired'
            Add-Check $state 'app-authored-two-pr-finalizer-retirement' $started 'Acceptance-only App PRs approved and finalized retirement; finalizers removed the Team stack and retained the final Database backup.'

            Set-AcceptanceStage $state 'cleanup-proof'
            $started = Get-Date
            $open = gh pr list --repo $Repository --base $state.branch --state open --json number | ConvertFrom-Json
            if ($LASTEXITCODE -ne 0 -or @($open).Count -ne 0) { throw 'Acceptance pull requests remain open.' }
            $requestBranches = gh api "repos/$Repository/git/matching-refs/heads/automation/platform/acceptance/phase8-$env:GITHUB_RUN_ID-$env:GITHUB_RUN_ATTEMPT" | ConvertFrom-Json
            if ($LASTEXITCODE -ne 0 -or @($requestBranches).Count -ne 0) { throw 'Acceptance request branches remain.' }
            Add-Check $state 'no-residual-live-or-request-resources' $started 'No Team namespace, CR, Argo child, generated workload, open acceptance PR, or request branch remained.'

            Capture-Logs 'success'
            $state.result = 'passed'
            $state.completedAt = (Get-Date).ToUniversalTime().ToString('o')
            $state.currentStage = 'completed'
            Save-State $state
            Write-Transcript "RESULT $($state.completedAt) PASSED"
            Write-Host 'PHASE8_ACCEPTANCE_RESULT_PASSED'
        } catch {
            $state.result = 'failed'
            $state.failedAt = (Get-Date).ToUniversalTime().ToString('o')
            $state.lastError = (([string]$_.Exception.Message) -replace '(?i)(token|password|secret)=\S+','$1=[REDACTED]') -replace 'postgresql://\S+','postgresql://[REDACTED]'
            Save-State $state
            Write-Transcript "RESULT $($state.failedAt) FAILED stage=$($state.currentStage) error=$($state.lastError)"
            Capture-Cluster 'failure'
            Capture-Logs 'failure'
            Write-Host "PHASE8_ACCEPTANCE_RESULT_FAILED stage=$($state.currentStage)"
            throw
        }
    }
    'Finalize' {
        $state = Get-State
        $names = @($state.checks | ForEach-Object name)
        $required = @('exact-cli-and-full-stack-healthy','tenant-filter-isolation','frontend-same-origin-api-and-postgresql','canary-provenance-telemetry-policy-and-diagnosis','full-profile-and-cli-resource-budget','app-authored-two-pr-finalizer-retirement','no-residual-live-or-request-resources')
        if ($state.schemaVersion -ne 1 -or $state.phase -ne '8' -or $state.result -ne 'passed' -or $state.currentStage -ne 'completed' -or
            @($state.checks).Count -ne $required.Count -or @($names | Sort-Object -Unique).Count -ne $required.Count -or
            @($required | Where-Object { $_ -notin $names }).Count -ne 0 -or @($state.pullRequests).Count -ne 2 -or
            @($state.humanPullRequests).Count -ne 2 -or $state.retainedObjectCount -lt 1 -or -not $state.ordersChecksum) {
            throw 'Phase 8 acceptance evidence is incomplete.'
        }
        $gif = Join-Path $ArtifactRoot 'phase8-zero-to-live.gif'
        if (-not (Test-Path -LiteralPath $gif -PathType Leaf) -or (Get-Item $gif).Length -le 0) { throw 'Phase 8 recording is missing.' }
        Assert-NoSecrets
        Copy-Item -LiteralPath $StatePath -Destination $EvidencePath -Force
        Write-Host 'Phase 8 acceptance evidence finalized.'
    }
    'CaptureFailure' {
        New-Item -ItemType Directory -Force -Path $ArtifactRoot | Out-Null
        if (Test-Path -LiteralPath $StatePath) {
            $state = Get-State
            $state.failureMessage = if ($env:PHASE8_FAILURE_MESSAGE) { $env:PHASE8_FAILURE_MESSAGE } else { 'Phase 8 acceptance failed.' }
            if ($state.result -ne 'passed') {
                $state.result = 'failed'
                if (-not $state.failedAt) { $state.failedAt = (Get-Date).ToUniversalTime().ToString('o') }
                if (-not $state.lastError) { $state.lastError = [string]$state.failureMessage }
                Write-Transcript "RESULT $($state.failedAt) FAILED stage=$($state.currentStage) error=$($state.lastError)"
            }
            Save-State $state
            Copy-Item -LiteralPath $StatePath -Destination (Join-Path $ArtifactRoot 'failure-evidence.json') -Force
        } else {
            Write-Utf8 (Join-Path $ArtifactRoot 'failure-evidence.json') (([ordered]@{schemaVersion=1;phase='8';result='failed';message=$env:PHASE8_FAILURE_MESSAGE} | ConvertTo-Json) + [Environment]::NewLine)
        }
        Capture-Host 'failure-capture'
        Capture-Cluster 'failure-capture'
        Capture-Logs 'failure-capture'
        Assert-NoSecrets
    }
}
