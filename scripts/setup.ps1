[CmdletBinding()]
param(
    [string]$DataDirectory = "",
    [string]$BackupDirectory = "",
    [string]$SecretsDirectory = "",
    [ValidateSet("none", "token")]
    [string]$AdminAuth = "none",
    [ValidateRange(1, 65535)]
    [int]$SyncPort = 8787,
    [ValidateRange(1, 65535)]
    [int]$AdminPort = 8788,
    [switch]$ResetConfiguration,
    [switch]$NoBuild,
    [switch]$NoOpen
)

$ErrorActionPreference = "Stop"
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$envPath = Join-Path $repoRoot ".env"
$manifestPath = Join-Path $repoRoot "manifest.json"

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "Docker Desktop was not found. Install and start Docker Desktop first."
}
& docker info *> $null
if ($LASTEXITCODE -ne 0) { throw "Docker Desktop is not running." }

if (-not (Test-Path -LiteralPath $envPath -PathType Leaf) -or $ResetConfiguration) {
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) { throw "Missing manifest.json" }
    $version = [string](Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json).version
    if (-not $DataDirectory) { $DataDirectory = Join-Path $repoRoot "runtime-data" }
    if (-not $BackupDirectory) { $BackupDirectory = Join-Path $repoRoot "runtime-backups" }
    if (-not $SecretsDirectory) { $SecretsDirectory = Join-Path $repoRoot "secrets" }
    $DataDirectory = [IO.Path]::GetFullPath($DataDirectory)
    $BackupDirectory = [IO.Path]::GetFullPath($BackupDirectory)
    $SecretsDirectory = [IO.Path]::GetFullPath($SecretsDirectory)
    New-Item -ItemType Directory -Path $DataDirectory, $BackupDirectory, $SecretsDirectory -Force | Out-Null

    if ($AdminAuth -eq "token") {
        $tokenPath = Join-Path $SecretsDirectory "admin-token.txt"
        if (-not (Test-Path -LiteralPath $tokenPath -PathType Leaf)) {
            $token = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(32)).TrimEnd("=").Replace("+", "-").Replace("/", "_")
            [IO.File]::WriteAllText($tokenPath, $token, [Text.UTF8Encoding]::new($false))
            $token = ""
        }
    }

    $lines = @(
        "OBSIDIAN_SYNC_VERSION=$version",
        "OBSIDIAN_SYNC_PORT=$SyncPort",
        "OBSIDIAN_SYNC_ADMIN_PORT=$AdminPort",
        "OBSIDIAN_SYNC_ADMIN_AUTH=$AdminAuth",
        "OBSIDIAN_SYNC_MAX_FILE_BYTES=67108864",
        "OBSIDIAN_SYNC_DATA_DIR=`"$($DataDirectory.Replace('\', '/'))`"",
        "OBSIDIAN_SYNC_BACKUP_DIR=`"$($BackupDirectory.Replace('\', '/'))`"",
        "OBSIDIAN_SYNC_SECRETS_DIR=`"$($SecretsDirectory.Replace('\', '/'))`""
    )
    [IO.File]::WriteAllText($envPath, (($lines -join "`n") + "`n"), [Text.UTF8Encoding]::new($false))
    Write-Host "Created local configuration and persistent directories."
} else {
    Write-Host "Using existing local configuration: $envPath"
}

Push-Location $repoRoot
try {
    & docker compose config --quiet
    if ($LASTEXITCODE -ne 0) { throw "Docker configuration is invalid." }
    $arguments = @("compose", "up", "--detach")
    if (-not $NoBuild) { $arguments += "--build" }
    $arguments += "sync-server"
    & docker @arguments
	$composeExitCode = $LASTEXITCODE

    $containerID = (& docker compose ps --quiet sync-server).Trim()
	if (-not $containerID) {
		if ($composeExitCode -ne 0) { throw "Could not start Sync Tunnel." }
		throw "Sync Tunnel container was not created."
	}
    $health = ""
    for ($attempt = 0; $attempt -lt 60; $attempt++) {
        $health = (& docker inspect --format "{{.State.Health.Status}}" $containerID).Trim()
        if ($health -eq "healthy") { break }
        if ($health -eq "unhealthy") { throw "Sync Tunnel failed its health check. Open Docker Desktop to view the logs." }
        Start-Sleep -Seconds 1
    }
    if ($health -ne "healthy") { throw "Sync Tunnel did not become ready within 60 seconds." }

    $compose = ((& docker compose config --format json) -join "`n") | ConvertFrom-Json
	$desiredImage = [string]$compose.services."sync-server".image
	$desiredImageID = (& docker image inspect $desiredImage --format "{{.Id}}").Trim()
	$runningImageID = (& docker inspect $containerID --format "{{.Image}}").Trim()
	if (-not $desiredImageID -or $runningImageID -ne $desiredImageID) { throw "The running container does not use the newly built image." }
	if ($composeExitCode -ne 0) { Write-Warning "Docker reported a transient replacement error, but the requested image is running and healthy." }
    $adminBinding = $compose.services."sync-server".ports | Where-Object { $_.target -eq 8788 } | Select-Object -First 1
    $publishedAdminPort = if ($adminBinding.published) { [int]$adminBinding.published } else { 8788 }
    $adminURL = "http://127.0.0.1:$publishedAdminPort/admin/"
    Write-Host "Sync Tunnel is ready: $adminURL"
    if (-not $NoOpen) { Start-Process $adminURL }
} finally {
    Pop-Location
}
