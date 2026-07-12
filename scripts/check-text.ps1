$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Push-Location $Root
try {
    $failed = $false
    foreach ($path in @(git ls-files)) {
        if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { continue }
        $extension = [IO.Path]::GetExtension($path).ToLowerInvariant()
        if ($extension -notin @('.go','.mod','.sum','.yaml','.yml','.json','.md','.sh','.ps1','.txt','.env') -and [IO.Path]::GetFileName($path) -ne 'Makefile') { continue }
        $bytes = [IO.File]::ReadAllBytes((Resolve-Path -LiteralPath $path))
        if ($bytes.Length -ge 2 -and (($bytes[0] -eq 0xff -and $bytes[1] -eq 0xfe) -or ($bytes[0] -eq 0xfe -and $bytes[1] -eq 0xff))) {
            Write-Error "$path is UTF-16; repository text must be UTF-8."
            $failed = $true
        }
        $text = [Text.Encoding]::UTF8.GetString($bytes)
        if ($text.Contains("`r`n")) {
            Write-Error "$path contains CRLF; repository text must use LF."
            $failed = $true
        }
        if ($bytes.Length -gt 0 -and $bytes[-1] -ne 10) {
            Write-Error "$path does not end with a newline."
            $failed = $true
        }
    }
    if ($failed) { exit 1 }
    Write-Host 'Text checks passed.'
} finally {
    Pop-Location
}
