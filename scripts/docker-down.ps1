[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
Push-Location $repoRoot
try {
    & docker compose down --remove-orphans
    if ($LASTEXITCODE -ne 0) { throw "docker compose down failed" }
} finally {
    Pop-Location
}
Write-Host "Containers and the Compose network were removed. Bind-mounted SQLite data and token files were retained."
