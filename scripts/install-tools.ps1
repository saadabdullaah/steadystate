[CmdletBinding()]
param(
    [switch]$Force,
    [switch]$BaseOnly,
    [switch]$SkipLint
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$VersionsFile = Join-Path $PSScriptRoot 'versions.env'
$IsWindowsHost = $env:OS -eq 'Windows_NT'
$Platform = if ($IsWindowsHost) { 'windows-amd64' } else { 'linux-amd64' }
$ToolsRoot = Join-Path $Root '.tools'
$BinDir = Join-Path $ToolsRoot "bin/$Platform"
$DownloadDir = Join-Path $ToolsRoot 'downloads'
$env:GOCACHE = Join-Path $ToolsRoot "cache/go-build/$Platform"
$env:GOMODCACHE = Join-Path $ToolsRoot "cache/go-mod/$Platform"
$env:GOPATH = Join-Path $ToolsRoot "gopath/$Platform"
if ($IsWindowsHost) { $env:LOCALAPPDATA = Join-Path $ToolsRoot "cache/localappdata/$Platform" }
$cacheDirectories = @($env:GOCACHE, $env:GOMODCACHE, $env:GOPATH)
if ($IsWindowsHost) { $cacheDirectories += $env:LOCALAPPDATA }
New-Item -ItemType Directory -Force -Path $cacheDirectories | Out-Null

function Read-Versions {
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $VersionsFile -Encoding UTF8) {
        $trimmed = $line.Trim()
        if (-not $trimmed -or $trimmed.StartsWith('#')) { continue }
        $parts = $trimmed.Split('=', 2)
        if ($parts.Count -ne 2) { throw "Invalid versions.env line: $line" }
        $values[$parts[0]] = $parts[1]
    }
    return $values
}

function Get-VerifiedFile {
    param(
        [Parameter(Mandatory)][string]$Url,
        [Parameter(Mandatory)][string]$Destination,
        [string]$ExpectedSha256,
        [string]$ChecksumUrl
    )

    if (-not $ExpectedSha256) {
        if (-not $ChecksumUrl) { throw "No checksum source provided for $Url" }
        $checksumPath = "$Destination.sha256"
        Invoke-WebRequest -UseBasicParsing -Uri $ChecksumUrl -OutFile $checksumPath
        $ExpectedSha256 = ((Get-Content -Raw -LiteralPath $checksumPath).Trim() -split '\s+')[0]
    }

    if (Test-Path -LiteralPath $Destination) {
        $existing = (Get-FileHash -Algorithm SHA256 -LiteralPath $Destination).Hash.ToLowerInvariant()
        if (-not $Force -and $existing -eq $ExpectedSha256.ToLowerInvariant()) { return }
        if ($Force) { Remove-Item -LiteralPath $Destination -Force }
    }

    Write-Host "Downloading $Url"
    $curl = Get-Command curl.exe -ErrorAction SilentlyContinue
    if (-not $curl) { $curl = Get-Command curl -ErrorAction SilentlyContinue }
    if ($curl) {
        & $curl.Source --location --fail --retry 3 --continue-at - --output $Destination $Url
        if ($LASTEXITCODE -ne 0) { throw "Download failed: $Url" }
    } else {
        Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $Destination
    }

    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $Destination).Hash.ToLowerInvariant()
    if ($actual -ne $ExpectedSha256.ToLowerInvariant()) {
        Remove-Item -LiteralPath $Destination -Force
        throw "Checksum mismatch for $Url. Expected $ExpectedSha256, got $actual"
    }
}

function Install-DirectBinary {
    param([string]$Name, [string]$Url, [string]$ChecksumUrl, [string]$ExpectedSha256)
    $extension = if ($IsWindowsHost) { '.exe' } else { '' }
    $destination = Join-Path $BinDir "$Name$extension"
    Get-VerifiedFile -Url $Url -Destination $destination -ChecksumUrl $ChecksumUrl -ExpectedSha256 $ExpectedSha256
    if (-not $IsWindowsHost) { & chmod +x $destination }
}

