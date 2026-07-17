[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$BaseRevision
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$VersionPath = 'apps/demo-app/VERSION'
$DirectRuntimePaths = @(
    'apps/demo-app/main.go',
    'apps/demo-app/Dockerfile'
)
$ModulePaths = @('go.mod', 'go.sum')

Push-Location $Root
try {
    git cat-file -e "$BaseRevision^{commit}"
    if ($LASTEXITCODE -ne 0) { throw "Base revision '$BaseRevision' is not available." }

    $version = (Get-Content -Raw -LiteralPath $VersionPath).Trim()
    if ($version -cnotmatch '^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$') {
        throw "$VersionPath must contain strict vMAJOR.MINOR.PATCH syntax."
    }

    $changed = @(git diff --name-only "$BaseRevision...HEAD")
    if ($LASTEXITCODE -ne 0) { throw "Could not compare HEAD with '$BaseRevision'." }
    $runtimeChanged = @($DirectRuntimePaths | Where-Object { $_ -in $changed })
    $changedModuleFiles = @($ModulePaths | Where-Object { $_ -in $changed })
    if ($changedModuleFiles.Count -gt 0) {
        $demoModules = @(go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}|{{.Version}}{{end}}{{end}}' ./apps/demo-app | Where-Object { $_ } | Sort-Object -Unique)
        if ($LASTEXITCODE -ne 0) { throw 'Could not resolve the demo dependency graph.' }

        $moduleDiff = @(git diff --unified=0 "$BaseRevision...HEAD" -- @ModulePaths)
        if ($LASTEXITCODE -ne 0) { throw "Could not inspect module changes against '$BaseRevision'." }
        $changedModuleLines = @($moduleDiff | Where-Object { $_ -cmatch '^[+-][^+-]' })
        $affectedModules = @($demoModules | Where-Object {
            $modulePath, $moduleVersion = $_ -split '\|', 2
            $modulePattern = '^[+-]\s*' + [regex]::Escape($modulePath) + '\s+' + [regex]::Escape($moduleVersion) + '(?:\s|/go\.mod\s)'
            $changedModuleLines -cmatch $modulePattern
        })
        $buildDirectiveChanged = @($changedModuleLines | Where-Object { $_ -cmatch '^[+-]\s*(?:go|toolchain|replace|exclude)\s+' }).Count -gt 0
        if ($affectedModules.Count -gt 0 -or $buildDirectiveChanged) {
            $runtimeChanged += $changedModuleFiles
        }
    }
    if ($runtimeChanged.Count -eq 0) {
        Write-Host "Demo VERSION contract passed at $version; no runtime-affecting demo files changed."
        return
    }
    if ($VersionPath -notin $changed) {
        throw "Demo runtime changes require a matching $VersionPath bump. Changed runtime files: $($runtimeChanged -join ', ')"
    }

    $previousPreference = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $baseVersion = @(& git show "$BaseRevision`:$VersionPath" 2>$null)
    $baseExitCode = $LASTEXITCODE
    $ErrorActionPreference = $previousPreference
    if ($baseExitCode -eq 0 -and (($baseVersion -join [Environment]::NewLine).Trim()) -eq $version) {
        throw "$VersionPath changed but its value was not bumped from $version."
    }
    Write-Host "Demo VERSION contract passed: runtime changes declare $version."
} finally {
    Pop-Location
}
