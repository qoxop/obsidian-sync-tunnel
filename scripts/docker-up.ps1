[CmdletBinding()]
param(
    [switch]$NoBuild
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$envPath = Join-Path $repoRoot ".env"
if (-not (Test-Path -LiteralPath $envPath -PathType Leaf)) {
    throw "Missing .env. Run scripts\docker-init.ps1 first."
}

Push-Location $repoRoot
try {
    $arguments = @("compose", "up", "--detach")
    if (-not $NoBuild) { $arguments += "--build" }
    $arguments += "sync-server"
    & docker @arguments
    if ($LASTEXITCODE -ne 0) { throw "docker compose up failed" }

    $containerId = (& docker compose ps --quiet sync-server).Trim()
    if (-not $containerId) { throw "sync-server container was not created" }
    $health = ""
    for ($attempt = 0; $attempt -lt 60; $attempt += 1) {
        $health = (& docker inspect --format "{{.State.Health.Status}}" $containerId).Trim()
        if ($health -eq "healthy") { break }
        if ($health -eq "unhealthy") {
            & docker compose logs --tail 100 sync-server
            throw "sync-server became unhealthy"
        }
        Start-Sleep -Seconds 1
    }
    if ($health -ne "healthy") {
        & docker compose logs --tail 100 sync-server
        throw "sync-server did not become healthy within 60 seconds"
    }
    & docker compose ps sync-server
} finally {
    Pop-Location
}
Write-Host "Docker sync server is healthy. cloudflared should point to the mapped http://127.0.0.1:<port>."
