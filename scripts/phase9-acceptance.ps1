[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateSet('Prepare','VerifyBroker','Finalize','CaptureFailure')]
    [string]$Stage
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$ArtifactRoot = Join-Path $Root '.artifacts/phase9/acceptance'
$StatePath = Join-Path $ArtifactRoot 'state.json'
$EvidencePath = Join-Path $ArtifactRoot 'evidence.json'

function Write-Utf8([string]$Path, [string]$Value) {
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Path) | Out-Null
    [IO.File]::WriteAllText($Path, $Value, [Text.UTF8Encoding]::new($false))
}

function Get-State {
    if (-not (Test-Path -LiteralPath $StatePath)) { throw 'Phase 9 acceptance state is missing.' }
    return Get-Content -Raw -LiteralPath $StatePath | ConvertFrom-Json
}

function Save-State($State) {
    Write-Utf8 $StatePath (($State | ConvertTo-Json -Depth 40) + [Environment]::NewLine)
}

function Add-Check($State, [string]$Name, [string]$Details) {
    $State.checks += [pscustomobject]@{ name=$Name; status='passed'; at=(Get-Date).ToUniversalTime().ToString('o'); details=$Details }
    Save-State $State
}

function Capture-SafeSnapshots {
    $directory = Join-Path $ArtifactRoot 'snapshots'
    New-Item -ItemType Directory -Force $directory | Out-Null
    $queries = @(
        @('applications.platform.steadystate.dev','-A'),
        @('databases.platform.steadystate.dev','-A'),
        @('teams.platform.steadystate.dev'),
        @('rollouts.argoproj.io','-A'),
        @('applications.argoproj.io','-n','argocd'),
        @('httproutes.gateway.networking.k8s.io','-A'),
        @('pods','-A')
    )
    foreach ($query in $queries) {
        $name = ($query -join '-').Replace('/','-')
        $output = @(& kubectl --request-timeout=15s get @query -o wide 2>&1)
        Write-Utf8 (Join-Path $directory "$name.txt") (($output -join [Environment]::NewLine) + [Environment]::NewLine)
    }
}

function Remove-SmokeGitHubResources {
    $proposalPath = Join-Path $ArtifactRoot 'proposal-result.txt'
    if (-not $env:GH_TOKEN -or -not $env:GITHUB_REPOSITORY -or -not (Test-Path -LiteralPath $proposalPath)) { return }
    $proposal = Get-Content -Raw -LiteralPath $proposalPath
    if ($proposal -cnotmatch 'Request ([0-9a-f-]{36}) dispatched\.') { return }
    $branch = "automation/platform/team-create/$($Matches[1])"
    $number = gh pr list --repo $env:GITHUB_REPOSITORY --head $branch --state open --json number --jq '.[0].number' --limit 1 2>$null
    if ($LASTEXITCODE -eq 0 -and $number) {
        gh pr close $number --repo $env:GITHUB_REPOSITORY --delete-branch --comment 'Phase 9 hosted portal cleanup after an incomplete acceptance run.' 2>$null
        return
    }
    gh api --method DELETE "repos/$($env:GITHUB_REPOSITORY)/git/refs/heads/$branch" 2>$null
    if ($LASTEXITCODE -ne 0) { $global:LASTEXITCODE = 0 }
}

