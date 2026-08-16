[CmdletBinding()]
param(
    [string]$DataDirectory = "",
    [string]$TokenFile = "",
    [ValidateRange(1, 65535)]
    [int]$HostPort = 8787,
    [ValidateRange(1024, [int64]::MaxValue)]
    [int64]$MaxUploadBytes = 67108864,
    [string]$Version = "0.1.0",
    [switch]$ForceConfig,
    [switch]$HideGeneratedToken
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$envPath = Join-Path $repoRoot ".env"

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
if (-not $TokenFile) { $TokenFile = Join-Path $repoRoot "secrets\api-token.txt" }
$DataDirectory = [System.IO.Path]::GetFullPath($DataDirectory)
$TokenFile = [System.IO.Path]::GetFullPath($TokenFile)
$tokenDirectory = Split-Path -Parent $TokenFile
New-Item -ItemType Directory -Path $DataDirectory, $tokenDirectory -Force | Out-Null

$generatedToken = $false
if (-not (Test-Path -LiteralPath $TokenFile -PathType Leaf)) {
    $bytes = [byte[]]::new(32)
    $generator = [Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($bytes)
    } finally {
        $generator.Dispose()
    }
    $token = [Convert]::ToBase64String($bytes).TrimEnd("=").Replace("+", "-").Replace("/", "_")
    [System.IO.File]::WriteAllText($TokenFile, $token, [System.Text.UTF8Encoding]::new($false))
    $generatedToken = $true
}

$dataForCompose = $DataDirectory.Replace("\", "/")
$tokenForCompose = $TokenFile.Replace("\", "/")
$lines = @(
    "OBSIDIAN_SYNC_VERSION=$Version",
    "OBSIDIAN_SYNC_PORT=$HostPort",
    "OBSIDIAN_SYNC_MAX_UPLOAD_BYTES=$MaxUploadBytes",
    "OBSIDIAN_SYNC_DATA_DIR=`"$dataForCompose`"",
    "OBSIDIAN_SYNC_TOKEN_FILE=`"$tokenForCompose`""
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
Write-Host "  API token file: $TokenFile"
Write-Host "  Local endpoint: http://127.0.0.1:$HostPort"
if ($generatedToken) {
    if ($HideGeneratedToken) {
        Write-Warning "A new API token was generated but hidden from this output. Read it locally from the API token file when configuring Obsidian."
    } else {
        Write-Warning "Save this API token in a password manager and Obsidian SecretStorage. It will not be displayed automatically again:"
        Write-Host $token
    }
} else {
    Write-Host "Existing API token retained."
}
