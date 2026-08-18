#!/usr/bin/env bash

set -euo pipefail
IFS=$'\n\t'

REPOSITORY="qoxop/obsidian-sync-tunnel"
PLUGIN_ID="sync-tunnel"
DEFAULT_VERSION="0.3.0-beta.2"
PROBE_ROOT="_sync-tunnel-verification"

COMMAND=""
VAULT_PATH=""
VERSION="$DEFAULT_VERSION"
ALLOW_NON_EMPTY=0
PROBE_SIZE_MIB=5
TEMP_DIRECTORY=""
RESUME_RELATIVE=""
RESUME_HASH=""
RESUME_SIZE=""

usage() {
  cat <<'EOF'
Sync Tunnel macOS second-device installer and verifier

Usage:
  macos-device-test.sh guided       --vault PATH [--version VERSION]
  macos-device-test.sh prepare      --vault PATH [--version VERSION] [--allow-non-empty]
  macos-device-test.sh status       --vault PATH [--version VERSION]
  macos-device-test.sh create-probe --vault PATH [--probe-size-mib NUMBER]
  macos-device-test.sh verify-probe --vault PATH [--version VERSION]
  macos-device-test.sh create-resume-probe     --vault PATH --probe-size-mib NUMBER
  macos-device-test.sh check-resume-interrupted --vault PATH
  macos-device-test.sh verify-resume-probe     --vault PATH [--version VERSION]

Commands:
  guided       Install the plugin and guide the complete interactive test.
  prepare      Download a pinned GitHub Release into an empty test Vault.
  status       Verify plugin version, configuration, cursor and persistent queues.
  create-probe Create Markdown, Canvas, PNG, Unicode-path and >4 MiB test files.
  verify-probe Verify local hashes and confirmed file revisions after two syncs.
  create-resume-probe Create a large file for the quit-and-resume acceptance test.
  check-resume-interrupted Confirm that a quit left a resumable put in the outbox.
  verify-resume-probe Verify the resumed upload, queues and local content hash.

The script never asks for or stores API Token or Cloudflare secrets. Enter those
only through Obsidian SecretStorage when the guided flow pauses.
EOF
}

info() {
  printf '[INFO] %s\n' "$*"
}

warn() {
  printf '[WARN] %s\n' "$*" >&2
}

fail() {
  printf '[ERROR] %s\n' "$*" >&2
  exit 1
}

cleanup() {
  if [[ -n "$TEMP_DIRECTORY" && -d "$TEMP_DIRECTORY" ]]; then
    rm -rf -- "$TEMP_DIRECTORY"
  fi
}
trap cleanup EXIT HUP INT TERM

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "Missing required command: $1"
}

file_size_bytes() {
  if [[ "$(uname -s)" == "Darwin" ]]; then
    stat -f '%z' "$1"
  else
    stat -c '%s' "$1"
  fi
}

