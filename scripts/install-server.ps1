#Requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string]$InstallDirectory = "C:\ProgramData\ObsidianSyncTunnel",
    [string]$ServiceName = "ObsidianSyncTunnel",
    [string]$SourceBinary = "",
    [int64]$MaxUploadBytes = 67108864
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
if (-not $SourceBinary) {
    $SourceBinary = Join-Path $repoRoot "dist\server\obsidian-sync-server.exe"
}
$SourceBinary = [System.IO.Path]::GetFullPath($SourceBinary)
if (-not (Test-Path -LiteralPath $SourceBinary -PathType Leaf)) {
    throw "Server binary not found: $SourceBinary. Run scripts\build.ps1 first."
}

$InstallDirectory = [System.IO.Path]::GetFullPath($InstallDirectory)
$binDirectory = Join-Path $InstallDirectory "bin"
$dataDirectory = Join-Path $InstallDirectory "data"
$logDirectory = Join-Path $InstallDirectory "logs"
$targetBinary = Join-Path $binDirectory "obsidian-sync-server.exe"
$configPath = Join-Path $InstallDirectory "config.json"
$tokenPath = Join-Path $InstallDirectory "token.txt"

New-Item -ItemType Directory -Path $InstallDirectory, $binDirectory, $dataDirectory, $logDirectory -Force | Out-Null
& icacls.exe $InstallDirectory /inheritance:r /grant:r "SYSTEM:(OI)(CI)F" "BUILTIN\Administrators:(OI)(CI)F" | Out-Null
if ($LASTEXITCODE -ne 0) { throw "Could not restrict the install directory ACL" }

$existingService = Get-Service -Name $ServiceName -ErrorAction SilentlyContinue
if ($existingService -and $existingService.Status -ne "Stopped") {
    Stop-Service -Name $ServiceName -Force
    (Get-Service -Name $ServiceName).WaitForStatus("Stopped", [TimeSpan]::FromSeconds(30))
}

Copy-Item -LiteralPath $SourceBinary -Destination $targetBinary -Force

$generatedToken = $false
if (-not (Test-Path -LiteralPath $tokenPath -PathType Leaf)) {
    $token = (& $targetBinary token).Trim()
    if ($LASTEXITCODE -ne 0 -or $token.Length -lt 32) { throw "Token generation failed" }
    [System.IO.File]::WriteAllText($tokenPath, $token, [System.Text.UTF8Encoding]::new($false))
    $generatedToken = $true
}

$config = [ordered]@{
    listen = "127.0.0.1:8787"
    database_path = "data/sync.db"
    token_file = "token.txt"
    log_path = "logs/server.jsonl"
    max_upload_bytes = $MaxUploadBytes
}
[System.IO.File]::WriteAllText($configPath, ($config | ConvertTo-Json), [System.Text.UTF8Encoding]::new($false))

$serviceCommand = ('"{0}" serve --config "{1}" --windows-service' -f $targetBinary, $configPath)
if ($existingService) {
    & sc.exe config $ServiceName binPath= $serviceCommand start= auto | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Could not update Windows service" }
} else {
    & sc.exe create $ServiceName binPath= $serviceCommand start= auto DisplayName= "Obsidian Sync Tunnel" | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Could not create Windows service" }
}
& sc.exe description $ServiceName "Self-hosted Obsidian vault synchronization server" | Out-Null
& sc.exe failure $ServiceName reset= 86400 actions= restart/5000/restart/30000/""/0 | Out-Null

Start-Service -Name $ServiceName
(Get-Service -Name $ServiceName).WaitForStatus("Running", [TimeSpan]::FromSeconds(30))
$health = $null
for ($attempt = 0; $attempt -lt 10; $attempt += 1) {
    try {
        $health = Invoke-RestMethod -Uri "http://127.0.0.1:8787/healthz" -TimeoutSec 2
        if ($health.status -eq "ok") { break }
    } catch {
        Start-Sleep -Milliseconds 500
    }
}
if (-not $health -or $health.status -ne "ok") { throw "Service started but health check failed; inspect $logDirectory\server.jsonl" }

Write-Host "Server service is running: $ServiceName"
Write-Host "Install directory: $InstallDirectory"
Write-Host "Log file: $logDirectory\server.jsonl"
if ($generatedToken) {
    Write-Warning "This is the only automatic display of the newly generated API token. Put it in a password manager and Obsidian SecretStorage now:"
    Write-Host $token
} else {
    Write-Host "The existing API token was retained."
}