switch ($Stage) {
    'Prepare' {
        New-Item -ItemType Directory -Force $ArtifactRoot | Out-Null
        $state = [ordered]@{
            schemaVersion = 'phase9.acceptance.steadystate.dev/v1alpha1'
            sourceSHA = if ($env:PHASE9_SOURCE_SHA) { $env:PHASE9_SOURCE_SHA } else { (git rev-parse HEAD).Trim() }
            runID = $env:GITHUB_RUN_ID
            runAttempt = $env:GITHUB_RUN_ATTEMPT
            startedAt = (Get-Date).ToUniversalTime().ToString('o')
            smokeTeam = "portal-smoke-$($env:GITHUB_RUN_ID)-$($env:GITHUB_RUN_ATTEMPT)".ToLowerInvariant()
            checks = @()
        }
        Save-State $state
        Write-Utf8 (Join-Path $ArtifactRoot 'lifecycle-timeline.txt') "$(Get-Date -Format o) prepare`n"
        Add-Check $state 'acceptance-prepared' 'Artifact root and unique proposal identity created.'
    }
    'VerifyBroker' {
        $proposalPath = Join-Path $ArtifactRoot 'proposal-result.txt'
        if (-not (Test-Path -LiteralPath $proposalPath)) { throw 'The real browser proposal result is missing.' }
        $proposal = Get-Content -Raw -LiteralPath $proposalPath
        if ($proposal -cnotmatch 'Request ([0-9a-f-]{36}) dispatched\. (https://github\.com/[^\s]+)') { throw 'The browser proposal result is malformed.' }
        $runURL = $Matches[2].Trim()
        $runID = [IO.Path]::GetFileName(([Uri]$runURL).AbsolutePath)
        if ($runID -cnotmatch '^[0-9]+$') { throw 'The broker workflow URL does not contain a numeric run ID.' }
        gh run watch $runID --repo $env:GITHUB_REPOSITORY --exit-status
        if ($LASTEXITCODE -ne 0) { throw 'The portal-dispatched broker run failed.' }
        Write-Utf8 (Join-Path $ArtifactRoot 'broker-verified.txt') "broker run passed`n"
    }
    'Finalize' {
        $state = Get-State
        $proposalPath = Join-Path $ArtifactRoot 'proposal-result.txt'
        if (-not (Test-Path -LiteralPath $proposalPath)) { throw 'The real browser proposal result is missing.' }
        $proposal = Get-Content -Raw -LiteralPath $proposalPath
        if ($proposal -cnotmatch 'Request ([0-9a-f-]{36}) dispatched\. (https://github\.com/[^\s]+)') { throw 'The browser proposal result is malformed.' }
        $requestID = $Matches[1]
        $runURL = $Matches[2].Trim()
        $branch = "automation/platform/team-create/$requestID"
        if (-not (Test-Path -LiteralPath (Join-Path $ArtifactRoot 'broker-verified.txt'))) { throw 'The broker success marker is missing.' }
        $number = gh pr list --repo $env:GITHUB_REPOSITORY --head $branch --state open --json number --jq '.[0].number' --limit 1
        if ($LASTEXITCODE -ne 0 -or -not $number) { throw 'The GitHub App smoke pull request was not found.' }
        $prJSON = gh pr view $number --repo $env:GITHUB_REPOSITORY --json number,url,author,headRefName,baseRefName,title,files
        if ($LASTEXITCODE -ne 0 -or -not $prJSON) { throw 'The GitHub App smoke pull request could not be inspected.' }
        $pull = $prJSON | ConvertFrom-Json
        if (-not $pull -or -not $pull.author.is_bot -or $pull.author.login -cne 'app/steadystate-delivery' -or $pull.baseRefName -cne 'main') { throw 'The smoke pull request identity or base is invalid.' }
        $expected = @('gitops/clusters/local/catalog/tenants.yaml', "gitops/teams/$($state.smokeTeam)/kustomization.yaml", "gitops/teams/$($state.smokeTeam)/team.yaml")
        $actual = @($pull.files.path | Sort-Object)
        if (($actual -join "`n") -cne (($expected | Sort-Object) -join "`n")) { throw "Smoke pull request changed unexpected paths: $($actual -join ', ')." }
        Write-Utf8 (Join-Path $ArtifactRoot 'proposal.json') (($pull | ConvertTo-Json -Depth 20) + [Environment]::NewLine)
        $state.requestID = $requestID
        $state.brokerRunURL = $runURL
        $state.pullRequestURL = $pull.url
        $state.proposalBranch = $branch
        Add-Check $state 'github-app-reviewed-proposal' 'The UI submitted a typed proposal and the GitHub App opened the exact three-file smoke PR.'

        gh pr close $pull.number --repo $env:GITHUB_REPOSITORY --delete-branch --comment 'Phase 9 hosted portal smoke completed; this proposal is intentionally not merged.'
        if ($LASTEXITCODE -ne 0) { throw 'Could not close the smoke pull request and remove its branch.' }
        Add-Check $state 'temporary-github-resources-removed' 'Smoke PR closed and automation branch deleted.'

        Capture-SafeSnapshots
        Add-Check $state 'real-platform-snapshots-captured' 'Curated Team, Application, Database, Rollout, Argo, route, and Pod snapshots captured.'

        $screenshots = @(Get-ChildItem -LiteralPath (Join-Path $ArtifactRoot 'screenshots') -Filter '*.png' | Sort-Object Name)
        if ($screenshots.Count -lt 4) { throw 'At least four real portal screenshots are required.' }
        & go run ./cmd/portal-gif (Join-Path $ArtifactRoot 'phase9-portal-golden-path.gif') @($screenshots.FullName)
        if ($LASTEXITCODE -ne 0) { throw 'The deterministic Go GIF compositor failed.' }
        $video = Get-ChildItem -Path (Join-Path $Root '.artifacts/phase9') -Recurse -Filter '*.webm' | Select-Object -First 1
        if (-not $video) { throw 'The full-fidelity Playwright recording is missing.' }
        Copy-Item -LiteralPath $video.FullName -Destination (Join-Path $ArtifactRoot 'phase9-portal-golden-path.webm') -Force

        $assetDirectory = Join-Path $Root 'internal/platformctl/portalassets'
        $assetFiles = @(Get-ChildItem -LiteralPath $assetDirectory -File | Sort-Object Name | ForEach-Object {
            [ordered]@{ name=$_.Name; bytes=$_.Length; sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant() }
        })
        Write-Utf8 (Join-Path $ArtifactRoot 'portal-assets.json') (([ordered]@{ files=$assetFiles; totalBytes=($assetFiles.bytes | Measure-Object -Sum).Sum } | ConvertTo-Json -Depth 10) + [Environment]::NewLine)
        Add-Check $state 'recording-and-assets-verified' 'Real screenshots, WebM, GIF, and embedded asset manifest exist.'

        $state.completedAt = (Get-Date).ToUniversalTime().ToString('o')
        $state.status = 'passed'
        Save-State $state
        Copy-Item -LiteralPath $StatePath -Destination $EvidencePath -Force
    }
    'CaptureFailure' {
        New-Item -ItemType Directory -Force $ArtifactRoot | Out-Null
        $failure = [ordered]@{
            schemaVersion = 'phase9.acceptance.failure.steadystate.dev/v1alpha1'
            sourceSHA = $env:PHASE9_SOURCE_SHA
            capturedAt = (Get-Date).ToUniversalTime().ToString('o')
            stageOutcomes = $env:PHASE9_FAILURE_MESSAGE
        }
        Write-Utf8 (Join-Path $ArtifactRoot 'failure.json') (($failure | ConvertTo-Json) + [Environment]::NewLine)
        try { Capture-SafeSnapshots } catch { Write-Utf8 (Join-Path $ArtifactRoot 'snapshot-error.txt') $_.Exception.Message }
        try { Remove-SmokeGitHubResources } catch { Write-Utf8 (Join-Path $ArtifactRoot 'github-cleanup-error.txt') "Temporary GitHub resource cleanup failed; inspect the workflow run.\n" }
    }
}
