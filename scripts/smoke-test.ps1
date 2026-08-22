[CmdletBinding()]
param(
    [string]$PublicUrl = "http://127.0.0.1:8787",
    [string]$AdminUrl = "http://127.0.0.1:8788",
    [string]$AdminToken = "",
    [string]$AdminTokenFile = (Join-Path $PSScriptRoot "../runtime-data/secrets/admin-token.txt")
)

$ErrorActionPreference = "Stop"
$PublicUrl = $PublicUrl.TrimEnd("/")
$AdminUrl = $AdminUrl.TrimEnd("/")
$adminSession = Invoke-RestMethod -Uri "$AdminUrl/admin/v1/session"
$adminHeaders = @{}
if ($adminSession.authentication -eq "token") {
    if (-not $AdminToken) {
        if (-not (Test-Path -LiteralPath $AdminTokenFile -PathType Leaf)) {
            throw "Admin token file not found. Pass -AdminTokenFile or -AdminToken."
        }
        $AdminToken = (Get-Content -LiteralPath $AdminTokenFile -Raw).Trim()
    }
    if ($AdminToken.Length -lt 32) { throw "Admin token is unexpectedly short" }
    $adminHeaders.Authorization = "Bearer $AdminToken"
} elseif ($adminSession.authentication -ne "none") {
    throw "Unsupported admin authentication mode: $($adminSession.authentication)"
}

function Get-Sha256([byte[]]$Bytes) {
    $hasher = [Security.Cryptography.SHA256]::Create()
    try { return -join ($hasher.ComputeHash($Bytes) | ForEach-Object { $_.ToString("x2") }) }
    finally { $hasher.Dispose() }
}

function New-MutationHeaders([string]$Token, [long]$BaseRevision, [string]$Hash = "") {
    $headers = @{
        Authorization = "Bearer $Token"
        "X-Operation-ID" = [Guid]::NewGuid().ToString()
        "X-Base-Revision" = $BaseRevision.ToString()
        "X-Modified-At" = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds().ToString()
    }
    if ($Hash) { $headers["X-Content-SHA256"] = $Hash }
    return $headers
}

function Assert-Unauthorized([scriptblock]$Request) {
    try {
        & $Request | Out-Null
        throw "Expected HTTP 401"
    } catch {
        $status = $_.Exception.Response.StatusCode.value__
        if ($status -ne 401) { throw }
    }
}

$vaultId = "smoke-$([Guid]::NewGuid().ToString('N').Substring(0, 12))"
$vaultBase = "$PublicUrl/api/v1/vaults/$vaultId"
$deviceToken = ""
$rotatedToken = ""
$downloadTarget = [IO.Path]::GetTempFileName()

