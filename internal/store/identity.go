package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const DefaultDeviceScopes = "sync:read,sync:write,history:read,restore:write"

type Vault struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	QuotaBytes  int64  `json:"quota_bytes"`
	MaxFiles    int64  `json:"max_files"`
	Status      string `json:"status"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type Device struct {
	VaultID         string `json:"vault_id"`
	ID              string `json:"id"`
	Name            string `json:"name"`
	Platform        string `json:"platform"`
	ClientVersion   string `json:"client_version"`
	Status          string `json:"status"`
	RegisteredAt    int64  `json:"registered_at"`
	LastSeenAt      int64  `json:"last_seen_at"`
	LastAckRevision int64  `json:"last_ack_revision"`
	RetiredAt       int64  `json:"retired_at,omitempty"`
	RevokedAt       int64  `json:"revoked_at,omitempty"`
}

type Principal struct {
	TokenID  string
	VaultID  string
	DeviceID string
	Scopes   map[string]bool
}

type PairResult struct {
	Vault  Vault  `json:"vault"`
	Device Device `json:"device"`
	Token  string `json:"token"`
}

type AuditEvent struct {
	ID        int64          `json:"id"`
	EventType string         `json:"event_type"`
	VaultID   string         `json:"vault_id,omitempty"`
	DeviceID  string         `json:"device_id,omitempty"`
	Actor     string         `json:"actor"`
	RequestID string         `json:"request_id,omitempty"`
	Details   map[string]any `json:"details"`
	CreatedAt int64          `json:"created_at"`
}

func (s *Store) CreateVault(ctx context.Context, id, displayName string, quotaBytes, maxFiles int64) (Vault, error) {
	if err := ValidateID("vault ID", id); err != nil {
		return Vault{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = id
	}
	if len(displayName) > 200 || quotaBytes < 0 || maxFiles < 0 {
		return Vault{}, errors.New("invalid vault settings")
	}
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO vaults(id, display_name, quota_bytes, max_files, status, created_at, updated_at)
		VALUES(?, ?, ?, ?, 'active', ?, ?)`, id, displayName, quotaBytes, maxFiles, now, now)
	if err != nil {
		return Vault{}, fmt.Errorf("create vault: %w", err)
	}
	_ = s.RecordAudit(ctx, "vault.created", id, "", "admin", "", map[string]any{"quota_bytes": quotaBytes, "max_files": maxFiles})
	return s.GetVault(ctx, id)
}

