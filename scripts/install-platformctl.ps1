[CmdletBinding()]
param(
    [ValidatePattern('^v[0-9]+\.[0-9]+\.[0-9]+$')][string]$Version = 'v1.0.1',
    [string]$InstallDirectory = (Join-Path $HOME '.local/bin'),
    [switch]$NoPathUpdate
)

$ErrorActionPreference = 'Stop'

# RuntimeInformation can report an enum differently across Windows PowerShell,
# PowerShell 7, and emulated 32-bit processes. Try the runtime value first, then
# the Windows architecture environment variables, and normalize all supported
# spellings before selecting a release archive.
$architectureCandidates = @(
    ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture).ToString()
    $env:PROCESSOR_ARCHITEW6432
    $env:PROCESSOR_ARCHITECTURE
)
$architecture = $null
foreach ($candidate in $architectureCandidates) {
    $candidateText = if ($null -eq $candidate) { '' } else { $candidate.ToString() }
    switch ($candidateText.Trim().ToUpperInvariant()) {
        { $_ -in @('X64', 'AMD64') } { $architecture = 'amd64'; break }
        { $_ -in @('ARM64', 'AARCH64') } { $architecture = 'arm64'; break }
    }
    if ($architecture) { break }
}
if (-not $architecture) {
    $observed = ($architectureCandidates | Where-Object { $_ } | ForEach-Object { $_.ToString() }) -join ', '
    throw "Unsupported Windows architecture. Observed values: $observed"
}

# Windows PowerShell 5.1 may otherwise negotiate a protocol GitHub no longer
# accepts. PowerShell 7 uses HttpClient, for which this is harmless.
if ([Net.ServicePointManager]::SecurityProtocol -notmatch 'Tls12') {
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
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
    $resolvedInstallDirectory = [IO.Path]::GetFullPath($InstallDirectory)
    New-Item -ItemType Directory -Force -Path $resolvedInstallDirectory | Out-Null
    Copy-Item -LiteralPath (Join-Path $extract 'platformctl.exe') -Destination (Join-Path $resolvedInstallDirectory 'platformctl.exe') -Force
    if (-not $NoPathUpdate) {
        $processEntries = @($env:PATH -split ';' | Where-Object { $_ })
        if (-not ($processEntries | Where-Object { $_.TrimEnd('\') -ieq $resolvedInstallDirectory.TrimEnd('\') })) {
            $env:PATH = "$resolvedInstallDirectory;$env:PATH"
        }
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $userEntries = @($userPath -split ';' | Where-Object { $_ })
        if (-not ($userEntries | Where-Object { $_.TrimEnd('\') -ieq $resolvedInstallDirectory.TrimEnd('\') })) {
            [Environment]::SetEnvironmentVariable('Path', ((@($userEntries) + $resolvedInstallDirectory) -join ';'), 'User')
            Write-Host "Added $resolvedInstallDirectory to the user PATH"
        }
    }
    Write-Host "Installed verified platformctl $Version to $resolvedInstallDirectory"
} finally {
    $resolvedTemp = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
    $resolvedTarget = [IO.Path]::GetFullPath($temporaryRoot)
    if ($resolvedTarget.StartsWith($resolvedTemp, [StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $resolvedTarget)) {
        Remove-Item -LiteralPath $resolvedTarget -Recurse -Force
    }
}