try {
    $health = Invoke-RestMethod -Uri "$PublicUrl/healthz"
    if ($health.status -ne "ok") { throw "Public health check failed" }
    $adminHealth = Invoke-RestMethod -Uri "$AdminUrl/healthz"
    if ($adminHealth.status -ne "ok") { throw "Admin health check failed" }

    $vaultBody = @{ id = $vaultId; display_name = "Automated smoke test"; quota_bytes = 0; max_files = 0 } | ConvertTo-Json -Compress
    Invoke-RestMethod -Method Post -Uri "$AdminUrl/admin/v1/vaults" -Headers $adminHeaders -ContentType "application/json" -Body $vaultBody | Out-Null
    $pairingBody = @{ ttl_seconds = 300; scopes = "sync:read,sync:write,history:read,restore:write" } | ConvertTo-Json -Compress
    $pairing = Invoke-RestMethod -Method Post -Uri "$AdminUrl/admin/v1/vaults/$vaultId/pairing-codes" -Headers $adminHeaders -ContentType "application/json" -Body $pairingBody

    $pairBody = @{ vault_id = $vaultId; code = $pairing.code; device_name = "PowerShell smoke test"; platform = "windows"; client_version = "1.0.0" } | ConvertTo-Json -Compress
    $paired = Invoke-RestMethod -Method Post -Uri "$PublicUrl/api/v1/pair" -ContentType "application/json" -Body $pairBody
    $deviceToken = $paired.token
    if (-not $deviceToken -or -not $paired.device.id) { throw "Pairing did not return a credential and server-assigned device ID" }
    $auth = @{ Authorization = "Bearer $deviceToken" }

    $serverInfo = Invoke-RestMethod -Uri "$PublicUrl/api/v1/server-info" -Headers $auth
    $required = @("snapshot", "idempotent-operations", "chunk-transfer", "rename", "batch-delete", "device-ack", "history", "restore", "scoped-credentials")
    if ($serverInfo.protocol.version -ne 1 -or @($required | Where-Object { $serverInfo.capabilities -notcontains $_ }).Count -ne 0) {
        throw "Server does not expose the final Sync Tunnel 1.0 protocol"
    }

    $path = "folder/smoke-note.md"
	$data = [Text.UTF8Encoding]::new($false).GetBytes("sync tunnel 1.0 smoke test $vaultId")
    $hash = Get-Sha256 $data
    $putHeaders = New-MutationHeaders $deviceToken 0 $hash
    $encodedPath = [Uri]::EscapeDataString($path)
    $put = Invoke-RestMethod -Method Put -Uri "$vaultBase/files/content?path=$encodedPath" -Headers $putHeaders -ContentType "application/octet-stream" -Body $data
    if (-not $put.changed) { throw "Whole-file upload did not create a revision" }
    $retry = Invoke-RestMethod -Method Put -Uri "$vaultBase/files/content?path=$encodedPath" -Headers $putHeaders -ContentType "application/octet-stream" -Body $data
    if ($retry.change.revision -ne $put.change.revision) { throw "Idempotent mutation retry returned a different revision" }
    $operation = Invoke-RestMethod -Uri "$vaultBase/operations/$($putHeaders['X-Operation-ID'])" -Headers $auth
    if ($operation.change.revision -ne $put.change.revision) { throw "Operation lookup failed" }

    $snapshot = Invoke-RestMethod -Uri "$vaultBase/snapshot?limit=10" -Headers $auth
    if ($snapshot.files.Count -ne 1 -or $snapshot.files[0].path -ne $path) { throw "Snapshot did not contain the uploaded file" }
    Invoke-WebRequest -Uri "$vaultBase/blobs/$hash" -Headers $auth -OutFile $downloadTarget | Out-Null
    if ((Get-FileHash -LiteralPath $downloadTarget -Algorithm SHA256).Hash.ToLowerInvariant() -ne $hash) { throw "Blob download hash mismatch" }

    $chunkPath = "folder/chunk-note.md"
    $missingBody = @{ hashes = @($hash) } | ConvertTo-Json -Compress
    $missing = Invoke-RestMethod -Method Post -Uri "$vaultBase/chunks/missing" -Headers $auth -ContentType "application/json" -Body $missingBody
    if (@($missing.missing).Count -ne 1) { throw "Chunk was not reported missing" }
    Invoke-RestMethod -Method Put -Uri "$vaultBase/chunks/$hash" -Headers $auth -ContentType "application/octet-stream" -Body $data | Out-Null
    $commitHeaders = New-MutationHeaders $deviceToken 0 $hash
    $manifestBody = @{ size = $data.Length; chunks = @(@{ hash = $hash; size = $data.Length }) } | ConvertTo-Json -Compress -Depth 4
    $commit = Invoke-RestMethod -Method Post -Uri "$vaultBase/files/commit?path=$([Uri]::EscapeDataString($chunkPath))" -Headers $commitHeaders -ContentType "application/json" -Body $manifestBody
    if (-not $commit.changed) { throw "Chunk manifest commit failed" }
    $manifest = Invoke-RestMethod -Uri "$vaultBase/manifests/$hash" -Headers $auth
    if ($manifest.size -ne $data.Length -or @($manifest.chunks).Count -ne 1) { throw "Chunk manifest lookup failed" }

    $renamedPath = "folder/chunk-note-renamed.md"
    $renameHeaders = New-MutationHeaders $deviceToken $commit.change.revision
    $renameBody = @{ from = $chunkPath; to = $renamedPath } | ConvertTo-Json -Compress
    $rename = Invoke-RestMethod -Method Post -Uri "$vaultBase/rename" -Headers $renameHeaders -ContentType "application/json" -Body $renameBody
    if (-not $rename.changed -or $rename.change.path -ne $renamedPath -or -not $rename.related_changes[0].deleted) { throw "Atomic rename failed" }

    $history = Invoke-RestMethod -Uri "$vaultBase/history?path=$encodedPath&limit=10" -Headers $auth
    if (@($history.versions).Count -ne 1) { throw "History lookup failed" }
    $deleteBody = @{ items = @(
        @{ path = $path; base_revision = $put.change.revision; modified_at = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() },
        @{ path = $renamedPath; base_revision = $rename.change.revision; modified_at = [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds() }
    ) } | ConvertTo-Json -Compress -Depth 4
    $deleteHeaders = @{ Authorization = "Bearer $deviceToken"; "X-Operation-ID" = [Guid]::NewGuid().ToString() }
    $deleted = Invoke-RestMethod -Method Post -Uri "$vaultBase/batch/delete" -Headers $deleteHeaders -ContentType "application/json" -Body $deleteBody
    if (-not $deleted.changed -or @($deleted.changes).Count -ne 2) { throw "Batch delete failed" }

    $deletedOriginal = @($deleted.changes | Where-Object { $_.path -eq $path })[0]
    $restoreHeaders = New-MutationHeaders $deviceToken $deletedOriginal.revision
    $restoreBody = @{ path = $path; source_revision = $put.change.revision } | ConvertTo-Json -Compress
    $restored = Invoke-RestMethod -Method Post -Uri "$vaultBase/restore" -Headers $restoreHeaders -ContentType "application/json" -Body $restoreBody
    if ($restored.change.restored_from_revision -ne $put.change.revision -or $restored.change.deleted) { throw "History restore failed" }

    $ackBody = @{ revision = $restored.change.revision } | ConvertTo-Json -Compress
    Invoke-RestMethod -Method Post -Uri "$vaultBase/ack" -Headers $auth -ContentType "application/json" -Body $ackBody | Out-Null
    $rotated = Invoke-RestMethod -Method Post -Uri "$vaultBase/credential/rotate" -Headers $auth
    $rotatedToken = $rotated.token
    Assert-Unauthorized { Invoke-RestMethod -Uri "$vaultBase/status" -Headers $auth }
    $rotatedAuth = @{ Authorization = "Bearer $rotatedToken" }
    Invoke-RestMethod -Uri "$vaultBase/status" -Headers $rotatedAuth | Out-Null

    $statusBody = @{ status = "revoked" } | ConvertTo-Json -Compress
    Invoke-RestMethod -Method Post -Uri "$AdminUrl/admin/v1/vaults/$vaultId/devices/$($paired.device.id)/status" -Headers $adminHeaders -ContentType "application/json" -Body $statusBody | Out-Null
    Assert-Unauthorized { Invoke-RestMethod -Uri "$vaultBase/status" -Headers $rotatedAuth }

    $doctor = Invoke-RestMethod -Uri "$AdminUrl/admin/v1/doctor" -Headers $adminHeaders
    if (-not $doctor.ok) { throw "Server doctor reported a problem" }
    Write-Host "FINAL_PROTOCOL_SMOKE_PASS vault=$vaultId"
} finally {
    if (Test-Path -LiteralPath $downloadTarget) { Remove-Item -LiteralPath $downloadTarget -Force }
    $deviceToken = ""
    $rotatedToken = ""
    $AdminToken = ""
}