parse_arguments() {
  [[ $# -gt 0 ]] || {
    usage
    exit 1
  }
  COMMAND="$1"
  shift

  while [[ $# -gt 0 ]]; do
    case "$1" in
      --vault)
        [[ $# -ge 2 ]] || fail "--vault requires a path"
        VAULT_PATH="$2"
        shift 2
        ;;
      --version)
        [[ $# -ge 2 ]] || fail "--version requires a value"
        VERSION="$2"
        shift 2
        ;;
      --probe-size-mib)
        [[ $# -ge 2 ]] || fail "--probe-size-mib requires a number"
        PROBE_SIZE_MIB="$2"
        shift 2
        ;;
      --allow-non-empty)
        ALLOW_NON_EMPTY=1
        shift
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        fail "Unknown argument: $1"
        ;;
    esac
  done

  case "$COMMAND" in
    guided|prepare|status|create-probe|verify-probe|create-resume-probe|check-resume-interrupted|verify-resume-probe) ;;
    help)
      usage
      exit 0
      ;;
    *) fail "Unknown command: $COMMAND" ;;
  esac

  [[ -n "$VAULT_PATH" ]] || fail "--vault is required"
  [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || fail "Invalid release version: $VERSION"
  [[ "$PROBE_SIZE_MIB" =~ ^[0-9]+$ ]] || fail "Probe size must be a positive integer"
  (( PROBE_SIZE_MIB >= 5 )) || fail "Probe size must be at least 5 MiB to exercise chunked transfer"
}

prepare_environment() {
  local system_name
  system_name="$(uname -s)"
  if [[ "$system_name" != "Darwin" && "${SYNC_TUNNEL_ALLOW_NON_DARWIN:-0}" != "1" ]]; then
    fail "This script is intended for macOS (detected: $system_name)"
  fi

  require_command awk

  [[ -d "$VAULT_PATH" ]] || fail "Vault directory does not exist: $VAULT_PATH"
  VAULT_PATH="$(cd "$VAULT_PATH" && pwd -P)"
  [[ -d "$VAULT_PATH/.obsidian" ]] || fail "Not an initialized Obsidian Vault (missing .obsidian): $VAULT_PATH"
}

emit_json_summary() {
  local manifest_path="$1"
  local data_path="$2"

  if command -v python3 >/dev/null 2>&1; then
    python3 - "$manifest_path" "$data_path" <<'PY'
import json
import sys

manifest_path, data_path = sys.argv[1:3]
with open(manifest_path, "r", encoding="utf-8") as handle:
    manifest = json.load(handle)

data = {}
if data_path != "-":
    with open(data_path, "r", encoding="utf-8") as handle:
        data = json.load(handle)

settings = data.get("settings") if isinstance(data.get("settings"), dict) else {}

def object_count(value):
    return len(value) if isinstance(value, dict) else 0

def boolean(value):
    return "true" if value is True else "false"

fields = [
    ("plugin_id", manifest.get("id", "")),
    ("plugin_version", manifest.get("version", "")),
    ("schema_version", data.get("schemaVersion", 0)),
    ("server_configured", boolean(bool(settings.get("serverUrl")))),
    ("vault_configured", boolean(bool(settings.get("vaultId")))),
    ("device_configured", boolean(bool(settings.get("deviceId")))),
    ("api_token_linked", boolean(bool(settings.get("apiTokenSecretName")))),
    ("sync_profile", settings.get("syncProfile", "")),
    ("automatic_sync", boolean(settings.get("automaticSync"))),
    ("initial_sync_completed", boolean(data.get("initialSyncCompleted"))),
    ("pending_initial_mode", data.get("pendingInitialSyncMode") or ""),
    ("cursor", data.get("cursor", 0)),
    ("tracked_files", object_count(data.get("files"))),
    ("scan_cache", object_count(data.get("scanCache"))),
    ("pending_paths", object_count(data.get("pendingPaths"))),
    ("outbox", object_count(data.get("outbox"))),
    ("inbox", object_count(data.get("inbox"))),
    ("pending_renames", object_count(data.get("pendingRenames"))),
    ("needs_full_scan", boolean(data.get("needsFullScan"))),
]
for key, value in fields:
    print(f"{key}\t{value}")
PY
    return
  fi

  [[ -x /usr/bin/osascript ]] || fail "Neither python3 nor macOS osascript is available for JSON validation"
  /usr/bin/osascript -l JavaScript - "$manifest_path" "$data_path" <<'JXA'
ObjC.import("Foundation");

function readText(path) {
  const value = $.NSString.stringWithContentsOfFileEncodingError(
    path,
    $.NSUTF8StringEncoding,
    null
  );
  if (!value) throw new Error("Cannot read JSON file: " + path);
  return ObjC.unwrap(value);
}

function run(argv) {
  const manifest = JSON.parse(readText(argv[0]));
  const data = argv[1] === "-" ? {} : JSON.parse(readText(argv[1]));
  const settings = data.settings && typeof data.settings === "object" ? data.settings : {};
  const count = value => value && typeof value === "object" && !Array.isArray(value) ? Object.keys(value).length : 0;
  const bool = value => value === true ? "true" : "false";
  const fields = [
    ["plugin_id", manifest.id || ""],
    ["plugin_version", manifest.version || ""],
    ["schema_version", data.schemaVersion || 0],
    ["server_configured", bool(Boolean(settings.serverUrl))],
    ["vault_configured", bool(Boolean(settings.vaultId))],
    ["device_configured", bool(Boolean(settings.deviceId))],
    ["api_token_linked", bool(Boolean(settings.apiTokenSecretName))],
    ["sync_profile", settings.syncProfile || ""],
    ["automatic_sync", bool(settings.automaticSync)],
    ["initial_sync_completed", bool(data.initialSyncCompleted)],
    ["pending_initial_mode", data.pendingInitialSyncMode || ""],
    ["cursor", data.cursor || 0],
    ["tracked_files", count(data.files)],
    ["scan_cache", count(data.scanCache)],
    ["pending_paths", count(data.pendingPaths)],
    ["outbox", count(data.outbox)],
    ["inbox", count(data.inbox)],
    ["pending_renames", count(data.pendingRenames)],
    ["needs_full_scan", bool(data.needsFullScan)],
  ];
  return fields.map(item => item[0] + "\t" + String(item[1])).join("\n");
}
JXA
}

summary_value() {
  local summary="$1"
  local requested_key="$2"
  printf '%s\n' "$summary" | awk -F '\t' -v key="$requested_key" '$1 == key { sub(/^[^\t]*\t/, ""); print; exit }'
}

print_check() {
  local label="$1"
  local passed="$2"
  local detail="$3"
  if [[ "$passed" == "true" ]]; then
    printf '  [PASS] %-27s %s\n' "$label" "$detail"
    return 0
  fi
  printf '  [FAIL] %-27s %s\n' "$label" "$detail" >&2
  return 1
}

status_client() {
  local plugin_directory="$VAULT_PATH/.obsidian/plugins/$PLUGIN_ID"
  local manifest_path="$plugin_directory/manifest.json"
  local data_path="$plugin_directory/data.json"
  local summary
  local failures=0
  local cursor
  local tracked_files
  local field
  local value

  [[ -f "$manifest_path" ]] || {
    warn "Plugin manifest is missing: $manifest_path"
    return 1
  }
  [[ -f "$data_path" ]] || {
    warn "Plugin data is missing. Enable Sync Tunnel in Obsidian, configure it and run the first sync."
    return 1
  }

  summary="$(emit_json_summary "$manifest_path" "$data_path")"
  info "Safe client-state summary (URLs, IDs and secrets are not printed):"

  value="$(summary_value "$summary" plugin_id)"
  print_check "Plugin ID" "$([[ "$value" == "$PLUGIN_ID" ]] && printf true || printf false)" "$value" || failures=$((failures + 1))

  value="$(summary_value "$summary" plugin_version)"
  print_check "Plugin version" "$([[ "$value" == "$VERSION" ]] && printf true || printf false)" "$value (expected $VERSION)" || failures=$((failures + 1))

  for field in server_configured vault_configured device_configured api_token_linked initial_sync_completed; do
    value="$(summary_value "$summary" "$field")"
    print_check "$field" "$value" "$value" || failures=$((failures + 1))
  done

  value="$(summary_value "$summary" sync_profile)"
  print_check "Sync profile" "$([[ "$value" == "recommended" ]] && printf true || printf false)" "${value:-<unset>} (expected recommended)" || failures=$((failures + 1))

  value="$(summary_value "$summary" pending_initial_mode)"
  print_check "Initial mode cleared" "$([[ -z "$value" ]] && printf true || printf false)" "${value:-<none>}" || failures=$((failures + 1))

  cursor="$(summary_value "$summary" cursor)"
  print_check "Server cursor" "$([[ "$cursor" =~ ^[0-9]+$ && "$cursor" -gt 0 ]] && printf true || printf false)" "$cursor" || failures=$((failures + 1))

  tracked_files="$(summary_value "$summary" tracked_files)"
  print_check "Tracked files" "$([[ "$tracked_files" =~ ^[0-9]+$ && "$tracked_files" -gt 0 ]] && printf true || printf false)" "$tracked_files" || failures=$((failures + 1))

  for field in pending_paths outbox inbox pending_renames; do
    value="$(summary_value "$summary" "$field")"
    print_check "$field" "$([[ "$value" == "0" ]] && printf true || printf false)" "$value" || failures=$((failures + 1))
  done

  value="$(summary_value "$summary" needs_full_scan)"
  print_check "Full scan settled" "$([[ "$value" == "false" ]] && printf true || printf false)" "$value" || failures=$((failures + 1))

  value="$(summary_value "$summary" automatic_sync)"
  info "Automatic sync: $value (either value is valid after the initial manual test)"

  if (( failures > 0 )); then
    warn "$failures client-state check(s) failed"
    return 1
  fi
  info "Client state PASS"
}

prepare_client() {
  local non_metadata_entry
  local plugin_directory="$VAULT_PATH/.obsidian/plugins/$PLUGIN_ID"
  local legacy_directory="$VAULT_PATH/.obsidian/plugins/obsidian-sync-tunnel"
  local release_base="https://github.com/$REPOSITORY/releases/download/$VERSION"
  local manifest_summary
  local downloaded_id
  local downloaded_version
  local backup_directory
  local file

  require_command curl
  require_command find
  require_command grep
  require_command install

  if (( ALLOW_NON_EMPTY == 0 )); then
    non_metadata_entry="$(find "$VAULT_PATH" -mindepth 1 -maxdepth 1 ! -name .obsidian ! -name .DS_Store -print -quit)"
    [[ -z "$non_metadata_entry" ]] || fail "The test Vault is not empty: $non_metadata_entry (use a new empty Vault)"
  fi

  if [[ -d "$legacy_directory" ]]; then
    warn "Legacy plugin directory found and left untouched: $legacy_directory"
    warn "Do not enable both plugin copies."
  fi

  TEMP_DIRECTORY="$(mktemp -d "${TMPDIR:-/tmp}/sync-tunnel-macos.XXXXXX")"
  for file in main.js manifest.json styles.css; do
    info "Downloading $file from GitHub Release $VERSION"
    curl --fail --location --silent --show-error \
      --proto '=https' --tlsv1.2 --retry 3 \
      --output "$TEMP_DIRECTORY/$file" "$release_base/$file"
    [[ -s "$TEMP_DIRECTORY/$file" ]] || fail "Downloaded asset is empty: $file"
  done

  manifest_summary="$(emit_json_summary "$TEMP_DIRECTORY/manifest.json" -)"
  downloaded_id="$(summary_value "$manifest_summary" plugin_id)"
  downloaded_version="$(summary_value "$manifest_summary" plugin_version)"
  [[ "$downloaded_id" == "$PLUGIN_ID" ]] || fail "Unexpected plugin ID in release: $downloaded_id"
  [[ "$downloaded_version" == "$VERSION" ]] || fail "Release manifest version is $downloaded_version, expected $VERSION"
  if grep -F 'import("node:' "$TEMP_DIRECTORY/main.js" >/dev/null 2>&1 || grep -F "import('node:" "$TEMP_DIRECTORY/main.js" >/dev/null 2>&1; then
    fail "Release bundle contains an unsupported dynamic node: import"
  fi

  if [[ -d "$plugin_directory" ]]; then
    backup_directory="$HOME/Documents/ObsidianSyncBackups/client-state/$(date '+%Y%m%d-%H%M%S')-macos-install-$$"
    mkdir -p "$backup_directory"
    cp -R "$plugin_directory" "$backup_directory/$PLUGIN_ID"
    chmod -R go-rwx "$backup_directory"
    info "Existing plugin state backed up to: $backup_directory"
  fi

  mkdir -p "$plugin_directory"
  for file in main.js manifest.json styles.css; do
    install -m 0644 "$TEMP_DIRECTORY/$file" "$plugin_directory/$file"
  done
  printf 'repository=%s\nversion=%s\ninstalled_at=%s\n' \
    "$REPOSITORY" "$VERSION" "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
    > "$plugin_directory/macos-test-install.txt"

  info "Installed Sync Tunnel $VERSION into: $plugin_directory"
  info "The script did not copy another device's data.json and did not write any secret."
  info "Reload Obsidian, enable Sync Tunnel, then configure the same Server URL and Vault ID with a unique Device ID."
}

decode_probe_png() {
  local output_path="$1"
  local encoded="iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
  if printf '%s' "$encoded" | base64 --decode > "$output_path" 2>/dev/null; then
    return
  fi
  printf '%s' "$encoded" | base64 -D > "$output_path"
}

create_probe() {
  local timestamp
  local probe_relative
  local probe_directory
  local pointer_path="$VAULT_PATH/.obsidian/plugins/$PLUGIN_ID/macos-last-probe.txt"

  status_client || fail "Client state is not ready. Finish the first sync before creating the probe."
  require_command base64
  require_command dd
  require_command find
  require_command shasum

  timestamp="$(date '+%Y%m%d-%H%M%S')"
  probe_relative="$PROBE_ROOT/mac-device-b-$timestamp"
  probe_directory="$VAULT_PATH/$probe_relative"
  mkdir -p "$probe_directory/附件" "$probe_directory/Nested Path"

  cat > "$probe_directory/README 测试.md" <<EOF
# Sync Tunnel macOS verification

- Created at: $(date -u '+%Y-%m-%dT%H:%M:%SZ')
- Purpose: Markdown, spaces and Unicode path synchronization.
- Device role: macOS device B.
EOF

  cat > "$probe_directory/diagram.canvas" <<'EOF'
{"nodes":[{"id":"mac-probe","type":"text","text":"Sync Tunnel macOS Canvas probe","x":0,"y":0,"width":360,"height":120}],"edges":[]}
EOF

  cat > "$probe_directory/Nested Path/CasePath.md" <<'EOF'
This file checks nested paths, spaces and case-preserving file names on macOS.
EOF

  decode_probe_png "$probe_directory/附件/像素.png"
  dd if=/dev/urandom of="$probe_directory/附件/large-file.bin" bs=1048576 count="$PROBE_SIZE_MIB" 2>/dev/null
  printf 'This file must be excluded by the recommended profile.\n' > "$probe_directory/.DS_Store"

  (
    cd "$probe_directory"
    find . -type f ! -name SHA256SUMS ! -name .DS_Store -print | LC_ALL=C sort | while IFS= read -r file; do
      shasum -a 256 "$file"
    done
  ) > "$probe_directory/SHA256SUMS"

  printf '%s\n' "$probe_relative" > "$pointer_path"
  chmod 0600 "$pointer_path"

  info "Created probe: $probe_directory"
  info "Probe includes Markdown, Canvas, PNG, Unicode paths and a $PROBE_SIZE_MIB MiB binary."
  info "It also includes .DS_Store, which must remain excluded."
  info "Now click Sync now, wait for completion, and click it once more; the second result must be all zero."
}

emit_probe_state_audit() {
  local data_path="$1"
  local probe_relative="$2"
  local sums_path="$3"

  if command -v python3 >/dev/null 2>&1; then
    python3 - "$data_path" "$probe_relative" "$sums_path" <<'PY'
import json
import sys

data_path, probe_relative, sums_path = sys.argv[1:4]
with open(data_path, "r", encoding="utf-8") as handle:
    data = json.load(handle)
with open(sums_path, "r", encoding="utf-8") as handle:
    lines = [line.rstrip("\n") for line in handle if line.strip()]

files = data.get("files") if isinstance(data.get("files"), dict) else {}
problems = []
tracked = 0
for line in lines:
    if "  " not in line:
        problems.append("Malformed SHA256SUMS line")
        continue
    expected_hash, relative_path = line.split("  ", 1)
    relative_path = relative_path[2:] if relative_path.startswith("./") else relative_path
    vault_path = f"{probe_relative}/{relative_path}"
    state = files.get(vault_path)
    if not isinstance(state, dict):
        problems.append(f"Not tracked: {vault_path}")
        continue
    if state.get("deleted") is True:
        problems.append(f"Unexpectedly deleted: {vault_path}")
        continue
    if state.get("hash") != expected_hash:
        problems.append(f"State hash mismatch: {vault_path}")
        continue
    revision = state.get("revision")
    if not isinstance(revision, int) or revision <= 0:
        problems.append(f"No confirmed server revision: {vault_path}")
        continue
    tracked += 1

excluded_path = f"{probe_relative}/.DS_Store"
excluded_state = files.get(excluded_path)
if isinstance(excluded_state, dict) and excluded_state.get("deleted") is not True:
    problems.append(f"Excluded path was tracked: {excluded_path}")

print("result\t" + ("pass" if not problems else "fail"))
print(f"tracked_probe_files\t{tracked}")
for problem in problems:
    print(f"problem\t{problem}")
PY
    return
  fi

  [[ -x /usr/bin/osascript ]] || fail "Neither python3 nor macOS osascript is available for probe validation"
  /usr/bin/osascript -l JavaScript - "$data_path" "$probe_relative" "$sums_path" <<'JXA'
ObjC.import("Foundation");

function readText(path) {
  const value = $.NSString.stringWithContentsOfFileEncodingError(path, $.NSUTF8StringEncoding, null);
  if (!value) throw new Error("Cannot read file: " + path);
  return ObjC.unwrap(value);
}

function run(argv) {
  const data = JSON.parse(readText(argv[0]));
  const probe = argv[1];
  const lines = readText(argv[2]).split(/\r?\n/).filter(line => line.trim().length > 0);
  const files = data.files && typeof data.files === "object" ? data.files : {};
  const problems = [];
  let tracked = 0;

  lines.forEach(line => {
    const separator = line.indexOf("  ");
    if (separator < 0) {
      problems.push("Malformed SHA256SUMS line");
      return;
    }
    const expected = line.slice(0, separator);
    let relative = line.slice(separator + 2);
    if (relative.slice(0, 2) === "./") relative = relative.slice(2);
    const path = probe + "/" + relative;
    const state = files[path];
    if (!state) problems.push("Not tracked: " + path);
    else if (state.deleted === true) problems.push("Unexpectedly deleted: " + path);
    else if (state.hash !== expected) problems.push("State hash mismatch: " + path);
    else if (typeof state.revision !== "number" || state.revision <= 0) problems.push("No confirmed server revision: " + path);
    else tracked += 1;
  });

  const excludedPath = probe + "/.DS_Store";
  const excludedState = files[excludedPath];
  if (excludedState && excludedState.deleted !== true) problems.push("Excluded path was tracked: " + excludedPath);

  const output = [
    "result\t" + (problems.length === 0 ? "pass" : "fail"),
    "tracked_probe_files\t" + String(tracked),
  ];
  problems.forEach(problem => output.push("problem\t" + problem));
  return output.join("\n");
}
JXA
}

verify_probe() {
  local plugin_directory="$VAULT_PATH/.obsidian/plugins/$PLUGIN_ID"
  local data_path="$plugin_directory/data.json"
  local pointer_path="$plugin_directory/macos-last-probe.txt"
  local probe_relative
  local probe_directory
  local audit
  local result
  local tracked
  local report_path="$plugin_directory/macos-verification-report.txt"

  require_command sed
  require_command shasum
  status_client || fail "Persistent queues have not converged; do not accept the test yet."
  [[ -f "$pointer_path" ]] || fail "No probe pointer found. Run create-probe first."
  probe_relative="$(sed -n '1p' "$pointer_path")"
  [[ "$probe_relative" == "$PROBE_ROOT"/mac-device-b-* ]] || fail "Unsafe or unexpected probe path: $probe_relative"
  [[ "$probe_relative" != *..* ]] || fail "Unsafe probe path: $probe_relative"
  probe_directory="$VAULT_PATH/$probe_relative"
  [[ -f "$probe_directory/SHA256SUMS" ]] || fail "Probe checksum manifest is missing: $probe_directory/SHA256SUMS"

  info "Checking local probe content hashes"
  (
    cd "$probe_directory"
    shasum -a 256 -c SHA256SUMS
  )

  audit="$(emit_probe_state_audit "$data_path" "$probe_relative" "$probe_directory/SHA256SUMS")"
  result="$(summary_value "$audit" result)"
  tracked="$(summary_value "$audit" tracked_probe_files)"
  if [[ "$result" != "pass" ]]; then
    printf '%s\n' "$audit" | awk -F '\t' '$1 == "problem" { print "  [FAIL] " $2 }' >&2
    fail "Probe files are not fully confirmed by the server"
  fi

  {
    printf 'result=PASS\n'
    printf 'verified_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'plugin_version=%s\n' "$VERSION"
    printf 'probe=%s\n' "$probe_relative"
    printf 'confirmed_probe_files=%s\n' "$tracked"
    printf 'secrets_included=false\n'
  } > "$report_path"
  chmod 0600 "$report_path"

  info "Probe state PASS: $tracked files have matching hashes and confirmed server revisions."
  info "Excluded .DS_Store was not tracked."
  info "Safe report written to: $report_path"
  info "macOS device-B verification PASS"
}

load_resume_pointer() {
  local pointer_path="$VAULT_PATH/.obsidian/plugins/$PLUGIN_ID/macos-resume-probe.txt"
  [[ -f "$pointer_path" ]] || fail "No resume probe pointer found. Run create-resume-probe first."
  IFS=$'\t' read -r RESUME_RELATIVE RESUME_HASH RESUME_SIZE < "$pointer_path"
  [[ "$RESUME_RELATIVE" == "$PROBE_ROOT"/client-restart-*/restart-upload.bin ]] \
    || fail "Unsafe or unexpected resume probe path: $RESUME_RELATIVE"
  [[ "$RESUME_RELATIVE" != /* && "$RESUME_RELATIVE" != *..* ]] || fail "Unsafe resume probe path"
  [[ "$RESUME_HASH" =~ ^[0-9a-f]{64}$ ]] || fail "Invalid resume probe hash"
  [[ "$RESUME_SIZE" =~ ^[0-9]+$ && "$RESUME_SIZE" -gt 0 ]] || fail "Invalid resume probe size"
}

emit_resume_state() {
  local data_path="$1"
  local relative_path="$2"
  local expected_hash="$3"
  local expected_size="$4"

  if command -v python3 >/dev/null 2>&1; then
    python3 - "$data_path" "$relative_path" "$expected_hash" "$expected_size" <<'PY'
import json
import sys

data_path, relative_path, expected_hash, expected_size_text = sys.argv[1:5]
expected_size = int(expected_size_text)
with open(data_path, "r", encoding="utf-8") as handle:
    data = json.load(handle)

def record(value):
    return value if isinstance(value, dict) else {}

def count(value):
    return len(value) if isinstance(value, dict) else 0

outbox = record(data.get("outbox"))
matches = [
    operation for operation in outbox.values()
    if isinstance(operation, dict)
    and operation.get("kind") == "put"
    and operation.get("path") == relative_path
    and operation.get("hash") == expected_hash
    and operation.get("size") == expected_size
]
chunk_counts = [
    len(operation.get("chunks"))
    for operation in matches
    if operation.get("transport") == "chunks" and isinstance(operation.get("chunks"), list)
]
state = record(data.get("files")).get(relative_path)
tracked = (
    isinstance(state, dict)
    and state.get("deleted") is not True
    and state.get("hash") == expected_hash
    and state.get("size") == expected_size
    and isinstance(state.get("revision"), int)
    and state.get("revision") > 0
)

fields = [
    ("outbox_match", len(matches)),
    ("chunk_count", max(chunk_counts, default=0)),
    ("tracked_match", "true" if tracked else "false"),
    ("tracked_revision", state.get("revision", 0) if isinstance(state, dict) else 0),
    ("outbox_total", count(data.get("outbox"))),
    ("pending_paths", count(data.get("pendingPaths"))),
    ("inbox", count(data.get("inbox"))),
    ("pending_renames", count(data.get("pendingRenames"))),
]
for key, value in fields:
    print(f"{key}\t{value}")
PY
    return
  fi

  [[ -x /usr/bin/osascript ]] || fail "Neither python3 nor macOS osascript is available for resume-state validation"
  /usr/bin/osascript -l JavaScript - "$data_path" "$relative_path" "$expected_hash" "$expected_size" <<'JXA'
ObjC.import("Foundation");

function readText(path) {
  const value = $.NSString.stringWithContentsOfFileEncodingError(path, $.NSUTF8StringEncoding, null);
  if (!value) throw new Error("Cannot read JSON file: " + path);
  return ObjC.unwrap(value);
}

function run(argv) {
  const data = JSON.parse(readText(argv[0]));
  const relativePath = argv[1];
  const expectedHash = argv[2];
  const expectedSize = Number(argv[3]);
  const record = value => value && typeof value === "object" && !Array.isArray(value) ? value : {};
  const count = value => Object.keys(record(value)).length;
  const outbox = record(data.outbox);
  const matches = Object.keys(outbox).map(key => outbox[key]).filter(operation =>
    operation && operation.kind === "put" && operation.path === relativePath
      && operation.hash === expectedHash && operation.size === expectedSize
  );
  const chunkCounts = matches.filter(operation => operation.transport === "chunks" && Array.isArray(operation.chunks))
    .map(operation => operation.chunks.length);
  const state = record(data.files)[relativePath];
  const tracked = Boolean(state && state.deleted !== true && state.hash === expectedHash
    && state.size === expectedSize && Number.isInteger(state.revision) && state.revision > 0);
  const fields = [
    ["outbox_match", matches.length],
    ["chunk_count", chunkCounts.length ? Math.max.apply(null, chunkCounts) : 0],
    ["tracked_match", tracked ? "true" : "false"],
    ["tracked_revision", state && Number.isInteger(state.revision) ? state.revision : 0],
    ["outbox_total", count(data.outbox)],
    ["pending_paths", count(data.pendingPaths)],
    ["inbox", count(data.inbox)],
    ["pending_renames", count(data.pendingRenames)],
  ];
  return fields.map(item => item[0] + "\t" + String(item[1])).join("\n");
}
JXA
}

create_resume_probe() {
  local plugin_directory="$VAULT_PATH/.obsidian/plugins/$PLUGIN_ID"
  local pointer_path="$plugin_directory/macos-resume-probe.txt"
  local timestamp
  local probe_directory
  local probe_file
  local actual_size
  local actual_hash

  (( PROBE_SIZE_MIB >= 16 )) || fail "The client-restart probe must be at least 16 MiB"
  status_client || fail "Client state must be settled before creating the client-restart probe."
  require_command dd
  require_command shasum
  require_command stat

  timestamp="$(date '+%Y%m%d-%H%M%S')"
  RESUME_RELATIVE="$PROBE_ROOT/client-restart-$timestamp/restart-upload.bin"
  probe_directory="$VAULT_PATH/${RESUME_RELATIVE%/*}"
  probe_file="$VAULT_PATH/$RESUME_RELATIVE"
  mkdir -p "$probe_directory"
  dd if=/dev/urandom of="$probe_file" bs=1048576 count="$PROBE_SIZE_MIB" 2>/dev/null
  actual_size="$(file_size_bytes "$probe_file")"
  actual_hash="$(shasum -a 256 "$probe_file" | awk '{print $1}')"
  printf '%s\t%s\t%s\n' "$RESUME_RELATIVE" "$actual_hash" "$actual_size" > "$pointer_path"
  chmod 0600 "$pointer_path"

  info "Created a $PROBE_SIZE_MIB MiB client-restart probe."
  info "In Obsidian, click Sync now and quit Obsidian with Command-Q while the upload is still running."
  info "After Obsidian has closed, run check-resume-interrupted before reopening it."
}

check_resume_interrupted() {
  local plugin_directory="$VAULT_PATH/.obsidian/plugins/$PLUGIN_ID"
  local data_path="$plugin_directory/data.json"
  local probe_file
  local state
  local outbox_match
  local chunk_count
  local tracked_match

  [[ -f "$data_path" ]] || fail "Plugin data is missing: $data_path"
  load_resume_pointer
  probe_file="$VAULT_PATH/$RESUME_RELATIVE"
  [[ -f "$probe_file" ]] || fail "Resume probe file is missing"

  if pgrep -x Obsidian >/dev/null 2>&1; then
    warn "Obsidian still appears to be running. Quit it before accepting the interrupted state."
  fi

  state="$(emit_resume_state "$data_path" "$RESUME_RELATIVE" "$RESUME_HASH" "$RESUME_SIZE")"
  outbox_match="$(summary_value "$state" outbox_match)"
  chunk_count="$(summary_value "$state" chunk_count)"
  tracked_match="$(summary_value "$state" tracked_match)"
  if [[ "$outbox_match" != "1" ]]; then
    if [[ "$tracked_match" == "true" ]]; then
      fail "The upload finished before Obsidian quit. Create a new probe and quit earlier."
    fi
    fail "No resumable put was persisted for the probe. Create a new probe and retry."
  fi
  [[ "$chunk_count" =~ ^[0-9]+$ && "$chunk_count" -gt 1 ]] || fail "The persisted put does not contain a multi-Chunk manifest"

  info "Persisted outbox entry PASS: one Chunk upload with $chunk_count Chunk references."
  printf 'CLIENT_RESTART_INTERRUPTED_STATE_PASS\n'
}

verify_resume_probe() {
  local plugin_directory="$VAULT_PATH/.obsidian/plugins/$PLUGIN_ID"
  local data_path="$plugin_directory/data.json"
  local probe_file
  local actual_hash
  local actual_size
  local state
  local tracked_match
  local tracked_revision
  local report_path="$plugin_directory/macos-resume-verification-report.txt"

  require_command shasum
  require_command stat
  status_client || fail "Persistent queues have not converged after reopening Obsidian."
  load_resume_pointer
  probe_file="$VAULT_PATH/$RESUME_RELATIVE"
  [[ -f "$probe_file" ]] || fail "Resume probe file is missing"
  actual_size="$(file_size_bytes "$probe_file")"
  actual_hash="$(shasum -a 256 "$probe_file" | awk '{print $1}')"
  [[ "$actual_size" == "$RESUME_SIZE" ]] || fail "Resume probe size changed"
  [[ "$actual_hash" == "$RESUME_HASH" ]] || fail "Resume probe hash changed"

  state="$(emit_resume_state "$data_path" "$RESUME_RELATIVE" "$RESUME_HASH" "$RESUME_SIZE")"
  tracked_match="$(summary_value "$state" tracked_match)"
  tracked_revision="$(summary_value "$state" tracked_revision)"
  [[ "$tracked_match" == "true" ]] || fail "Resume probe is not confirmed in client state"
  [[ "$(summary_value "$state" outbox_match)" == "0" ]] || fail "Resume probe still has a persisted outbox entry"

  {
    printf 'result=PASS\n'
    printf 'verified_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'plugin_version=%s\n' "$VERSION"
    printf 'confirmed_revision=%s\n' "$tracked_revision"
    printf 'secrets_included=false\n'
  } > "$report_path"
  chmod 0600 "$report_path"

  info "Resumed upload has matching size, SHA-256 and confirmed revision $tracked_revision."
  info "Safe report written to: $report_path"
  printf 'CLIENT_RESTART_RESUME_PASS\n'
}

pause_for_user() {
  local message="$1"
  [[ -t 0 ]] || fail "The guided command needs an interactive terminal. Use the individual subcommands in automation."
  printf '\n%s\nPress Return to continue: ' "$message"
  read -r _unused
}

run_guided() {
  prepare_client
  if command -v open >/dev/null 2>&1; then
    open -a Obsidian >/dev/null 2>&1 || warn "Could not launch Obsidian automatically"
  fi

  pause_for_user "In Obsidian, open this empty Vault and enable Sync Tunnel. Configure the same Server URL and Vault ID as device A, keep this Mac's unique Device ID, bind the API Token in SecretStorage, keep Automatic sync off, click Test, inspect First sync preview, choose Recommended safe, then click Sync now twice. Do not paste any secret into this terminal."
  while ! status_client; do
    warn "Initial Mac sync is not ready yet. The plugin creates data.json as soon as it is enabled in the target Vault."
    pause_for_user "Return to the same target Vault in Obsidian, enable Sync Tunnel if necessary, finish its configuration and first sync, then come back here. Press Control-C if you want to stop instead."
  done
  create_probe
  pause_for_user "Return to Obsidian. Click Sync now, wait for completion, then click Sync now again and confirm every count is zero."
  while ! status_client; do
    warn "The probe sync has not converged yet."
    pause_for_user "Return to Obsidian, finish both manual syncs, then come back here. Press Control-C if you want to stop instead."
  done
  verify_probe
}

parse_arguments "$@"
prepare_environment

case "$COMMAND" in
  guided) run_guided ;;
  prepare) prepare_client ;;
  status) status_client ;;
  create-probe) create_probe ;;
  verify-probe) verify_probe ;;
  create-resume-probe) create_resume_probe ;;
  check-resume-interrupted) check_resume_interrupted ;;
  verify-resume-probe) verify_resume_probe ;;
esac
