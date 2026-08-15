[CmdletBinding()]
param(
    [string]$Version = "0.1.0",
    [switch]$SkipInstall
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$distPath = Join-Path $repoRoot "dist"
$pluginPath = Join-Path $repoRoot "plugin"
$serverOutput = Join-Path $distPath "server\obsidian-sync-server.exe"
$pluginOutput = Join-Path $distPath "plugin\obsidian-sync-tunnel"
$pluginZip = Join-Path $distPath "obsidian-sync-tunnel-plugin-$Version.zip"

if (Test-Path -LiteralPath $distPath) {
    $resolvedDist = [System.IO.Path]::GetFullPath($distPath)
    if (-not $resolvedDist.StartsWith($repoRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to clean a dist directory outside the repository: $resolvedDist"
    }
    Remove-Item -LiteralPath $resolvedDist -Recurse -Force
}
New-Item -ItemType Directory -Path (Split-Path $serverOutput), $pluginOutput -Force | Out-Null

Push-Location $repoRoot
try {
    & go test ./...
    if ($LASTEXITCODE -ne 0) { throw "Go tests failed" }
    & go vet ./...
    if ($LASTEXITCODE -ne 0) { throw "go vet failed" }
    & go build -trimpath -ldflags "-s -w -X main.version=$Version" -o $serverOutput ./cmd/obsidian-sync-server
    if ($LASTEXITCODE -ne 0) { throw "Go build failed" }
} finally {
    Pop-Location
}

Push-Location $pluginPath
try {
    if (-not $SkipInstall) {
        & npm.cmd ci
        if ($LASTEXITCODE -ne 0) { throw "npm ci failed" }
    }
    & npm.cmd run typecheck
    if ($LASTEXITCODE -ne 0) { throw "Plugin typecheck failed" }
    & npm.cmd run build
    if ($LASTEXITCODE -ne 0) { throw "Plugin build failed" }
} finally {
    Pop-Location
}

Copy-Item -LiteralPath (Join-Path $pluginPath "main.js") -Destination $pluginOutput
Copy-Item -LiteralPath (Join-Path $pluginPath "manifest.json") -Destination $pluginOutput
Copy-Item -LiteralPath (Join-Path $pluginPath "styles.css") -Destination $pluginOutput
Compress-Archive -Path (Join-Path $pluginOutput "*") -DestinationPath $pluginZip -CompressionLevel Optimal

Write-Host "Build complete"
Write-Host "  Server: $serverOutput"
Write-Host "  Plugin: $pluginOutput"
Write-Host "  Plugin ZIP: $pluginZip"
