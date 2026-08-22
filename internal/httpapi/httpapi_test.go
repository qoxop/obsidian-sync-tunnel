package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"obsidian-sync-tunnel/internal/store"
)

type testServer struct {
	t       *testing.T
	db      *store.Store
	handler http.Handler
	token   string
	device  string
}

func TestPairingAuthenticationScopeAndRevocation(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, 1024, 100)

	health := serve(server.handler, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health=%d", health.Code)
	}
	unauthorized := serve(server.handler, httptest.NewRequest(http.MethodGet, "/api/v1/vaults/test/status", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized=%d", unauthorized.Code)
	}

	info := serve(server.handler, server.request(http.MethodGet, "/api/v1/server-info", nil))
	if info.Code != http.StatusOK || !bytes.Contains(info.Body.Bytes(), []byte(`"version":1`)) || !bytes.Contains(info.Body.Bytes(), []byte("scoped-credentials")) {
		t.Fatalf("server info=%d body=%s", info.Code, info.Body)
	}
	wrongVault := serve(server.handler, server.request(http.MethodGet, "/api/v1/vaults/other/status", nil))
	if wrongVault.Code != http.StatusForbidden {
		t.Fatalf("cross-vault=%d body=%s", wrongVault.Code, wrongVault.Body)
	}

	readCode, _, err := server.db.CreatePairingCode(context.Background(), "test", time.Minute, "sync:read")
	if err != nil {
		t.Fatal(err)
	}
	readOnly := pairThroughAPI(t, server.handler, "test", readCode, "reader")
	request := httptest.NewRequest(http.MethodPut, "/api/v1/vaults/test/files/content?path=denied.md", bytes.NewReader([]byte("x")))
	request.Header.Set("Authorization", "Bearer "+readOnly.Token)
	request.Header.Set("X-Base-Revision", "0")
	request.Header.Set("X-Modified-At", "1")
	request.Header.Set("X-Operation-ID", "11111111-1111-4111-8111-111111111111")
	request.Header.Set("X-Content-SHA256", store.Hash([]byte("x")))
	denied := serve(server.handler, request)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("read-only write=%d body=%s", denied.Code, denied.Body)
	}

	if err := server.db.SetDeviceStatus(context.Background(), "test", server.device, "revoked"); err != nil {
		t.Fatal(err)
	}
	revoked := serve(server.handler, server.request(http.MethodGet, "/api/v1/vaults/test/status", nil))
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("revoked=%d body=%s", revoked.Code, revoked.Body)
	}
}

func TestParseLimitQueryRejectsIntegerOverflow(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodGet, "/?limit=9223372036854775808", nil)
	if _, err := parseLimitQuery(request, "limit", 50); err == nil {
		t.Fatal("overflowing limit was accepted")
	}
}

