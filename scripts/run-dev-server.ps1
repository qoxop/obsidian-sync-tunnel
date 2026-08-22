[CmdletBinding()]
param(
	[string]$Listen = "127.0.0.1:8787",
	[ValidateSet("none", "token")]
	[string]$AdminAuth = "none"
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$tokenPath = Join-Path $repoRoot "dev-admin-token.txt"
$databasePath = Join-Path $repoRoot "data\dev-sync.db"
$adminWebPath = Join-Path $repoRoot "admin-web"
$backupPath = Join-Path $repoRoot "runtime-backups"
if ($AdminAuth -eq "token" -and -not (Test-Path -LiteralPath $tokenPath -PathType Leaf)) {
    Push-Location $repoRoot
    try {
        $token = (& go run .\cmd\obsidian-sync-server token).Trim()
    } finally {
        Pop-Location
    }
    [System.IO.File]::WriteAllText($tokenPath, $token, [System.Text.UTF8Encoding]::new($false))
	Write-Warning "Development Admin Token created in ignored dev-admin-token.txt; it is not printed and is never entered into Obsidian."
}
Push-Location $adminWebPath
try {
	& npm.cmd run build
	if ($LASTEXITCODE -ne 0) { throw "Admin Web build failed" }
} finally {
	Pop-Location
}
Push-Location $repoRoot
try {
	& go run .\cmd\obsidian-sync-server serve --listen $Listen --admin-listen 127.0.0.1:8788 --admin-auth $AdminAuth --database $databasePath --admin-token-file $tokenPath --admin-ui-directory (Join-Path $adminWebPath "dist") --backup-directory $backupPath
} finally {
    Pop-Location
}
