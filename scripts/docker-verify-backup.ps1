[CmdletBinding()]
param([Parameter(Mandatory)] [string]$BackupDirectory)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$backup = [System.IO.Path]::GetFullPath($BackupDirectory)
Push-Location $repoRoot
try {
    $config = ((& docker compose config --format json) -join "`n") | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0) { throw "Could not resolve Compose configuration" }
    $mount = $config.services."sync-server".volumes | Where-Object { $_.target -eq "/backups" } | Select-Object -First 1
    $root = [System.IO.Path]::GetFullPath([string]$mount.source)
    $relative = [System.IO.Path]::GetRelativePath($root, $backup)
    if ($relative -eq ".." -or $relative.StartsWith("..$([System.IO.Path]::DirectorySeparatorChar)")) { throw "Backup must be inside the configured /backups bind mount" }
    $containerPath = "/backups/" + $relative.Replace("\", "/")
    & docker compose exec --no-TTY sync-server /app/obsidian-sync-server verify-backup --directory $containerPath | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Backup verification failed" }
    Write-Host "Backup verification PASS: $backup"
} finally {
    Pop-Location
}