func (s *Store) GetVault(ctx context.Context, id string) (Vault, error) {
	var v Vault
	err := s.db.QueryRowContext(ctx, `SELECT id, display_name, quota_bytes, max_files, status, created_at, updated_at FROM vaults WHERE id=?`, id).
		Scan(&v.ID, &v.DisplayName, &v.QuotaBytes, &v.MaxFiles, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func (s *Store) UpdateVault(ctx context.Context, id, displayName, status string, quotaBytes, maxFiles int64) (Vault, error) {
	if err := ValidateID("vault ID", id); err != nil {
		return Vault{}, err
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > 200 || (status != "active" && status != "suspended") || quotaBytes < 0 || maxFiles < 0 {
		return Vault{}, errors.New("invalid vault settings")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE vaults SET display_name=?, status=?, quota_bytes=?, max_files=?, updated_at=? WHERE id=?`, displayName, status, quotaBytes, maxFiles, time.Now().UnixMilli(), id)
	if err != nil {
		return Vault{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return Vault{}, sql.ErrNoRows
	}
	_ = s.RecordAudit(ctx, "vault.updated", id, "", "admin", "", map[string]any{"status": status, "quota_bytes": quotaBytes, "max_files": maxFiles})
	return s.GetVault(ctx, id)
}

func (s *Store) ListVaults(ctx context.Context) ([]Vault, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, display_name, quota_bytes, max_files, status, created_at, updated_at FROM vaults ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Vault
	for rows.Next() {
		var v Vault
		if err := rows.Scan(&v.ID, &v.DisplayName, &v.QuotaBytes, &v.MaxFiles, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (s *Store) CreatePairingCode(ctx context.Context, vaultID string, ttl time.Duration, scopes string) (string, int64, error) {
	if ttl <= 0 || ttl > 24*time.Hour {
		return "", 0, errors.New("pairing TTL must be between 1 second and 24 hours")
	}
	if _, err := s.GetVault(ctx, vaultID); err != nil {
		return "", 0, err
	}
	if scopes == "" {
		scopes = DefaultDeviceScopes
	}
	scopes = normalizeScopes(scopes)
	if !parseScopes(scopes)["sync:read"] {
		return "", 0, errors.New("pairing scopes must include sync:read")
	}
	code, err := randomSecret(24)
	if err != nil {
		return "", 0, err
	}
	now := time.Now().UnixMilli()
	expires := time.Now().Add(ttl).UnixMilli()
	_, err = s.db.ExecContext(ctx, `INSERT INTO pairing_codes(code_hash, vault_id, scopes, expires_at, created_at) VALUES(?, ?, ?, ?, ?)`, secretHash(code), vaultID, scopes, expires, now)
	if err != nil {
		return "", 0, err
	}
	_ = s.RecordAudit(ctx, "pairing.created", vaultID, "", "admin", "", map[string]any{"expires_at": expires})
	return code, expires, nil
}

func (s *Store) PairDevice(ctx context.Context, vaultID, code, name, platform, clientVersion string) (PairResult, error) {
	if err := ValidateID("vault ID", vaultID); err != nil {
		return PairResult{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 200 || len(platform) > 80 || len(clientVersion) > 80 {
		return PairResult{}, errors.New("invalid device metadata")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PairResult{}, err
	}
	defer tx.Rollback()
	var storedVault, scopes string
	var expires int64
	var used sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT vault_id, scopes, expires_at, used_at FROM pairing_codes WHERE code_hash=?`, secretHash(strings.TrimSpace(code))).Scan(&storedVault, &scopes, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) || storedVault != vaultID {
		return PairResult{}, errors.New("invalid pairing code")
	}
	if err != nil {
		return PairResult{}, err
	}
	now := time.Now().UnixMilli()
	if used.Valid || expires < now {
		return PairResult{}, errors.New("pairing code is expired or already used")
	}
	deviceID, err := randomID("device")
	if err != nil {
		return PairResult{}, err
	}
	token, err := randomSecret(32)
	if err != nil {
		return PairResult{}, err
	}
	tokenID, err := randomID("token")
	if err != nil {
		return PairResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO devices(vault_id, id, name, platform, client_version, status, registered_at, last_seen_at)
		VALUES(?, ?, ?, ?, ?, 'active', ?, ?)`, vaultID, deviceID, name, platform, clientVersion, now, now); err != nil {
		return PairResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_tokens(id, token_prefix, token_hash, vault_id, device_id, scopes, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, tokenID, tokenPrefix(token), secretHash(token), vaultID, deviceID, scopes, now); err != nil {
		return PairResult{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE pairing_codes SET used_at=? WHERE code_hash=? AND used_at IS NULL`, now, secretHash(code))
	if err != nil {
		return PairResult{}, err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return PairResult{}, errors.New("pairing code was already used")
	}
	if err := tx.Commit(); err != nil {
		return PairResult{}, err
	}
	vault, err := s.GetVault(ctx, vaultID)
	if err != nil {
		return PairResult{}, err
	}
	device := Device{VaultID: vaultID, ID: deviceID, Name: name, Platform: platform, ClientVersion: clientVersion, Status: "active", RegisteredAt: now, LastSeenAt: now}
	_ = s.RecordAudit(ctx, "device.paired", vaultID, deviceID, "pairing", "", map[string]any{"platform": platform})
	return PairResult{Vault: vault, Device: device, Token: token}, nil
}

func (s *Store) AuthenticateToken(ctx context.Context, token, vaultID, requiredScope string) (Principal, error) {
	if token == "" {
		return Principal{}, sql.ErrNoRows
	}
	var p Principal
	var scopes, status string
	var expires, revoked sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT t.id, t.vault_id, t.device_id, t.scopes, t.expires_at, t.revoked_at, d.status
		FROM auth_tokens t JOIN devices d ON d.vault_id=t.vault_id AND d.id=t.device_id
		WHERE t.token_prefix=? AND t.token_hash=?`, tokenPrefix(token), secretHash(token)).
		Scan(&p.TokenID, &p.VaultID, &p.DeviceID, &scopes, &expires, &revoked, &status)
	if err != nil {
		return Principal{}, err
	}
	if revoked.Valid || status != "active" || (expires.Valid && expires.Int64 < time.Now().UnixMilli()) {
		return Principal{}, errors.New("credential revoked or expired")
	}
	if vaultID != "" && p.VaultID != vaultID {
		return Principal{}, errors.New("credential does not grant access to this vault")
	}
	var vaultStatus string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM vaults WHERE id=?`, p.VaultID).Scan(&vaultStatus); err != nil {
		return Principal{}, err
	}
	if vaultStatus != "active" {
		return Principal{}, errors.New("vault is suspended")
	}
	p.Scopes = parseScopes(scopes)
	if requiredScope != "" && !p.Scopes[requiredScope] {
		return Principal{}, errors.New("credential lacks required scope")
	}
	now := time.Now().UnixMilli()
	_, _ = s.db.ExecContext(ctx, `UPDATE auth_tokens SET last_used_at=? WHERE id=?`, now, p.TokenID)
	_, _ = s.db.ExecContext(ctx, `UPDATE devices SET last_seen_at=? WHERE vault_id=? AND id=?`, now, p.VaultID, p.DeviceID)
	return p, nil
}

func (s *Store) RotateToken(ctx context.Context, principal Principal) (string, error) {
	newToken, err := randomSecret(32)
	if err != nil {
		return "", err
	}
	newID, err := randomID("token")
	if err != nil {
		return "", err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var scopes string
	if err := tx.QueryRowContext(ctx, `SELECT scopes FROM auth_tokens WHERE id=? AND revoked_at IS NULL`, principal.TokenID).Scan(&scopes); err != nil {
		return "", err
	}
	now := time.Now().UnixMilli()
	if _, err := tx.ExecContext(ctx, `INSERT INTO auth_tokens(id, token_prefix, token_hash, vault_id, device_id, scopes, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, newID, tokenPrefix(newToken), secretHash(newToken), principal.VaultID, principal.DeviceID, scopes, now); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE auth_tokens SET revoked_at=? WHERE id=?`, now, principal.TokenID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	_ = s.RecordAudit(ctx, "credential.rotated", principal.VaultID, principal.DeviceID, "device", "", nil)
	return newToken, nil
}

func (s *Store) ListDevices(ctx context.Context, vaultID string) ([]Device, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT vault_id, id, name, platform, client_version, status, registered_at, last_seen_at, last_ack_revision,
		COALESCE(retired_at,0), COALESCE(revoked_at,0) FROM devices WHERE vault_id=? ORDER BY registered_at`, vaultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.VaultID, &d.ID, &d.Name, &d.Platform, &d.ClientVersion, &d.Status, &d.RegisteredAt, &d.LastSeenAt, &d.LastAckRevision, &d.RetiredAt, &d.RevokedAt); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

func (s *Store) SetDeviceStatus(ctx context.Context, vaultID, deviceID, status string) error {
	if status != "retired" && status != "revoked" {
		return errors.New("invalid device status")
	}
	now := time.Now().UnixMilli()
	retired, revoked := any(nil), any(nil)
	if status == "retired" {
		retired = now
	}
	if status == "revoked" {
		revoked = now
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE devices SET status=?, retired_at=?, revoked_at=? WHERE vault_id=? AND id=?`, status, retired, revoked, vaultID, deviceID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return sql.ErrNoRows
	}
	if status == "revoked" || status == "retired" {
		if _, err := tx.ExecContext(ctx, `UPDATE auth_tokens SET revoked_at=? WHERE vault_id=? AND device_id=? AND revoked_at IS NULL`, now, vaultID, deviceID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.RecordAudit(ctx, "device."+status, vaultID, deviceID, "admin", "", nil)
}

func (s *Store) AcknowledgeDevice(ctx context.Context, vaultID, deviceID string, revision int64) error {
	if revision < 0 {
		return errors.New("revision cannot be negative")
	}
	latest, err := s.LatestRevision(ctx, vaultID)
	if err != nil {
		return err
	}
	if revision > latest {
		return errors.New("revision is newer than the vault")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE devices SET last_ack_revision=MAX(last_ack_revision, ?), last_seen_at=?
		WHERE vault_id=? AND id=? AND status='active'`, revision, time.Now().UnixMilli(), vaultID, deviceID)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) RecordAudit(ctx context.Context, eventType, vaultID, deviceID, actor, requestID string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO audit_events(event_type, vault_id, device_id, actor, request_id, details_json, created_at)
		VALUES(?, NULLIF(?,''), NULLIF(?,''), ?, NULLIF(?,''), ?, ?)`, eventType, vaultID, deviceID, actor, requestID, string(encoded), time.Now().UnixMilli())
	return err
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEvent, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("limit must be between 1 and 1000")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, event_type, COALESCE(vault_id,''), COALESCE(device_id,''), actor,
		COALESCE(request_id,''), details_json, created_at FROM audit_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AuditEvent
	for rows.Next() {
		var event AuditEvent
		var details string
		if err := rows.Scan(&event.ID, &event.EventType, &event.VaultID, &event.DeviceID, &event.Actor, &event.RequestID, &details, &event.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(details), &event.Details)
		result = append(result, event)
	}
	return result, rows.Err()
}

func randomSecret(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func randomID(prefix string) (string, error) {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(value), nil
}

func secretHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func tokenPrefix(value string) string {
	if len(value) > 10 {
		return value[:10]
	}
	return value
}

func normalizeScopes(scopes string) string {
	parsed := parseScopes(scopes)
	ordered := []string{"sync:read", "sync:write", "history:read", "restore:write"}
	var result []string
	for _, scope := range ordered {
		if parsed[scope] {
			result = append(result, scope)
		}
	}
	return strings.Join(result, ",")
}

func parseScopes(value string) map[string]bool {
	result := make(map[string]bool)
	for _, scope := range strings.Split(value, ",") {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			result[scope] = true
		}
	}
	return result
}
