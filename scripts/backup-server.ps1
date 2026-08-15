#Requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string]$InstallDirectory = "C:\ProgramData\ObsidianSyncTunnel",
    [string]$ServiceName = "ObsidianSyncTunnel",
    [string]$DestinationDirectory = ""
)

$ErrorActionPreference = "Stop"
if (-not $DestinationDirectory) {
    $documents = [Environment]::GetFolderPath([Environment+SpecialFolder]::MyDocuments)
    $DestinationDirectory = Join-Path $documents "ObsidianSyncBackups"
}
$InstallDirectory = [System.IO.Path]::GetFullPath($InstallDirectory)
$DestinationDirectory = [System.IO.Path]::GetFullPath($DestinationDirectory)
$databasePath = Join-Path $InstallDirectory "data\sync.db"
$configPath = Join-Path $InstallDirectory "config.json"
if (-not (Test-Path -LiteralPath $databasePath -PathType Leaf)) { throw "Database not found: $databasePath" }

$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$backupPath = Join-Path $DestinationDirectory $stamp
New-Item -ItemType Directory -Path $backupPath -Force | Out-Null
$service = Get-Service -Name $ServiceName -ErrorAction Stop
$wasRunning = $service.Status -eq "Running"
try {
    if ($wasRunning) {
        Stop-Service -Name $ServiceName
        (Get-Service -Name $ServiceName).WaitForStatus("Stopped", [TimeSpan]::FromSeconds(30))
    }
    Copy-Item -LiteralPath $databasePath -Destination (Join-Path $backupPath "sync.db")
    if (Test-Path -LiteralPath $configPath -PathType Leaf) {
        Copy-Item -LiteralPath $configPath -Destination (Join-Path $backupPath "config.json")
    }
} finally {
    if ($wasRunning) { Start-Service -Name $ServiceName }
}

$hash = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $backupPath "sync.db")).Hash.ToLowerInvariant()
$metadata = [ordered]@{
    created_at = (Get-Date).ToUniversalTime().ToString("o")
    source = $databasePath
    sha256 = $hash
    includes_token = $false
}
[System.IO.File]::WriteAllText((Join-Path $backupPath "backup.json"), ($metadata | ConvertTo-Json), [System.Text.UTF8Encoding]::new($false))
Write-Host "Consistent backup created: $backupPath"
Write-Warning "The database contains plaintext vault content. Copy the backup to encrypted storage on another device."
