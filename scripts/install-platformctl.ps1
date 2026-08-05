[CmdletBinding()]
param(
    [ValidatePattern('^v[0-9]+\.[0-9]+\.[0-9]+$')][string]$Version = 'v0.8.0',
    [string]$InstallDirectory = (Join-Path $HOME '.local/bin')
)

$ErrorActionPreference = 'Stop'
$architecture = switch ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    'X64' { 'amd64' }
    'Arm64' { 'arm64' }
    default { throw "Unsupported Windows architecture: $_" }
}
$releaseVersion = $Version.TrimStart('v')
$archiveName = "platformctl_${releaseVersion}_windows_${architecture}.zip"
$releaseRoot = "https://github.com/saadabdullaah/steadystate/releases/download/$Version"
$temporaryRoot = Join-Path ([IO.Path]::GetTempPath()) ("steadystate-platformctl-" + [guid]::NewGuid().ToString('N'))

try {
    New-Item -ItemType Directory -Force -Path $temporaryRoot | Out-Null
    $archive = Join-Path $temporaryRoot $archiveName
    $checksums = Join-Path $temporaryRoot 'checksums.txt'
    Invoke-WebRequest -UseBasicParsing -Uri "$releaseRoot/$archiveName" -OutFile $archive
    Invoke-WebRequest -UseBasicParsing -Uri "$releaseRoot/checksums.txt" -OutFile $checksums
    $line = Get-Content -LiteralPath $checksums | Where-Object { $_ -match "\s+$([regex]::Escape($archiveName))$" } | Select-Object -First 1
    if (-not $line) { throw "Release checksum does not contain $archiveName" }
    $expected = ($line -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { throw "Checksum verification failed for $archiveName" }
    $extract = Join-Path $temporaryRoot 'extract'
    Expand-Archive -LiteralPath $archive -DestinationPath $extract
    New-Item -ItemType Directory -Force -Path $InstallDirectory | Out-Null
    Copy-Item -LiteralPath (Join-Path $extract 'platformctl.exe') -Destination (Join-Path $InstallDirectory 'platformctl.exe') -Force
    Write-Host "Installed verified platformctl $Version to $InstallDirectory"
} finally {
    $resolvedTemp = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    $resolvedTarget = [IO.Path]::GetFullPath($temporaryRoot)
    if ($resolvedTarget.StartsWith($resolvedTemp, [StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $resolvedTarget)) {
        Remove-Item -LiteralPath $resolvedTarget -Recurse -Force
    }
}
