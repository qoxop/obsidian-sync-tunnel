[CmdletBinding()]
param(
    [string]$Origin = "http://127.0.0.1:8787"
)

$ErrorActionPreference = "Stop"
$cloudflared = Get-Command cloudflared.exe -ErrorAction SilentlyContinue
if (-not $cloudflared) { throw "cloudflared.exe is not installed or not on PATH" }
Write-Warning "Quick Tunnels are temporary development endpoints. Do not treat the generated URL as a stable production hostname."
& $cloudflared.Source tunnel --url $Origin
