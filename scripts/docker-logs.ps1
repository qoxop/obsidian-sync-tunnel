[CmdletBinding()]
param(
    [int]$Tail = 100,
    [switch]$Follow
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
Push-Location $repoRoot
try {
    $arguments = @("compose", "logs", "--tail", $Tail.ToString())
    if ($Follow) { $arguments += "--follow" }
    $arguments += "sync-server"
    & docker @arguments
    if ($LASTEXITCODE -ne 0) { throw "docker compose logs failed" }
} finally {
    Pop-Location
}
