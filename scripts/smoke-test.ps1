[CmdletBinding()]
param(
    [string]$ServerUrl = "http://127.0.0.1:8787",
    [string]$Token = "",
    [string]$TokenFile = ""
)

$ErrorActionPreference = "Stop"
if (-not $Token -and $TokenFile) {
    $Token = (Get-Content -LiteralPath $TokenFile -Raw).Trim()
}
if (-not $Token) { throw "Pass -Token or -TokenFile" }
$ServerUrl = $ServerUrl.TrimEnd("/")
$vaultId = "smoke-$([Guid]::NewGuid().ToString('N').Substring(0, 12))"
$path = "folder/smoke-note.md"
$data = [System.Text.UTF8Encoding]::new($false).GetBytes("sync smoke test")
$hasher = [Security.Cryptography.SHA256]::Create()
try {
    $sha = -join ($hasher.ComputeHash($data) | ForEach-Object { $_.ToString("x2") })
} finally {
    $hasher.Dispose()
}
$headers = @{
    Authorization = "Bearer $Token"
    "X-Device-ID" = "smoke-device"
    "X-Base-Revision" = "0"
    "X-Modified-At" = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds().ToString()
    "X-Content-SHA256" = $sha
}
$encodedPath = [Uri]::EscapeDataString($path)
$base = "$ServerUrl/api/v1/vaults/$vaultId"

$health = Invoke-RestMethod -Uri "$ServerUrl/healthz"
if ($health.status -ne "ok") { throw "Health check failed" }
$put = Invoke-RestMethod -Method Put -Uri "$base/file?path=$encodedPath" -Headers $headers -ContentType "application/octet-stream" -Body $data
if (-not $put.changed) { throw "Initial upload did not create a change" }
$page = Invoke-RestMethod -Uri "$base/changes?after=0&limit=10" -Headers @{ Authorization = "Bearer $Token" }
if ($page.changes.Count -ne 1) { throw "Expected one change, got $($page.changes.Count)" }
$download = Invoke-WebRequest -Uri "$base/blobs/$sha" -Headers @{ Authorization = "Bearer $Token" }
$downloaded = $download.Content
if ($downloaded -is [string]) { $downloaded = [System.Text.Encoding]::UTF8.GetBytes($downloaded) }
$hasher = [Security.Cryptography.SHA256]::Create()
try {
    $downloadSha = -join ($hasher.ComputeHash([byte[]]$downloaded) | ForEach-Object { $_.ToString("x2") })
} finally {
    $hasher.Dispose()
}
if ($downloadSha -ne $sha) { throw "Downloaded blob hash mismatch" }

$deleteHeaders = @{
    Authorization = "Bearer $Token"
    "X-Device-ID" = "smoke-device"
    "X-Base-Revision" = $put.change.revision.ToString()
    "X-Modified-At" = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds().ToString()
}
$deleted = Invoke-RestMethod -Method Delete -Uri "$base/file?path=$encodedPath" -Headers $deleteHeaders
if (-not $deleted.changed -or -not $deleted.change.deleted) { throw "Delete tombstone failed" }
Write-Host "Smoke test passed for temporary vault $vaultId"
