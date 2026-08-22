[CmdletBinding()]
param(
    [ValidateRange(1024, 65533)]
    [int]$PublicPort = 18787,
    [ValidateRange(1025, 65534)]
    [int]$AdminPort = 18788
)

$ErrorActionPreference = "Stop"
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
$temporaryRoot = [IO.Path]::GetFullPath((Join-Path ([IO.Path]::GetTempPath()) ("sync-tunnel-selftest-" + [Guid]::NewGuid().ToString("N"))))
$systemTemporaryRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
if (-not $temporaryRoot.StartsWith($systemTemporaryRoot, [StringComparison]::OrdinalIgnoreCase) -or
    -not [IO.Path]::GetFileName($temporaryRoot).StartsWith("sync-tunnel-selftest-")) {
    throw "Unsafe temporary test directory"
}
New-Item -ItemType Directory -Path $temporaryRoot | Out-Null

$binary = Join-Path $temporaryRoot "obsidian-sync-server.exe"
$adminTokenFile = Join-Path $temporaryRoot "admin-token.txt"
$adminToken = [Convert]::ToBase64String([Security.Cryptography.RandomNumberGenerator]::GetBytes(36)).TrimEnd("=").Replace("+", "-").Replace("/", "_")
[IO.File]::WriteAllText($adminTokenFile, $adminToken, [Text.UTF8Encoding]::new($false))
$process = $null
try {
    Push-Location $repoRoot
    try {
        & go build -o $binary ./cmd/obsidian-sync-server
        if ($LASTEXITCODE -ne 0) { throw "Server build failed" }
    } finally {
        Pop-Location
    }

    $arguments = @(
        "serve", "--listen", "127.0.0.1:$PublicPort",
        "--admin-listen", "127.0.0.1:$AdminPort",
        "--database", (Join-Path $temporaryRoot "sync.db"),
        "--admin-token-file", $adminTokenFile,
        "--log", (Join-Path $temporaryRoot "server.jsonl"),
        "--max-file-bytes", "67108864", "--min-free-bytes", "0"
    )
    $process = Start-Process -FilePath $binary -ArgumentList $arguments -WindowStyle Hidden -PassThru
    $ready = $false
    for ($attempt = 0; $attempt -lt 40; $attempt += 1) {
        try {
            $health = Invoke-RestMethod -Uri "http://127.0.0.1:$PublicPort/healthz" -TimeoutSec 1
            if ($health.status -eq "ok") { $ready = $true; break }
        } catch {}
        Start-Sleep -Milliseconds 250
    }
    if (-not $ready) { throw "Temporary server did not become healthy" }

    & (Join-Path $PSScriptRoot "admin.ps1") -CreateVault -VaultId cli-managed -DisplayName "CLI managed" -AdminPort $AdminPort -AdminTokenFile $adminTokenFile | Out-Null
    & (Join-Path $PSScriptRoot "admin.ps1") -UpdateVault -VaultId cli-managed -DisplayName "CLI managed" -VaultStatus suspended -QuotaBytes 4096 -MaxFiles 20 -AdminPort $AdminPort -AdminTokenFile $adminTokenFile | Out-Null
    $vaultResponse = & (Join-Path $PSScriptRoot "admin.ps1") -ListVaults -AdminPort $AdminPort -AdminTokenFile $adminTokenFile
    $managedVault = @($vaultResponse.vaults) | Where-Object { $_.id -eq "cli-managed" }
    if (-not $managedVault -or $managedVault.status -ne "suspended") { throw "Admin CLI lifecycle failed" }

    & (Join-Path $PSScriptRoot "smoke-test.ps1") -PublicUrl "http://127.0.0.1:$PublicPort" -AdminUrl "http://127.0.0.1:$AdminPort" -AdminToken $adminToken
    Write-Host "ISOLATED_SMOKE_SELFTEST_PASS"
} finally {
    if ($process -and -not $process.HasExited) {
        Stop-Process -Id $process.Id -Force
        $process.WaitForExit()
    }
    $adminToken = ""
    if (Test-Path -LiteralPath $temporaryRoot) {
        $verified = [IO.Path]::GetFullPath($temporaryRoot)
        if ($verified.StartsWith($systemTemporaryRoot, [StringComparison]::OrdinalIgnoreCase) -and
            [IO.Path]::GetFileName($verified).StartsWith("sync-tunnel-selftest-")) {
            [IO.Directory]::Delete($verified, $true)
        }
    }
}
