[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [ValidateSet('Decrypt','Verify','Rotate','Apply','ApplyEphemeral')]
    [string]$Action,
    [string]$KeyPath
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
if (-not $KeyPath) {
    $KeyPath = Join-Path $Root '.artifacts/secrets/steadystate.agekey'
}
$Platform = if ($env:OS -eq 'Windows_NT') { 'windows-amd64' } else { 'linux-amd64' }
$Extension = if ($env:OS -eq 'Windows_NT') { '.exe' } else { '' }
$Sops = Join-Path $Root ".tools/bin/$Platform/sops$Extension"
$Encrypted = Join-Path $Root 'gitops/secrets/grafana-admin.enc.yaml'
$RenderedDirectory = Join-Path $Root '.artifacts/secrets/rendered'
$Rendered = Join-Path $RenderedDirectory 'grafana-admin.yaml'
$Recipient = 'age19nqqe30cjcegfagf63ccamqd9a2qw9vv6xavscjcldsz6u2mpf3sm6qs0p'

if (-not (Test-Path -LiteralPath $Sops -PathType Leaf)) {
    throw 'The checksum-verified SOPS binary is missing. Run scripts/dev.ps1 tools.'
}

function Set-KeyEnvironment {
    if ($env:SOPS_AGE_KEY) {
        return
    }
    if (-not (Test-Path -LiteralPath $KeyPath -PathType Leaf)) {
        throw 'No SOPS age key is available. Set SOPS_AGE_KEY or restore the ignored local key file.'
    }
    $env:SOPS_AGE_KEY_FILE = (Resolve-Path -LiteralPath $KeyPath).Path
}

function Invoke-Sops {
    param([Parameter(Mandatory)][string[]]$Arguments)
    & $Sops @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "SOPS exited with code $LASTEXITCODE."
    }
}

function New-RandomPassword {
    $passwordBytes = [byte[]]::new(32)
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($passwordBytes)
    } finally {
        $generator.Dispose()
    }
    return [Convert]::ToBase64String($passwordBytes)
}

switch ($Action) {
    'Rotate' {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Encrypted), $RenderedDirectory | Out-Null
        $password = New-RandomPassword
        $plain = @"
apiVersion: v1
kind: Secret
metadata:
  name: steadystate-grafana-admin
  namespace: monitoring
type: Opaque
stringData:
  admin-user: admin
  admin-password: $password
"@
        [IO.File]::WriteAllText($Rendered, $plain, [Text.UTF8Encoding]::new($false))
        try {
            Invoke-Sops @('--encrypt','--filename-override','gitops/secrets/grafana-admin.enc.yaml','--age',$Recipient,'--encrypted-regex','^(data|stringData)$','--output',$Encrypted,$Rendered)
        } finally {
            Remove-Item -LiteralPath $Rendered -Force -ErrorAction SilentlyContinue
        }
        Write-Host 'Rotated the encrypted Grafana administrator Secret.'
    }
    'Decrypt' {
        Set-KeyEnvironment
        New-Item -ItemType Directory -Force -Path $RenderedDirectory | Out-Null
        Invoke-Sops @('--decrypt','--output',$Rendered,$Encrypted)
        Write-Host "Decrypted the Grafana Secret to the ignored short-lived path $Rendered"
    }
    'Verify' {
        Set-KeyEnvironment
        $null = & $Sops --decrypt $Encrypted
        if ($LASTEXITCODE -ne 0) {
            throw 'The encrypted Grafana Secret cannot be decrypted with the configured age identity.'
        }
        $trackedPlaintext = @(git -C $Root ls-files | Where-Object { $_ -match '(?i)(^|/)(grafana-admin\.yaml|.*\.agekey)$' })
        if ($trackedPlaintext.Count -gt 0) {
            throw "Tracked plaintext secret material found: $($trackedPlaintext -join ', ')"
        }
        Write-Host 'Encrypted secret custody and decryption verified.'
    }
    'Apply' {
        Set-KeyEnvironment
        New-Item -ItemType Directory -Force -Path $RenderedDirectory | Out-Null
        try {
            Invoke-Sops @('--decrypt','--output',$Rendered,$Encrypted)
            & kubectl apply --server-side --force-conflicts -f $Rendered
            if ($LASTEXITCODE -ne 0) {
                throw 'Applying the decrypted Grafana Secret failed.'
            }
        } finally {
            Remove-Item -LiteralPath $Rendered -Force -ErrorAction SilentlyContinue
        }
        Write-Host 'Applied the encrypted Grafana Secret without retaining plaintext.'
    }
    'ApplyEphemeral' {
        $secret = [ordered]@{
            apiVersion = 'v1'
            kind = 'Secret'
            metadata = [ordered]@{
                name = 'steadystate-grafana-admin'
                namespace = 'monitoring'
            }
            type = 'Opaque'
            stringData = [ordered]@{
                'admin-user' = 'admin'
                'admin-password' = New-RandomPassword
            }
        }
        $secret | ConvertTo-Json -Depth 10 | & kubectl apply -f - *> $null
        if ($LASTEXITCODE -ne 0) {
            throw 'Applying the ephemeral Grafana Secret failed.'
        }
        Write-Host 'Applied an ephemeral Grafana administrator Secret for an untrusted/no-key validation context.'
    }
}
