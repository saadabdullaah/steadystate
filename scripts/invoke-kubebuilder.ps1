[CmdletBinding()]
param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$KubebuilderArguments
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
if ($env:OS -ne 'Windows_NT') { throw 'This wrapper is for invoking Linux-only Kubebuilder from Windows.' }
if ($Root -notmatch '^([A-Za-z]):\\(.*)$') { throw "Cannot map repository path '$Root' into WSL." }

$drive = $Matches[1].ToLowerInvariant()
$relativePath = $Matches[2].Replace('\', '/')
$wslRoot = "/mnt/$drive/$relativePath"
$kubebuilder = "$wslRoot/.tools/bin/linux-amd64/kubebuilder"

& wsl.exe -d Ubuntu --cd $wslRoot -- $kubebuilder @KubebuilderArguments
if ($LASTEXITCODE -ne 0) { throw "Kubebuilder exited with code $LASTEXITCODE" }
