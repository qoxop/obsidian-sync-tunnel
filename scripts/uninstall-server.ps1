#Requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string]$ServiceName = "ObsidianSyncTunnel",
    [string]$InstallDirectory = "C:\ProgramData\ObsidianSyncTunnel"
)

$ErrorActionPreference = "Stop"
$service = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($service) {
    if ($service.Status -ne "Stopped") {
        Stop-Service -Name $ServiceName -Force
        (Get-Service -Name $ServiceName).WaitForStatus("Stopped", [TimeSpan]::FromSeconds(30))
    }
    & sc.exe delete $ServiceName | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Could not delete Windows service $ServiceName" }
    Write-Host "Removed Windows service: $ServiceName"
} else {
    Write-Host "Windows service is not installed: $ServiceName"
}

Write-Warning "Data was intentionally retained at $InstallDirectory"
Write-Warning "Delete that directory manually only after verifying a backup."
