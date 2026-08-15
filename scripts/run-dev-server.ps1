[CmdletBinding()]
param(
    [string]$Listen = "127.0.0.1:8787"
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$tokenPath = Join-Path $repoRoot "dev-token.txt"
$databasePath = Join-Path $repoRoot "data\dev-sync.db"
if (-not (Test-Path -LiteralPath $tokenPath -PathType Leaf)) {
    Push-Location $repoRoot
    try {
        $token = (& go run .\cmd\obsidian-sync-server token).Trim()
    } finally {
        Pop-Location
    }
    [System.IO.File]::WriteAllText($tokenPath, $token, [System.Text.UTF8Encoding]::new($false))
    Write-Warning "Development API token (stored in ignored dev-token.txt):"
    Write-Host $token
}
Push-Location $repoRoot
try {
    & go run .\cmd\obsidian-sync-server serve --listen $Listen --database $databasePath --token-file $tokenPath
} finally {
    Pop-Location
}
