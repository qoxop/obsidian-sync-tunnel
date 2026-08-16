[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$VaultPath
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$source = Join-Path $repoRoot "dist\plugin\sync-tunnel"
$vault = [System.IO.Path]::GetFullPath($VaultPath)
$obsidianDirectory = Join-Path $vault ".obsidian"
$destination = Join-Path $obsidianDirectory "plugins\sync-tunnel"
$legacyDestination = Join-Path $obsidianDirectory "plugins\obsidian-sync-tunnel"

if (-not (Test-Path -LiteralPath $obsidianDirectory -PathType Container)) {
    throw "Not an Obsidian vault (missing .obsidian): $vault"
}
if (Test-Path -LiteralPath $legacyDestination -PathType Container) {
    Write-Warning "Legacy plugin directory found: $legacyDestination"
    Write-Warning "After preserving any required settings, disable and remove that directory to avoid loading two plugin copies."
}
foreach ($file in @("main.js", "manifest.json", "styles.css")) {
    if (-not (Test-Path -LiteralPath (Join-Path $source $file) -PathType Leaf)) {
        throw "Missing build artifact $file. Run scripts\build.ps1 first."
    }
}

New-Item -ItemType Directory -Path $destination -Force | Out-Null
foreach ($file in @("main.js", "manifest.json", "styles.css")) {
    Copy-Item -LiteralPath (Join-Path $source $file) -Destination (Join-Path $destination $file) -Force
}
Write-Host "Installed plugin files to $destination"
Write-Host "Reload Obsidian, enable Sync Tunnel, and configure it in a test vault."