func TestWholeFileOperationsSnapshotAckHistoryAndRestore(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, 1024, 1000)
	first := server.mutate(http.MethodPut, "folder/note.md", 0, []byte("first"), "11111111-1111-4111-8111-111111111111")
	second := server.mutate(http.MethodPut, "folder/note.md", first.Change.Revision, []byte("second"), "22222222-2222-4222-8222-222222222222")

	retryRequest := server.mutationRequest(http.MethodPut, "folder/note.md", 0, []byte("first"), "11111111-1111-4111-8111-111111111111")
	retry := serve(server.handler, retryRequest)
	var retried mutationPayload
	decodeJSON(t, retry.Body.Bytes(), &retried)
	if retry.Code != http.StatusOK || retried.Change.Revision != first.Change.Revision {
		t.Fatalf("retry=%d %+v", retry.Code, retried)
	}

	operation := serve(server.handler, server.request(http.MethodGet, "/api/v1/vaults/test/operations/11111111-1111-4111-8111-111111111111", nil))
	if operation.Code != http.StatusOK {
		t.Fatalf("operation=%d %s", operation.Code, operation.Body)
	}
	snapshot := serve(server.handler, server.request(http.MethodGet, "/api/v1/vaults/test/snapshot?at="+strconv.FormatInt(first.Change.Revision, 10)+"&limit=10", nil))
	if snapshot.Code != http.StatusOK || !bytes.Contains(snapshot.Body.Bytes(), []byte(first.Change.BlobHash)) {
		t.Fatalf("snapshot=%d %s", snapshot.Code, snapshot.Body)
	}

	history := serve(server.handler, server.request(http.MethodGet, "/api/v1/vaults/test/history?path=folder%2Fnote.md&limit=10", nil))
	if history.Code != http.StatusOK {
		t.Fatalf("history=%d %s", history.Code, history.Body)
	}
	var historyPage store.HistoryPage
	decodeJSON(t, history.Body.Bytes(), &historyPage)
	if len(historyPage.Versions) != 2 || historyPage.Versions[0].Revision != second.Change.Revision {
		t.Fatalf("history=%+v", historyPage)
	}

	restoreBody, _ := json.Marshal(map[string]any{"path": "folder/note.md", "source_revision": first.Change.Revision})
	restoreRequest := server.request(http.MethodPost, "/api/v1/vaults/test/restore", bytes.NewReader(restoreBody))
	setMutationHeaders(restoreRequest, second.Change.Revision, "33333333-3333-4333-8333-333333333333", "")
	restored := serve(server.handler, restoreRequest)
	if restored.Code != http.StatusOK {
		t.Fatalf("restore=%d %s", restored.Code, restored.Body)
	}
	var restoredPayload mutationPayload
	decodeJSON(t, restored.Body.Bytes(), &restoredPayload)
	if restoredPayload.Change.RestoredFromRevision != first.Change.Revision || restoredPayload.Change.BlobHash != first.Change.BlobHash {
		t.Fatalf("restored=%+v", restoredPayload)
	}

	ackBody, _ := json.Marshal(map[string]int64{"revision": restoredPayload.Change.Revision})
	ack := serve(server.handler, server.request(http.MethodPost, "/api/v1/vaults/test/ack", bytes.NewReader(ackBody)))
	if ack.Code != http.StatusNoContent {
		t.Fatalf("ack=%d %s", ack.Code, ack.Body)
	}
	devices, err := server.db.ListDevices(context.Background(), "test")
	if err != nil || devices[0].LastAckRevision != restoredPayload.Change.Revision {
		t.Fatalf("devices=%+v err=%v", devices, err)
	}
}

func TestDeletedFileRestoreUsesLastRetainedContent(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, 1024, 1000)
	created := server.mutate(http.MethodPut, "deleted.md", 0, []byte("recover me"), "11111111-1111-4111-8111-111111111111")
	deleted := server.mutate(http.MethodDelete, "deleted.md", created.Change.Revision, nil, "22222222-2222-4222-8222-222222222222")
	body, _ := json.Marshal(map[string]any{"path": "deleted.md", "source_revision": deleted.Change.Revision})
	request := server.request(http.MethodPost, "/api/v1/vaults/test/restore", bytes.NewReader(body))
	setMutationHeaders(request, deleted.Change.Revision, "33333333-3333-4333-8333-333333333333", "")
	response := serve(server.handler, request)
	var result mutationPayload
	decodeJSON(t, response.Body.Bytes(), &result)
	if response.Code != http.StatusOK || result.Change.Deleted || result.Change.RestoredFromRevision != created.Change.Revision || result.Change.BlobHash != created.Change.BlobHash {
		t.Fatalf("deleted restore=%d %+v body=%s", response.Code, result, response.Body)
	}
}

