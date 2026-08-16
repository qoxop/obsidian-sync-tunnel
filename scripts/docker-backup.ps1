[CmdletBinding()]
param(
    [string]$DestinationDirectory = ""
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
if (-not $DestinationDirectory) {
    $documents = [Environment]::GetFolderPath([Environment+SpecialFolder]::MyDocuments)
    $DestinationDirectory = Join-Path $documents "ObsidianSyncBackups"
}
$DestinationDirectory = [System.IO.Path]::GetFullPath($DestinationDirectory)

Push-Location $repoRoot
try {
    $composeJsonText = (& docker compose config --format json) -join "`n"
    if ($LASTEXITCODE -ne 0) { throw "Could not resolve Compose configuration" }
    $composeConfig = $composeJsonText | ConvertFrom-Json
    $dataMount = $composeConfig.services."sync-server".volumes | Where-Object { $_.target -eq "/data" } | Select-Object -First 1
    if (-not $dataMount -or -not $dataMount.source) { throw "Compose /data bind mount was not found" }
    $dataDirectory = [System.IO.Path]::GetFullPath([string]$dataMount.source)
    $databasePath = Join-Path $dataDirectory "sync.db"
    if (-not (Test-Path -LiteralPath $databasePath -PathType Leaf)) { throw "Database not found: $databasePath" }

    $containerId = (& docker compose ps --quiet sync-server).Trim()
    $wasRunning = $false
    if ($containerId) {
        $wasRunning = ((& docker inspect --format "{{.State.Running}}" $containerId).Trim() -eq "true")
    }
    try {
        if ($wasRunning) {
            & docker compose stop --timeout 20 sync-server
            if ($LASTEXITCODE -ne 0) { throw "Could not stop sync-server for backup" }
        }

        $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
        $backupPath = Join-Path $DestinationDirectory $stamp
        if (Test-Path -LiteralPath $backupPath) { throw "Backup destination already exists: $backupPath" }
        New-Item -ItemType Directory -Path $backupPath -Force | Out-Null
        $backupDatabase = Join-Path $backupPath "sync.db"
        Copy-Item -LiteralPath $databasePath -Destination $backupDatabase
        $hash = (Get-FileHash -Algorithm SHA256 -LiteralPath $backupDatabase).Hash.ToLowerInvariant()
        $metadata = [ordered]@{
            created_at = (Get-Date).ToUniversalTime().ToString("o")
            source = $databasePath
            sha256 = $hash
            includes_token = $false
            deployment = "docker-compose-bind-mount"
        }
        [System.IO.File]::WriteAllText((Join-Path $backupPath "backup.json"), ($metadata | ConvertTo-Json), [System.Text.UTF8Encoding]::new($false))
    } finally {
        if ($wasRunning) {
            & docker compose up --detach --no-build sync-server
            if ($LASTEXITCODE -ne 0) { Write-Warning "Backup succeeded, but sync-server could not be restarted automatically" }
        }
    }
} finally {
    Pop-Location
}

Write-Host "Consistent SQLite backup created: $backupPath"
Write-Warning "The backup contains plaintext vault content. Copy it to encrypted storage on another device."
