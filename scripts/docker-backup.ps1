[CmdletBinding()]
param([ValidateRange(0, 3650)] [int]$KeepLast = 0)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))

Push-Location $repoRoot
try {
    $composeJsonText = (& docker compose config --format json) -join "`n"
    if ($LASTEXITCODE -ne 0) { throw "Could not resolve Compose configuration" }
    $composeConfig = $composeJsonText | ConvertFrom-Json
    $service = $composeConfig.services."sync-server"
    $backupMount = $service.volumes | Where-Object { $_.target -eq "/backups" } | Select-Object -First 1
    if (-not $backupMount -or -not $backupMount.source) { throw "Compose /backups bind mount was not found" }
    $backupRoot = [System.IO.Path]::GetFullPath([string]$backupMount.source)
    $adminSecretName = @($service.secrets)[0].source
    $secret = $composeConfig.secrets.$adminSecretName
    if (-not $secret.file) { throw "Admin token secret source was not found" }
    $adminToken = [System.IO.File]::ReadAllText([string]$secret.file).Trim()
    if ($adminToken.Length -lt 32) { throw "Admin token is invalid" }

    $adminPort = 8788
    $adminBinding = $service.ports | Where-Object { $_.target -eq 8788 } | Select-Object -First 1
    if ($adminBinding -and $adminBinding.published) { $adminPort = [int]$adminBinding.published }
    $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $containerDestination = "/backups/$stamp"
    $headers = @{ Authorization = "Bearer $adminToken" }
    $body = @{ destination = $containerDestination } | ConvertTo-Json -Compress
    try {
        $null = Invoke-RestMethod -Method Post -Uri "http://127.0.0.1:$adminPort/admin/v1/backups" -Headers $headers -ContentType "application/json" -Body $body
    } finally {
        $adminToken = $null
        $headers = $null
    }
    & docker compose exec --no-TTY sync-server /app/obsidian-sync-server verify-backup --directory $containerDestination | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "Backup was created but verification failed" }
    $hostDestination = Join-Path $backupRoot $stamp
    Write-Host "Verified online backup created: $hostDestination"
	if ($KeepLast -gt 0) {
		$rootPrefix = $backupRoot.TrimEnd([System.IO.Path]::DirectorySeparatorChar) + [System.IO.Path]::DirectorySeparatorChar
		$expired = @(Get-ChildItem -LiteralPath $backupRoot -Directory | Where-Object { $_.Name -match '^\d{8}-\d{6}$' -and (Test-Path -LiteralPath (Join-Path $_.FullName 'backup.json') -PathType Leaf) } | Sort-Object Name -Descending | Select-Object -Skip $KeepLast)
		foreach ($item in $expired) {
			$resolved = [System.IO.Path]::GetFullPath($item.FullName)
			if (-not $resolved.StartsWith($rootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) { throw "Refusing to prune outside backup root: $resolved" }
			Remove-Item -LiteralPath $resolved -Recurse -Force
			Write-Host "Pruned expired verified backup: $resolved"
		}
	}
    Write-Warning "The backup contains plaintext Vault content. Replicate it to encrypted storage on another device."
} finally {
    Pop-Location
}
