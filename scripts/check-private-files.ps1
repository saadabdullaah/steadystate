$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
Push-Location $Root
try {
    $patterns = @(
        '(^|/)(m-plan\.md)$',
        '(^|/)(master[-_ ]?plan\.md)$',
        '(^|/)(private[-_ ]?plan\.md)$'
    )
    $tracked = @(git ls-files)
    foreach ($path in $tracked) {
        foreach ($pattern in $patterns) {
            if ($path -match $pattern) {
                throw "Private planning material is tracked: $path"
            }
        }
    }
    Write-Host 'Privacy guard passed: no private planning material is tracked.'
} finally {
    Pop-Location
}
