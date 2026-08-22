[CmdletBinding()]
param(
    [string]$Version,
    [switch]$SkipInstall
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$distPath = Join-Path $repoRoot "dist"
$pluginPath = Join-Path $repoRoot "plugin"
$adminWebPath = Join-Path $repoRoot "admin-web"
$rootManifestPath = Join-Path $repoRoot "manifest.json"
$pluginManifestPath = Join-Path $pluginPath "manifest.json"
$serverOutput = Join-Path $distPath "server\obsidian-sync-server.exe"
$adminWebOutput = Join-Path $distPath "server\admin-web"
$pluginOutput = Join-Path $distPath "plugin\sync-tunnel"
$releaseOutput = Join-Path $distPath "release"

if (-not (Test-Path -LiteralPath $rootManifestPath -PathType Leaf)) {
    throw "Missing root manifest.json"
}
if ((Get-FileHash -LiteralPath $rootManifestPath).Hash -ne (Get-FileHash -LiteralPath $pluginManifestPath).Hash) {
    throw "Root manifest.json and plugin\manifest.json must be identical"
}
$manifest = Get-Content -LiteralPath $rootManifestPath -Raw | ConvertFrom-Json
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = [string]$manifest.version
}
if ($Version -ne [string]$manifest.version) {
    throw "Build version '$Version' does not match manifest version '$($manifest.version)'"
}
$pluginZip = Join-Path $distPath "sync-tunnel-plugin-$Version.zip"

if (Test-Path -LiteralPath $distPath) {
    $resolvedDist = [System.IO.Path]::GetFullPath($distPath)
    if (-not $resolvedDist.StartsWith($repoRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) {
        throw "Refusing to clean a dist directory outside the repository: $resolvedDist"
    }
    Remove-Item -LiteralPath $resolvedDist -Recurse -Force
}
New-Item -ItemType Directory -Path (Split-Path $serverOutput), $pluginOutput, $releaseOutput -Force | Out-Null

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

Push-Location $adminWebPath
try {
	if (-not $SkipInstall) {
		& npm.cmd ci
		if ($LASTEXITCODE -ne 0) { throw "Admin Web npm ci failed" }
	}
	& npm.cmd run typecheck
	if ($LASTEXITCODE -ne 0) { throw "Admin Web typecheck failed" }
	& npm.cmd test
	if ($LASTEXITCODE -ne 0) { throw "Admin Web tests failed" }
	& npm.cmd run build
	if ($LASTEXITCODE -ne 0) { throw "Admin Web build failed" }
} finally {
	Pop-Location
}
Copy-Item -LiteralPath (Join-Path $adminWebPath "dist") -Destination $adminWebOutput -Recurse

Copy-Item -LiteralPath (Join-Path $pluginPath "main.js") -Destination $pluginOutput
Copy-Item -LiteralPath $rootManifestPath -Destination $pluginOutput
Copy-Item -LiteralPath (Join-Path $pluginPath "styles.css") -Destination $pluginOutput
Copy-Item -LiteralPath (Join-Path $pluginPath "main.js") -Destination $releaseOutput
Copy-Item -LiteralPath $rootManifestPath -Destination $releaseOutput
Copy-Item -LiteralPath (Join-Path $pluginPath "styles.css") -Destination $releaseOutput
Compress-Archive -Path (Join-Path $pluginOutput "*") -DestinationPath $pluginZip -CompressionLevel Optimal

Push-Location $repoRoot
try {
    & node .\scripts\verify-plugin-release.mjs $Version --artifacts
    if ($LASTEXITCODE -ne 0) { throw "Plugin release validation failed" }
} finally {
    Pop-Location
}

Write-Host "Build complete"
Write-Host "  Server: $serverOutput"
Write-Host "  Admin Web: $adminWebOutput"
Write-Host "  Plugin: $pluginOutput"
Write-Host "  GitHub Release assets: $releaseOutput"
Write-Host "  Plugin ZIP: $pluginZip"