function Install-GoTool {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][string]$Package,
        [Parameter(Mandatory)][string]$Version
    )
    $extension = if ($IsWindowsHost) { '.exe' } else { '' }
    $binary = Join-Path $BinDir "$Name$extension"
    $marker = Join-Path $BinDir "$Name.version"
    if (-not $Force -and (Test-Path -LiteralPath $binary) -and (Test-Path -LiteralPath $marker) -and
        ((Get-Content -Raw -LiteralPath $marker).Trim() -eq $Version)) { return }

    Write-Host "Installing $Name $Version"
    $previousGoBin = $env:GOBIN
    $previousPath = $env:PATH
    try {
        $env:GOBIN = $BinDir
        $env:PATH = "$(Join-Path $goRoot 'bin')$([IO.Path]::PathSeparator)$env:PATH"
        & $goBinary install "$Package@v$Version"
        if ($LASTEXITCODE -ne 0) { throw "Failed to install $Name $Version" }
        [IO.File]::WriteAllText($marker, "$Version`n", [Text.UTF8Encoding]::new($false))
    } finally {
        $env:GOBIN = $previousGoBin
        $env:PATH = $previousPath
    }
}

$v = Read-Versions
New-Item -ItemType Directory -Force -Path $BinDir, $DownloadDir | Out-Null

