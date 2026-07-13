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
    $dockerIgnore = @(Get-Content -LiteralPath '.dockerignore' -Encoding UTF8)
    foreach ($requiredPattern in @('.git','.tools','.artifacts','m-plan.md','**/m-plan.md','MASTER_PLAN.md','**/MASTER_PLAN.md')) {
        if ($requiredPattern -notin $dockerIgnore) {
            throw "Docker build context can expose private or local material; .dockerignore is missing: $requiredPattern"
        }
    }
    Write-Host 'Privacy guard passed: private planning material is absent from Git and Docker build contexts.'
} finally {
    Pop-Location
}
