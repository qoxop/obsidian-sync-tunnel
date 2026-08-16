[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$')]
    [string]$Version
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$envPath = Join-Path $repoRoot ".env"
if (-not (Test-Path -LiteralPath $envPath -PathType Leaf)) {
    throw "Missing .env. Run scripts\docker-init.ps1 first."
}

$original = [System.IO.File]::ReadAllText($envPath)
$lines = [System.IO.File]::ReadAllLines($envPath)
$found = $false
for ($index = 0; $index -lt $lines.Length; $index += 1) {
    if ($lines[$index] -match '^OBSIDIAN_SYNC_VERSION=') {
        $lines[$index] = "OBSIDIAN_SYNC_VERSION=$Version"
        $found = $true
    }
}
if (-not $found) {
    $lines += "OBSIDIAN_SYNC_VERSION=$Version"
}

try {
    [System.IO.File]::WriteAllText($envPath, (($lines -join "`n") + "`n"), [System.Text.UTF8Encoding]::new($false))
    Push-Location $repoRoot
    try {
        & docker compose config --quiet
        if ($LASTEXITCODE -ne 0) { throw "Updated Compose configuration is invalid" }
    } finally {
        Pop-Location
    }
} catch {
    [System.IO.File]::WriteAllText($envPath, $original, [System.Text.UTF8Encoding]::new($false))
    throw
}

Write-Host "Docker deployment version set to $Version in $envPath"
Write-Host "Run scripts\docker-up.ps1 to build and start this version."
