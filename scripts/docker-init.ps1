[CmdletBinding()]
param(
    [string]$DataDirectory = "",
    [string]$AdminTokenFile = "",
	[string]$BackupDirectory = "",
    [ValidateRange(1, 65535)]
    [int]$HostPort = 8787,
	[ValidateRange(1, 65535)]
	[int]$AdminHostPort = 8788,
    [ValidateRange(1024, [int64]::MaxValue)]
    [int64]$MaxFileBytes = 67108864,
    [string]$Version = "",
	[switch]$ForceConfig
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$envPath = Join-Path $repoRoot ".env"

if (-not $Version) {
    $manifestPath = Join-Path $repoRoot "manifest.json"
    if (-not (Test-Path -LiteralPath $manifestPath -PathType Leaf)) {
        throw "Missing manifest.json; cannot determine the source version"
    }
    $Version = [string](Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json).version
}
if ($Version -notmatch '^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$') {
    throw "Invalid source version: $Version"
}

if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    throw "Docker CLI was not found. Start Docker Desktop and try again."
}
& docker info *> $null
if ($LASTEXITCODE -ne 0) { throw "Docker Desktop engine is not available" }

if ((Test-Path -LiteralPath $envPath -PathType Leaf) -and -not $ForceConfig) {
    Write-Host "Existing .env retained: $envPath"
    Push-Location $repoRoot
    try {
        & docker compose config --quiet
        if ($LASTEXITCODE -ne 0) { throw "Existing Compose configuration is invalid; use -ForceConfig to replace .env" }
    } finally {
        Pop-Location
    }
    Write-Host "Docker configuration is already initialized."
    exit 0
}

if (-not $DataDirectory) { $DataDirectory = Join-Path $repoRoot "runtime-data" }
if (-not $AdminTokenFile) { $AdminTokenFile = Join-Path $repoRoot "secrets\admin-token.txt" }
if (-not $BackupDirectory) { $BackupDirectory = Join-Path $repoRoot "runtime-backups" }
$DataDirectory = [System.IO.Path]::GetFullPath($DataDirectory)
$AdminTokenFile = [System.IO.Path]::GetFullPath($AdminTokenFile)
$BackupDirectory = [System.IO.Path]::GetFullPath($BackupDirectory)
$tokenDirectory = Split-Path -Parent $AdminTokenFile
New-Item -ItemType Directory -Path $DataDirectory, $BackupDirectory, $tokenDirectory -Force | Out-Null

$generatedToken = $false
if (-not (Test-Path -LiteralPath $AdminTokenFile -PathType Leaf)) {
    $bytes = [byte[]]::new(32)
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    } finally {
        $generator.Dispose()
    }
    $token = [Convert]::ToBase64String($bytes).TrimEnd("=").Replace("+", "-").Replace("/", "_")
    [System.IO.File]::WriteAllText($AdminTokenFile, $token, [System.Text.UTF8Encoding]::new($false))
    $generatedToken = $true
}

$dataForCompose = $DataDirectory.Replace("\", "/")
$tokenForCompose = $AdminTokenFile.Replace("\", "/")
$backupForCompose = $BackupDirectory.Replace("\", "/")
$lines = @(
    "OBSIDIAN_SYNC_VERSION=$Version",
    "OBSIDIAN_SYNC_PORT=$HostPort",
	"OBSIDIAN_SYNC_ADMIN_PORT=$AdminHostPort",
    "OBSIDIAN_SYNC_MAX_FILE_BYTES=$MaxFileBytes",
    "OBSIDIAN_SYNC_DATA_DIR=`"$dataForCompose`"",
	"OBSIDIAN_SYNC_BACKUP_DIR=`"$backupForCompose`"",
    "OBSIDIAN_SYNC_ADMIN_TOKEN_FILE=`"$tokenForCompose`""
)
[System.IO.File]::WriteAllText($envPath, (($lines -join "`n") + "`n"), [System.Text.UTF8Encoding]::new($false))

Push-Location $repoRoot
try {
    & docker compose config --quiet
    if ($LASTEXITCODE -ne 0) { throw "Generated Compose configuration is invalid" }
} finally {
    Pop-Location
}

Write-Host "Docker deployment initialized"
Write-Host "  Environment: $envPath"
Write-Host "  Persistent data: $DataDirectory"
Write-Host "  Admin token file: $AdminTokenFile"
Write-Host "  Local endpoint: http://127.0.0.1:$HostPort"
Write-Host "  Local admin endpoint: http://127.0.0.1:$AdminHostPort"
if ($generatedToken) {
	Write-Warning "A new local Admin Token was generated and stored in the protected token file. It is not printed and is never entered into Obsidian."
} else {
    Write-Host "Existing admin token retained."
}
