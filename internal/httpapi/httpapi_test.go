package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"

	"obsidian-sync-tunnel/internal/store"
)

const testToken = "0123456789abcdefghijklmnopqrstuvwxyz-TEST-TOKEN"

func TestHealthAndAuthentication(t *testing.T) {
	t.Parallel()
	handler := testHandler(t, 1024)

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status: %d", health.Code)
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/vaults/test/status", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status: %d", unauthorized.Code)
	}
}

func TestUploadDownloadDeleteAndConflict(t *testing.T) {
	t.Parallel()
	handler := testHandler(t, 1024)
	path := "folder/note.md"
	firstData := []byte("first")
	first := mutate(t, handler, http.MethodPut, path, 0, firstData, store.Hash(firstData))
	if first.Change.Revision <= 0 || !first.Changed {
		t.Fatalf("unexpected first mutation: %+v", first)
	}

	changesRequest := authorizedRequest(http.MethodGet, "/api/v1/vaults/test/changes?after=0&limit=10", nil)
	changesResponse := httptest.NewRecorder()
	handler.ServeHTTP(changesResponse, changesRequest)
	if changesResponse.Code != http.StatusOK {
		t.Fatalf("changes status: %d body=%s", changesResponse.Code, changesResponse.Body)
	}
	var page struct {
		Changes []store.Change `json:"changes"`
		Cursor  int64          `json:"cursor"`
	}
	decodeJSON(t, changesResponse.Body.Bytes(), &page)
	if len(page.Changes) != 1 || page.Cursor != first.Change.Revision {
		t.Fatalf("unexpected changes: %+v", page)
	}

	blobRequest := authorizedRequest(http.MethodGet, "/api/v1/vaults/test/blobs/"+first.Change.BlobHash, nil)
	blobResponse := httptest.NewRecorder()
	handler.ServeHTTP(blobResponse, blobRequest)
	if blobResponse.Code != http.StatusOK || !bytes.Equal(blobResponse.Body.Bytes(), firstData) {
		t.Fatalf("blob status=%d body=%q", blobResponse.Code, blobResponse.Body.Bytes())
	}

	conflictData := []byte("conflict")
	conflictRequest := fileRequest(http.MethodPut, path, 0, conflictData)
	conflictRequest.Header.Set("X-Content-SHA256", store.Hash(conflictData))
	conflictResponse := httptest.NewRecorder()
	handler.ServeHTTP(conflictResponse, conflictRequest)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("conflict status=%d body=%s", conflictResponse.Code, conflictResponse.Body)
	}

	deleted := mutate(t, handler, http.MethodDelete, path, first.Change.Revision, nil, "")
	if !deleted.Changed || !deleted.Change.Deleted {
		t.Fatalf("unexpected delete: %+v", deleted)
	}
}

func TestUploadValidationAndLimit(t *testing.T) {
	t.Parallel()
	handler := testHandler(t, 4)

	tooLarge := fileRequest(http.MethodPut, "large.bin", 0, []byte("12345"))
	tooLarge.Header.Set("X-Content-SHA256", store.Hash([]byte("12345")))
	tooLargeResponse := httptest.NewRecorder()
	handler.ServeHTTP(tooLargeResponse, tooLarge)
	if tooLargeResponse.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large upload status=%d body=%s", tooLargeResponse.Code, tooLargeResponse.Body)
	}

	badHash := fileRequest(http.MethodPut, "note.md", 0, []byte("data"))
	badHash.Header.Set("X-Content-SHA256", store.Hash([]byte("other")))
	badHashResponse := httptest.NewRecorder()
	handler.ServeHTTP(badHashResponse, badHash)
	if badHashResponse.Code != http.StatusBadRequest {
		t.Fatalf("hash status=%d body=%s", badHashResponse.Code, badHashResponse.Body)
	}
}

type mutationPayload struct {
	Change  store.Change `json:"change"`
	Changed bool         `json:"changed"`
}

func mutate(t *testing.T, handler http.Handler, method, path string, baseRevision int64, data []byte, hash string) mutationPayload {
	t.Helper()
	request := fileRequest(method, path, baseRevision, data)
	if hash != "" {
		request.Header.Set("X-Content-SHA256", hash)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("mutation status=%d body=%s", response.Code, response.Body)
	}
	var payload mutationPayload
	decodeJSON(t, response.Body.Bytes(), &payload)
	return payload
}

func fileRequest(method, path string, baseRevision int64, data []byte) *http.Request {
	request := authorizedRequest(method, "/api/v1/vaults/test/file?path="+url.QueryEscape(path), bytes.NewReader(data))
	request.Header.Set("X-Device-ID", "test-device")
	request.Header.Set("X-Base-Revision", strconv.FormatInt(baseRevision, 10))
	request.Header.Set("X-Modified-At", "1234")
	return request
}

func authorizedRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.Header.Set("Authorization", "Bearer "+testToken)
	return request
}

func testHandler(t *testing.T, maxUpload int64) http.Handler {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db, testToken, maxUpload, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func decodeJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode JSON %q: %v", data, err)
	}
}
