[CmdletBinding()]
param(
    [Parameter(Mandatory)] [string]$BackupDirectory,
    [Parameter(Mandatory)] [switch]$ConfirmRestore
)

$ErrorActionPreference = "Stop"
if (-not $ConfirmRestore) { throw "Pass -ConfirmRestore after reviewing the restore target" }
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$backup = [System.IO.Path]::GetFullPath($BackupDirectory)
& (Join-Path $PSScriptRoot "docker-verify-backup.ps1") -BackupDirectory $backup

Push-Location $repoRoot
try {
    $config = ((& docker compose config --format json) -join "`n") | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0) { throw "Could not resolve Compose configuration" }
    $dataMount = $config.services."sync-server".volumes | Where-Object { $_.target -eq "/data" } | Select-Object -First 1
    $data = [System.IO.Path]::GetFullPath([string]$dataMount.source)
    $parent = Split-Path -Parent $data
    if (-not $parent -or $data -eq [System.IO.Path]::GetPathRoot($data)) { throw "Refusing a broad restore target: $data" }
    $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $rollback = Join-Path $parent "$(Split-Path -Leaf $data).restore-rollback-$stamp"
    $failed = Join-Path $parent "$(Split-Path -Leaf $data).restore-failed-$stamp"
	if ((Test-Path -LiteralPath $rollback) -or (Test-Path -LiteralPath $failed)) { throw "Restore safety directory already exists" }

    & docker compose stop --timeout 20 sync-server
    if ($LASTEXITCODE -ne 0) { throw "Could not stop sync-server" }
    $movedLiveData = $false
    try {
        Move-Item -LiteralPath $data -Destination $rollback
        $movedLiveData = $true
        New-Item -ItemType Directory -Path $data | Out-Null
        Copy-Item -LiteralPath (Join-Path $backup "sync.db") -Destination (Join-Path $data "sync.db")
        if (Test-Path -LiteralPath (Join-Path $backup "blobs") -PathType Container) {
            Copy-Item -LiteralPath (Join-Path $backup "blobs") -Destination (Join-Path $data "blobs") -Recurse
        }
        & docker compose up --detach --no-build sync-server
        if ($LASTEXITCODE -ne 0) { throw "Restored container did not start" }
        $healthy = $false
        for ($attempt = 0; $attempt -lt 60; $attempt += 1) {
            $container = (& docker compose ps --quiet sync-server).Trim()
            if ($container -and ((& docker inspect --format "{{.State.Health.Status}}" $container).Trim() -eq "healthy")) { $healthy = $true; break }
            Start-Sleep -Seconds 1
        }
        if (-not $healthy) { throw "Restored server did not become healthy" }
    } catch {
        & docker compose stop --timeout 20 sync-server *> $null
        if (Test-Path -LiteralPath $data) { Move-Item -LiteralPath $data -Destination $failed }
        if ($movedLiveData -and (Test-Path -LiteralPath $rollback)) { Move-Item -LiteralPath $rollback -Destination $data }
        & docker compose up --detach --no-build sync-server *> $null
        throw
    }
    Write-Host "Restore completed. Previous live data remains recoverable at: $rollback"
} finally {
    Pop-Location
}