if ($IsWindowsHost) {
    $goArchive = Join-Path $DownloadDir "go$($v.GO_VERSION).windows-amd64.zip"
    Get-VerifiedFile -Url "https://go.dev/dl/go$($v.GO_VERSION).windows-amd64.zip" -Destination $goArchive -ExpectedSha256 $v.GO_WINDOWS_AMD64_SHA256
    $goRoot = Join-Path $ToolsRoot "go/$Platform"
    $goBinary = Join-Path $goRoot 'bin/go.exe'
    if (Test-Path $goBinary) {
        $installedGo = ((& $goBinary version) -split ' ')[2].TrimStart('go')
        if ($installedGo -ne $v.GO_VERSION) { Remove-Item -Recurse -Force $goRoot }
    }
    if ($Force -and (Test-Path $goRoot)) { Remove-Item -Recurse -Force $goRoot }
    if (-not (Test-Path $goBinary)) {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $goRoot) | Out-Null
        $extractRoot = Join-Path $ToolsRoot 'go-extract'
        if (Test-Path -LiteralPath $extractRoot) { Remove-Item -Recurse -Force $extractRoot }
        New-Item -ItemType Directory -Force -Path $extractRoot | Out-Null
        & tar -xf $goArchive -C $extractRoot
        if ($LASTEXITCODE -ne 0) { throw 'Failed to extract the Go SDK' }
        Move-Item -LiteralPath (Join-Path $extractRoot 'go') -Destination $goRoot
        Remove-Item -Recurse -Force $extractRoot
    }

    Install-DirectBinary -Name 'kubectl' -Url "https://dl.k8s.io/release/v$($v.KUBERNETES_VERSION)/bin/windows/amd64/kubectl.exe" -ChecksumUrl "https://dl.k8s.io/release/v$($v.KUBERNETES_VERSION)/bin/windows/amd64/kubectl.exe.sha256"
    Install-DirectBinary -Name 'kind' -Url "https://kind.sigs.k8s.io/dl/v$($v.KIND_VERSION)/kind-windows-amd64" -ChecksumUrl "https://kind.sigs.k8s.io/dl/v$($v.KIND_VERSION)/kind-windows-amd64.sha256sum"

    $helmArchive = Join-Path $DownloadDir "helm-v$($v.HELM_VERSION)-windows-amd64.zip"
    Get-VerifiedFile -Url "https://get.helm.sh/helm-v$($v.HELM_VERSION)-windows-amd64.zip" -Destination $helmArchive -ChecksumUrl "https://get.helm.sh/helm-v$($v.HELM_VERSION)-windows-amd64.zip.sha256sum"
    $helmExtract = Join-Path $ToolsRoot 'helm-extract'
    Expand-Archive -LiteralPath $helmArchive -DestinationPath $helmExtract -Force
    Copy-Item -Force (Join-Path $helmExtract 'windows-amd64/helm.exe') (Join-Path $BinDir 'helm.exe')
} else {
    $goArchive = Join-Path $DownloadDir "go$($v.GO_VERSION).linux-amd64.tar.gz"
    Get-VerifiedFile -Url "https://go.dev/dl/go$($v.GO_VERSION).linux-amd64.tar.gz" -Destination $goArchive -ExpectedSha256 $v.GO_LINUX_AMD64_SHA256
    $goRoot = Join-Path $ToolsRoot "go/$Platform"
    $goBinary = Join-Path $goRoot 'bin/go'
    if (Test-Path $goBinary) {
        $installedGo = ((& $goBinary version) -split ' ')[2].TrimStart('go')
        if ($installedGo -ne $v.GO_VERSION) { Remove-Item -Recurse -Force $goRoot }
    }
    if ($Force -and (Test-Path $goRoot)) { Remove-Item -Recurse -Force $goRoot }
    if (-not (Test-Path $goBinary)) {
        New-Item -ItemType Directory -Force -Path $goRoot | Out-Null
        & tar -xzf $goArchive --strip-components=1 -C $goRoot
    }

    Install-DirectBinary -Name 'kubectl' -Url "https://dl.k8s.io/release/v$($v.KUBERNETES_VERSION)/bin/linux/amd64/kubectl" -ChecksumUrl "https://dl.k8s.io/release/v$($v.KUBERNETES_VERSION)/bin/linux/amd64/kubectl.sha256"
    Install-DirectBinary -Name 'kind' -Url "https://kind.sigs.k8s.io/dl/v$($v.KIND_VERSION)/kind-linux-amd64" -ChecksumUrl "https://kind.sigs.k8s.io/dl/v$($v.KIND_VERSION)/kind-linux-amd64.sha256sum"
    Install-DirectBinary -Name 'kubebuilder' -Url "https://github.com/kubernetes-sigs/kubebuilder/releases/download/v$($v.KUBEBUILDER_VERSION)/kubebuilder_linux_amd64" -ExpectedSha256 $v.KUBEBUILDER_LINUX_AMD64_SHA256

    $helmArchive = Join-Path $DownloadDir "helm-v$($v.HELM_VERSION)-linux-amd64.tar.gz"
    Get-VerifiedFile -Url "https://get.helm.sh/helm-v$($v.HELM_VERSION)-linux-amd64.tar.gz" -Destination $helmArchive -ChecksumUrl "https://get.helm.sh/helm-v$($v.HELM_VERSION)-linux-amd64.tar.gz.sha256sum"
    & tar -xzf $helmArchive -C $ToolsRoot
    Copy-Item -Force (Join-Path $ToolsRoot 'linux-amd64/helm') (Join-Path $BinDir 'helm')
    & chmod +x (Join-Path $BinDir 'helm')
}

if (-not $BaseOnly) {
    Install-GoTool -Name 'controller-gen' -Package 'sigs.k8s.io/controller-tools/cmd/controller-gen' -Version $v.CONTROLLER_TOOLS_VERSION
    Install-GoTool -Name 'kustomize' -Package 'sigs.k8s.io/kustomize/kustomize/v5' -Version $v.KUSTOMIZE_VERSION
    Install-GoTool -Name 'setup-envtest' -Package 'sigs.k8s.io/controller-runtime/tools/setup-envtest' -Version $v.SETUP_ENVTEST_VERSION
    if (-not $SkipLint) {
        Install-GoTool -Name 'golangci-lint' -Package 'github.com/golangci/golangci-lint/v2/cmd/golangci-lint' -Version $v.GOLANGCI_LINT_VERSION
    }
}

Write-Host "Verified tools installed under $BinDir"
