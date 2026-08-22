package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"obsidian-sync-tunnel/internal/store"
)

func TestAdminUIIsServedOnlyByTheAdminHandler(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ui := filepath.Join(root, "ui")
	if err := os.MkdirAll(filepath.Join(ui, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ui, "index.html"), []byte("<main>Sync Tunnel Admin</main>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ui, "assets", "app.js"), []byte("console.log('admin')"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	admin := NewAdmin(db, strings.Repeat("a", 48), AdminOptions{StaticDirectory: ui, AuthRequired: true}, logger)

	redirect := serve(admin, newLocalAdminRequest(http.MethodGet, "/", nil))
	if redirect.Code != http.StatusTemporaryRedirect || redirect.Header().Get("Location") != "/admin/" {
		t.Fatalf("root redirect=%d location=%q", redirect.Code, redirect.Header().Get("Location"))
	}
	index := serve(admin, newLocalAdminRequest(http.MethodGet, "/admin/", nil))
	if index.Code != http.StatusOK || !bytes.Contains(index.Body.Bytes(), []byte("Sync Tunnel Admin")) {
		t.Fatalf("admin index=%d %s", index.Code, index.Body)
	}
	if !strings.Contains(index.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatal("admin UI is missing the restrictive content security policy")
	}
	spa := serve(admin, newLocalAdminRequest(http.MethodGet, "/admin/vaults", nil))
	if spa.Code != http.StatusOK || !bytes.Contains(spa.Body.Bytes(), []byte("Sync Tunnel Admin")) {
		t.Fatalf("SPA fallback=%d %s", spa.Code, spa.Body)
	}
	asset := serve(admin, newLocalAdminRequest(http.MethodGet, "/admin/assets/app.js", nil))
	if asset.Code != http.StatusOK || !bytes.Contains(asset.Body.Bytes(), []byte("console.log")) {
		t.Fatalf("admin asset=%d %s", asset.Code, asset.Body)
	}
	unauthorized := serve(admin, newLocalAdminRequest(http.MethodGet, "/admin/v1/stats", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("admin API without token=%d", unauthorized.Code)
	}

	public := New(db, Options{MaxFileBytes: 1024, RequestsPerMinute: 100, BytesPerMinute: 1024 * 1024, Version: "test"}, logger)
	publicAdmin := serve(public, httptest.NewRequest(http.MethodGet, "/admin/", nil))
	if publicAdmin.Code != http.StatusNotFound {
		t.Fatalf("public listener exposed admin UI: %d", publicAdmin.Code)
	}
}

func TestManagedBackupLifecycle(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "data", "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	backupRoot := filepath.Join(root, "backups")
	const token = "0123456789abcdefghijklmnopqrstuvwxyz-ADMIN"
	admin := NewAdmin(db, token, AdminOptions{BackupDirectory: backupRoot, AuthRequired: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := func(method, target string, body any) *http.Request {
		var reader io.Reader
		if body != nil {
			encoded, marshalErr := json.Marshal(body)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			reader = bytes.NewReader(encoded)
		}
		r := httptest.NewRequest(method, target, reader)
		r.Host = "127.0.0.1:8788"
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	}

	created := serve(admin, request(http.MethodPost, "/admin/v1/backups", map[string]string{}))
	if created.Code != http.StatusCreated {
		t.Fatalf("create backup=%d %s", created.Code, created.Body)
	}
	var result struct {
		Destination string               `json:"destination"`
		Manifest    store.BackupManifest `json:"manifest"`
	}
	decodeJSON(t, created.Body.Bytes(), &result)
	if !pathWithinDirectory(result.Destination, backupRoot) || result.Manifest.FormatVersion != 1 {
		t.Fatalf("unexpected backup result: %+v", result)
	}

	verified := serve(admin, request(http.MethodPost, "/admin/v1/backups/verify", map[string]string{"destination": result.Destination}))
	if verified.Code != http.StatusOK {
		t.Fatalf("verify backup=%d %s", verified.Code, verified.Body)
	}
	listed := serve(admin, request(http.MethodGet, "/admin/v1/backups", nil))
	var listResult struct {
		Backups []store.BackupRun `json:"backups"`
	}
	decodeJSON(t, listed.Body.Bytes(), &listResult)
	if listed.Code != http.StatusOK || len(listResult.Backups) != 1 || listResult.Backups[0].Destination != result.Destination {
		t.Fatalf("list backups=%d %s", listed.Code, listed.Body)
	}
	outside := serve(admin, request(http.MethodPost, "/admin/v1/backups/verify", map[string]string{"destination": filepath.Join(root, "outside")}))
	if outside.Code != http.StatusBadRequest {
		t.Fatalf("outside backup path=%d %s", outside.Code, outside.Body)
	}
}

func TestAdminAuthenticationCanBeDisabledForLoopbackUse(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	admin := NewAdmin(db, "", AdminOptions{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	session := serve(admin, newLocalAdminRequest(http.MethodGet, "/admin/v1/session", nil))
	if session.Code != http.StatusOK || !bytes.Contains(session.Body.Bytes(), []byte(`"authentication":"none"`)) {
		t.Fatalf("session=%d %s", session.Code, session.Body)
	}
	stats := serve(admin, newLocalAdminRequest(http.MethodGet, "/admin/v1/stats", nil))
	if stats.Code != http.StatusOK {
		t.Fatalf("tokenless stats=%d %s", stats.Code, stats.Body)
	}
	remote := httptest.NewRequest(http.MethodGet, "/admin/v1/stats", nil)
	remote.Host = "admin.example.com"
	if response := serve(admin, remote); response.Code != http.StatusForbidden {
		t.Fatalf("non-loopback Host was accepted: %d", response.Code)
	}
	crossOrigin := newLocalAdminRequest(http.MethodPost, "/admin/v1/backups", strings.NewReader(`{}`))
	crossOrigin.Header.Set("Origin", "https://evil.example")
	if response := serve(admin, crossOrigin); response.Code != http.StatusForbidden {
		t.Fatalf("cross-origin request was accepted: %d", response.Code)
	}
}

func newLocalAdminRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.Host = "127.0.0.1:8788"
	return request
}
