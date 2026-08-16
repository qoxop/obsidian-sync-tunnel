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
    "X-Operation-ID" = [Guid]::NewGuid().ToString()
    "X-Base-Revision" = "0"
    "X-Modified-At" = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds().ToString()
    "X-Content-SHA256" = $sha
}
$encodedPath = [Uri]::EscapeDataString($path)
$base = "$ServerUrl/api/v1/vaults/$vaultId"

$health = Invoke-RestMethod -Uri "$ServerUrl/healthz"
if ($health.status -ne "ok") { throw "Health check failed" }
$serverInfo = Invoke-RestMethod -Uri "$ServerUrl/api/v2/server-info" -Headers @{ Authorization = "Bearer $Token" }
if ($serverInfo.protocol.max -lt 2 -or $serverInfo.capabilities -notcontains "snapshot-v1" -or
    $serverInfo.capabilities -notcontains "operation-id" -or $serverInfo.capabilities -notcontains "chunk-upload-v1" -or
    $serverInfo.capabilities -notcontains "rename-v1" -or $serverInfo.capabilities -notcontains "batch-delete-v1") {
    throw "Server does not advertise the required Protocol v2 capabilities"
}
$put = Invoke-RestMethod -Method Put -Uri "$base/file?path=$encodedPath" -Headers $headers -ContentType "application/octet-stream" -Body $data
if (-not $put.changed) { throw "Initial upload did not create a change" }
$page = Invoke-RestMethod -Uri "$base/changes?after=0&limit=10" -Headers @{ Authorization = "Bearer $Token" }
if ($page.changes.Count -ne 1) { throw "Expected one change, got $($page.changes.Count)" }
$snapshot = Invoke-RestMethod -Uri "$ServerUrl/api/v2/vaults/$vaultId/snapshot?limit=10" -Headers @{ Authorization = "Bearer $Token" }
if ($snapshot.snapshot_revision -ne $put.change.revision -or $snapshot.files.Count -ne 1 -or $snapshot.files[0].path -ne $path) {
    throw "Protocol v2 snapshot did not return the uploaded file"
}
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

$chunkPath = "folder/chunk-note.md"
$chunkMissingBody = @{ hashes = @($sha) } | ConvertTo-Json -Compress
$chunkApi = "$ServerUrl/api/v2/vaults/$vaultId/chunks"
$missingBefore = Invoke-RestMethod -Method Post -Uri "$chunkApi/missing" -Headers @{ Authorization = "Bearer $Token" } -ContentType "application/json" -Body $chunkMissingBody
if (@($missingBefore.missing).Count -ne 1) { throw "Chunk was not reported missing before upload" }
Invoke-RestMethod -Method Put -Uri "$chunkApi/$sha" -Headers @{ Authorization = "Bearer $Token" } -ContentType "application/octet-stream" -Body $data | Out-Null
$missingAfter = Invoke-RestMethod -Method Post -Uri "$chunkApi/missing" -Headers @{ Authorization = "Bearer $Token" } -ContentType "application/json" -Body $chunkMissingBody
if (@($missingAfter.missing).Count -ne 0) { throw "Uploaded Chunk is still reported missing" }
$chunkHeaders = @{
    Authorization = "Bearer $Token"
    "X-Device-ID" = "smoke-device"
    "X-Operation-ID" = [Guid]::NewGuid().ToString()
    "X-Base-Revision" = "0"
    "X-Modified-At" = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds().ToString()
    "X-Content-SHA256" = $sha
}
$manifestBody = @{ size = $data.Length; chunks = @(@{ hash = $sha; size = $data.Length }) } | ConvertTo-Json -Compress -Depth 4
$chunkCommit = Invoke-RestMethod -Method Post -Uri "$ServerUrl/api/v2/vaults/$vaultId/files/commit?path=$([Uri]::EscapeDataString($chunkPath))" -Headers $chunkHeaders -ContentType "application/json" -Body $manifestBody
if (-not $chunkCommit.changed) { throw "Chunk Manifest commit did not create a change" }
$manifest = Invoke-RestMethod -Uri "$ServerUrl/api/v2/vaults/$vaultId/manifests/$sha" -Headers @{ Authorization = "Bearer $Token" }
if ($manifest.size -ne $data.Length -or @($manifest.chunks).Count -ne 1) { throw "Stored Chunk Manifest is invalid" }
$renamedPath = "folder/chunk-note-renamed.md"
$renameHeaders = @{
    Authorization = "Bearer $Token"
    "X-Device-ID" = "smoke-device"
    "X-Operation-ID" = [Guid]::NewGuid().ToString()
    "X-Base-Revision" = $chunkCommit.change.revision.ToString()
    "X-Modified-At" = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds().ToString()
}
$renameBody = @{ from = $chunkPath; to = $renamedPath } | ConvertTo-Json -Compress
$rename = Invoke-RestMethod -Method Post -Uri "$ServerUrl/api/v2/vaults/$vaultId/rename" -Headers $renameHeaders -ContentType "application/json" -Body $renameBody
if (-not $rename.changed -or $rename.change.path -ne $renamedPath -or @($rename.related_changes).Count -ne 1 -or -not $rename.related_changes[0].deleted) {
    throw "Atomic rename did not return destination and source tombstone"
}

$deleteHeaders = @{
    Authorization = "Bearer $Token"
    "X-Device-ID" = "smoke-device"
    "X-Operation-ID" = [Guid]::NewGuid().ToString()
}
$deleteTime = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds()
$batchDeleteBody = @{ items = @(
    @{ path = $path; base_revision = $put.change.revision; modified_at = $deleteTime },
    @{ path = $renamedPath; base_revision = $rename.change.revision; modified_at = $deleteTime }
) } | ConvertTo-Json -Compress -Depth 4
$deleted = Invoke-RestMethod -Method Post -Uri "$ServerUrl/api/v2/vaults/$vaultId/batch/delete" -Headers $deleteHeaders -ContentType "application/json" -Body $batchDeleteBody
if (-not $deleted.changed -or @($deleted.changes).Count -ne 2 -or @($deleted.changes | Where-Object { -not $_.deleted }).Count -ne 0) {
    throw "Batch delete tombstones failed"
}
Write-Host "Smoke test passed for temporary vault $vaultId"