func TestChunkCommitDownloadIsolationRenameAndBatchDelete(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, 1024, 1000)
	data := []byte("chunk content")
	hash := store.Hash(data)
	putChunk := serve(server.handler, server.request(http.MethodPut, "/api/v1/vaults/test/chunks/"+hash, bytes.NewReader(data)))
	if putChunk.Code != http.StatusOK {
		t.Fatalf("put chunk=%d %s", putChunk.Code, putChunk.Body)
	}
	manifestBody, _ := json.Marshal(map[string]any{"size": len(data), "chunks": []store.ChunkRef{{Hash: hash, Size: int64(len(data))}}})
	commit := server.request(http.MethodPost, "/api/v1/vaults/test/files/commit?path=old.md", bytes.NewReader(manifestBody))
	setMutationHeaders(commit, 0, "11111111-1111-4111-8111-111111111111", hash)
	commitResponse := serve(server.handler, commit)
	var committed mutationPayload
	decodeJSON(t, commitResponse.Body.Bytes(), &committed)
	if commitResponse.Code != http.StatusOK {
		t.Fatalf("commit=%d %s", commitResponse.Code, commitResponse.Body)
	}
	download := serve(server.handler, server.request(http.MethodGet, "/api/v1/vaults/test/chunks/"+hash, nil))
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), data) {
		t.Fatalf("download=%d", download.Code)
	}

	if _, err := server.db.CreateVault(context.Background(), "other", "Other", 0, 0); err != nil {
		t.Fatal(err)
	}
	otherCode, _, _ := server.db.CreatePairingCode(context.Background(), "other", time.Minute, store.DefaultDeviceScopes)
	other := pairThroughAPI(t, server.handler, "other", otherCode, "other")
	otherRequest := httptest.NewRequest(http.MethodGet, "/api/v1/vaults/other/chunks/"+hash, nil)
	otherRequest.Header.Set("Authorization", "Bearer "+other.Token)
	isolated := serve(server.handler, otherRequest)
	if isolated.Code != http.StatusNotFound {
		t.Fatalf("cross-vault chunk=%d %s", isolated.Code, isolated.Body)
	}

	renameBody, _ := json.Marshal(map[string]string{"from": "old.md", "to": "new.md"})
	rename := server.request(http.MethodPost, "/api/v1/vaults/test/rename", bytes.NewReader(renameBody))
	setMutationHeaders(rename, committed.Change.Revision, "22222222-2222-4222-8222-222222222222", "")
	renameResponse := serve(server.handler, rename)
	if renameResponse.Code != http.StatusOK {
		t.Fatalf("rename=%d %s", renameResponse.Code, renameResponse.Body)
	}
	var renamed struct {
		Change store.Change `json:"change"`
	}
	decodeJSON(t, renameResponse.Body.Bytes(), &renamed)

	second := server.mutate(http.MethodPut, "second.md", 0, []byte("two"), "33333333-3333-4333-8333-333333333333")
	batchBody, _ := json.Marshal(map[string]any{"items": []store.BatchDeleteItem{
		{Path: "new.md", BaseRevision: renamed.Change.Revision, ModifiedAt: 2},
		{Path: "second.md", BaseRevision: second.Change.Revision, ModifiedAt: 2},
	}})
	batch := server.request(http.MethodPost, "/api/v1/vaults/test/batch/delete", bytes.NewReader(batchBody))
	batch.Header.Set("X-Operation-ID", "44444444-4444-4444-8444-444444444444")
	batchResponse := serve(server.handler, batch)
	if batchResponse.Code != http.StatusOK {
		t.Fatalf("batch=%d %s", batchResponse.Code, batchResponse.Body)
	}
}

