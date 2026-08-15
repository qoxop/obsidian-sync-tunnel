#Requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string]$TunnelToken = "",
    [string]$CloudflaredPath = ""
)

$ErrorActionPreference = "Stop"
if (-not $CloudflaredPath) {
    $command = Get-Command cloudflared.exe -ErrorAction SilentlyContinue
    if ($command) {
        $CloudflaredPath = $command.Source
    } else {
        $candidates = @(
            "C:\Program Files\cloudflared\cloudflared.exe",
            "C:\Cloudflared\bin\cloudflared.exe"
        )
        $CloudflaredPath = $candidates | Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } | Select-Object -First 1
    }
}
if (-not $CloudflaredPath -or -not (Test-Path -LiteralPath $CloudflaredPath -PathType Leaf)) {
    throw "cloudflared.exe was not found. Install it from the official Cloudflare download page first."
}

if (-not $TunnelToken) {
    $secureToken = Read-Host "Paste the remotely-managed Tunnel token" -AsSecureString
    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secureToken)
    try {
        $TunnelToken = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
    } finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
    }
}
if ($TunnelToken.Length -lt 32) { throw "Tunnel token is missing or too short" }

& $CloudflaredPath service install $TunnelToken
if ($LASTEXITCODE -ne 0) { throw "cloudflared service install failed" }
$TunnelToken = $null
Get-Service -Name cloudflared | Format-Table Name, Status, StartType
Write-Host "cloudflared is installed. Configure its public hostname to http://127.0.0.1:8787 in the Cloudflare dashboard."
