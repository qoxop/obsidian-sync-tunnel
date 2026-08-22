#Requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string]$InstallDirectory = "C:\ProgramData\ObsidianSyncTunnel",
    [string]$DestinationDirectory = ""
)

$ErrorActionPreference = "Stop"
if (-not $DestinationDirectory) {
    $documents = [Environment]::GetFolderPath([Environment+SpecialFolder]::MyDocuments)
    $DestinationDirectory = Join-Path $documents "ObsidianSyncBackups"
}
$InstallDirectory = [System.IO.Path]::GetFullPath($InstallDirectory)
$DestinationDirectory = [System.IO.Path]::GetFullPath($DestinationDirectory)
$tokenPath = Join-Path $InstallDirectory "admin-token.txt"
$token = [System.IO.File]::ReadAllText($tokenPath).Trim()
$stamp = Get-Date -Format "yyyyMMdd-HHmmss"
$backupPath = Join-Path $DestinationDirectory $stamp
$headers = @{ Authorization = "Bearer $token" }
try {
    $body = @{ destination = $backupPath } | ConvertTo-Json -Compress
    $null = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:8788/admin/v1/backups" -Headers $headers -ContentType "application/json" -Body $body
} finally {
    $token = $null
    $headers = $null
}
$binary = Join-Path $InstallDirectory "bin\obsidian-sync-server.exe"
& $binary verify-backup --directory $backupPath
if ($LASTEXITCODE -ne 0) { throw "Backup verification failed" }
Write-Host "Verified online backup created: $backupPath"
Write-Warning "The backup contains plaintext Vault content. Replicate it to encrypted storage on another device."
