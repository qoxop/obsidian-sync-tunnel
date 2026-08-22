[CmdletBinding(DefaultParameterSetName = "ListVaults")]
param(
    [Parameter(ParameterSetName = "CreateVault", Mandatory)] [switch]$CreateVault,
    [Parameter(ParameterSetName = "UpdateVault", Mandatory)] [switch]$UpdateVault,
    [Parameter(ParameterSetName = "CreatePairing", Mandatory)] [switch]$CreatePairing,
    [Parameter(ParameterSetName = "ListDevices", Mandatory)] [switch]$ListDevices,
    [Parameter(ParameterSetName = "SetDeviceStatus", Mandatory)] [switch]$SetDeviceStatus,
    [Parameter(ParameterSetName = "ListAudit", Mandatory)] [switch]$ListAudit,
    [Parameter(ParameterSetName = "Doctor", Mandatory)] [switch]$Doctor,
	[Parameter(ParameterSetName = "Stats", Mandatory)] [switch]$Stats,
    [Parameter(ParameterSetName = "PlanGC", Mandatory)] [switch]$PlanGC,
    [Parameter(ParameterSetName = "ExecuteGC", Mandatory)] [switch]$ExecuteGC,
    [Parameter(ParameterSetName = "ListVaults")] [switch]$ListVaults,
    [Parameter(ParameterSetName = "CreateVault", Mandatory)]
    [Parameter(ParameterSetName = "UpdateVault", Mandatory)]
    [Parameter(ParameterSetName = "CreatePairing", Mandatory)]
    [Parameter(ParameterSetName = "ListDevices", Mandatory)]
    [Parameter(ParameterSetName = "SetDeviceStatus", Mandatory)] [string]$VaultId,
    [Parameter(ParameterSetName = "CreateVault")]
    [Parameter(ParameterSetName = "UpdateVault", Mandatory)] [string]$DisplayName = "",
    [Parameter(ParameterSetName = "CreateVault")]
    [Parameter(ParameterSetName = "UpdateVault")] [int64]$QuotaBytes = 0,
    [Parameter(ParameterSetName = "CreateVault")]
    [Parameter(ParameterSetName = "UpdateVault")] [int64]$MaxFiles = 0,
    [Parameter(ParameterSetName = "UpdateVault")] [ValidateSet("active", "suspended")] [string]$VaultStatus = "active",
    [Parameter(ParameterSetName = "CreatePairing")] [ValidateRange(60, 86400)] [int]$TTLSeconds = 600,
    [Parameter(ParameterSetName = "SetDeviceStatus", Mandatory)] [string]$DeviceId,
	[Parameter(ParameterSetName = "SetDeviceStatus", Mandatory)] [ValidateSet("retired", "revoked")] [string]$Status,
    [Parameter(ParameterSetName = "PlanGC")] [ValidateRange(1, 3650)] [int]$RetentionDays = 90,
    [Parameter(ParameterSetName = "PlanGC")] [ValidateRange(1, 1000)] [int]$KeepVersions = 20,
    [Parameter(ParameterSetName = "ExecuteGC", Mandatory)] [string]$PlanId,
    [Parameter(ParameterSetName = "ExecuteGC", Mandatory)] [string]$PlanHash,
    [Parameter(ParameterSetName = "ListAudit")] [ValidateRange(1, 1000)] [int]$Limit = 100,
    [ValidateRange(1, 65535)] [int]$AdminPort = 8788,
    [string]$AdminTokenFile = ""
)

$ErrorActionPreference = "Stop"
$repoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot ".."))
if (-not $AdminTokenFile) { $AdminTokenFile = Join-Path $repoRoot "secrets\admin-token.txt" }
$token = [System.IO.File]::ReadAllText([System.IO.Path]::GetFullPath($AdminTokenFile)).Trim()
if ($token.Length -lt 32) { throw "Admin token is invalid" }
$headers = @{ Authorization = "Bearer $token" }
$base = "http://127.0.0.1:$AdminPort/admin/v1"

function Invoke-SyncAdmin([string]$Method, [string]$Path, [object]$Body = $null) {
    $parameters = @{ Method = $Method; Uri = "$base$Path"; Headers = $headers }
    if ($null -ne $Body) {
        $parameters.ContentType = "application/json"
        $parameters.Body = $Body | ConvertTo-Json -Compress
    }
    Invoke-RestMethod @parameters
}

try {
    switch ($PSCmdlet.ParameterSetName) {
        "CreateVault" { Invoke-SyncAdmin Post "/vaults" @{ id = $VaultId; display_name = $DisplayName; quota_bytes = $QuotaBytes; max_files = $MaxFiles } }
        "UpdateVault" { Invoke-SyncAdmin Put "/vaults/$([uri]::EscapeDataString($VaultId))" @{ display_name = $DisplayName; status = $VaultStatus; quota_bytes = $QuotaBytes; max_files = $MaxFiles } }
        "CreatePairing" { Invoke-SyncAdmin Post "/vaults/$([uri]::EscapeDataString($VaultId))/pairing-codes" @{ ttl_seconds = $TTLSeconds } }
        "ListDevices" { Invoke-SyncAdmin Get "/vaults/$([uri]::EscapeDataString($VaultId))/devices" }
        "SetDeviceStatus" { Invoke-SyncAdmin Post "/vaults/$([uri]::EscapeDataString($VaultId))/devices/$([uri]::EscapeDataString($DeviceId))/status" @{ status = $Status } }
        "ListAudit" { Invoke-SyncAdmin Get "/audit?limit=$Limit" }
        "Doctor" { Invoke-SyncAdmin Get "/doctor" }
		"Stats" { Invoke-SyncAdmin Get "/stats" }
        "PlanGC" { Invoke-SyncAdmin Post "/gc/plans" @{ retention_days = $RetentionDays; keep_versions = $KeepVersions } }
        "ExecuteGC" { Invoke-SyncAdmin Post "/gc/plans/$([uri]::EscapeDataString($PlanId))/execute" @{ plan_hash = $PlanHash } }
        default { Invoke-SyncAdmin Get "/vaults" }
    }
} finally {
    $token = $null
    $headers = $null
}