func TestLimitsRateAndAdminAPI(t *testing.T) {
	t.Parallel()
	server := newTestServer(t, 4, 2)
	large := serve(server.handler, server.mutationRequest(http.MethodPut, "large.bin", 0, []byte("12345"), "11111111-1111-4111-8111-111111111111"))
	if large.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large=%d %s", large.Code, large.Body)
	}
	_ = serve(server.handler, server.request(http.MethodGet, "/api/v1/server-info", nil))
	rateLimited := serve(server.handler, server.request(http.MethodGet, "/api/v1/server-info", nil))
	if rateLimited.Code != http.StatusTooManyRequests || rateLimited.Header().Get("Retry-After") == "" {
		t.Fatalf("rate=%d %s", rateLimited.Code, rateLimited.Body)
	}

	admin := NewAdmin(server.db, "0123456789abcdefghijklmnopqrstuvwxyz-ADMIN", AdminOptions{AuthRequired: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	unauthorized := serve(admin, newLocalAdminRequest(http.MethodGet, "/admin/v1/vaults", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("admin unauthorized=%d", unauthorized.Code)
	}
	request := newLocalAdminRequest(http.MethodGet, "/admin/v1/stats", nil)
	request.Header.Set("Authorization", "Bearer 0123456789abcdefghijklmnopqrstuvwxyz-ADMIN")
	stats := serve(admin, request)
	if stats.Code != http.StatusOK || !bytes.Contains(stats.Body.Bytes(), []byte(`"vaults":1`)) {
		t.Fatalf("stats=%d %s", stats.Code, stats.Body)
	}
}

func TestAdminVaultAndDeviceLifecycle(t *testing.T) {
	t.Parallel()
	db, err := store.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	const adminToken = "0123456789abcdefghijklmnopqrstuvwxyz-ADMIN"
	admin := NewAdmin(db, adminToken, AdminOptions{AuthRequired: true}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	adminRequest := func(method, target string, body any) *http.Request {
		var reader io.Reader
		if body != nil {
			encoded, encodeErr := json.Marshal(body)
			if encodeErr != nil {
				t.Fatal(encodeErr)
			}
			reader = bytes.NewReader(encoded)
		}
		request := httptest.NewRequest(method, target, reader)
		request.Host = "127.0.0.1:8788"
		request.Header.Set("Authorization", "Bearer "+adminToken)
		return request
	}

	created := serve(admin, adminRequest(http.MethodPost, "/admin/v1/vaults", map[string]any{
		"id": "managed", "display_name": "Managed", "quota_bytes": 2048, "max_files": 10,
	}))
	if created.Code != http.StatusCreated {
		t.Fatalf("create=%d %s", created.Code, created.Body)
	}
	suspended := serve(admin, adminRequest(http.MethodPut, "/admin/v1/vaults/managed", map[string]any{
		"display_name": "Managed", "status": "suspended", "quota_bytes": 2048, "max_files": 10,
	}))
	if suspended.Code != http.StatusOK || !bytes.Contains(suspended.Body.Bytes(), []byte(`"status":"suspended"`)) {
		t.Fatalf("suspend=%d %s", suspended.Code, suspended.Body)
	}
	active := serve(admin, adminRequest(http.MethodPut, "/admin/v1/vaults/managed", map[string]any{
		"display_name": "Managed", "status": "active", "quota_bytes": 4096, "max_files": 20,
	}))
	if active.Code != http.StatusOK {
		t.Fatalf("activate=%d %s", active.Code, active.Body)
	}
	pairing := serve(admin, adminRequest(http.MethodPost, "/admin/v1/vaults/managed/pairing-codes", map[string]any{
		"ttl_seconds": 60, "scopes": store.DefaultDeviceScopes,
	}))
	var pairingResult struct {
		Code string `json:"code"`
	}
	decodeJSON(t, pairing.Body.Bytes(), &pairingResult)
	if pairing.Code != http.StatusCreated || pairingResult.Code == "" {
		t.Fatalf("pairing=%d %s", pairing.Code, pairing.Body)
	}

	public := New(db, Options{MaxFileBytes: 1024, RequestsPerMinute: 1000, BytesPerMinute: 1024 * 1024, Version: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	paired := pairThroughAPI(t, public, "managed", pairingResult.Code, "Managed device")
	devices := serve(admin, adminRequest(http.MethodGet, "/admin/v1/vaults/managed/devices", nil))
	if devices.Code != http.StatusOK || !bytes.Contains(devices.Body.Bytes(), []byte(paired.Device.ID)) {
		t.Fatalf("devices=%d %s", devices.Code, devices.Body)
	}
	revoked := serve(admin, adminRequest(http.MethodPost, "/admin/v1/vaults/managed/devices/"+paired.Device.ID+"/status", map[string]string{"status": "revoked"}))
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke=%d %s", revoked.Code, revoked.Body)
	}
	if _, err := db.AuthenticateToken(context.Background(), paired.Token, "managed", "sync:read"); err == nil {
		t.Fatal("revoked admin-managed device still authenticated")
	}
	audit := serve(admin, adminRequest(http.MethodGet, "/admin/v1/audit?limit=100", nil))
	if audit.Code != http.StatusOK || !bytes.Contains(audit.Body.Bytes(), []byte("device.revoked")) {
		t.Fatalf("audit=%d %s", audit.Code, audit.Body)
	}
}

func newTestServer(t *testing.T, maxFile int64, requestsPerMinute int) *testServer {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.CreateVault(context.Background(), "test", "Test", 0, 0); err != nil {
		t.Fatal(err)
	}
	db.ConfigureLimits(store.ResourceLimits{MaxFileBytes: maxFile})
	handler := New(db, Options{MaxFileBytes: maxFile, RequestsPerMinute: requestsPerMinute, BytesPerMinute: 1024 * 1024, Version: "test"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	code, _, err := db.CreatePairingCode(context.Background(), "test", time.Minute, store.DefaultDeviceScopes)
	if err != nil {
		t.Fatal(err)
	}
	paired := pairThroughAPI(t, handler, "test", code, "test-device")
	return &testServer{t: t, db: db, handler: handler, token: paired.Token, device: paired.Device.ID}
}

func pairThroughAPI(t *testing.T, handler http.Handler, vault, code, name string) store.PairResult {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"vault_id": vault, "code": code, "device_name": name, "platform": "test", "client_version": "1.0.0"})
	response := serve(handler, httptest.NewRequest(http.MethodPost, "/api/v1/pair", bytes.NewReader(body)))
	if response.Code != http.StatusCreated {
		t.Fatalf("pair=%d %s", response.Code, response.Body)
	}
	var result store.PairResult
	decodeJSON(t, response.Body.Bytes(), &result)
	return result
}

func (s *testServer) request(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Authorization", "Bearer "+s.token)
	return request
}

func (s *testServer) mutationRequest(method, path string, baseRevision int64, data []byte, operationID string) *http.Request {
	target := "/api/v1/vaults/test/files/content?path=" + url.QueryEscape(path)
	request := s.request(method, target, bytes.NewReader(data))
	hash := ""
	if method == http.MethodPut {
		hash = store.Hash(data)
	}
	setMutationHeaders(request, baseRevision, operationID, hash)
	return request
}

func (s *testServer) mutate(method, path string, baseRevision int64, data []byte, operationID string) mutationPayload {
	s.t.Helper()
	response := serve(s.handler, s.mutationRequest(method, path, baseRevision, data, operationID))
	if response.Code != http.StatusOK {
		s.t.Fatalf("mutation=%d body=%s", response.Code, response.Body)
	}
	var payload mutationPayload
	decodeJSON(s.t, response.Body.Bytes(), &payload)
	return payload
}

func setMutationHeaders(request *http.Request, baseRevision int64, operationID, hash string) {
	request.Header.Set("X-Base-Revision", strconv.FormatInt(baseRevision, 10))
	request.Header.Set("X-Modified-At", "1234")
	request.Header.Set("X-Operation-ID", operationID)
	if hash != "" {
		request.Header.Set("X-Content-SHA256", hash)
	}
}

func serve(handler http.Handler, request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type mutationPayload struct {
	Change  store.Change `json:"change"`
	Changed bool         `json:"changed"`
}

func decodeJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode JSON %q: %v", data, err)
	}
}
