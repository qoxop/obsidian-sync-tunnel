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

func TestOperationIDRetryReturnsOriginalRevision(t *testing.T) {
	t.Parallel()
	handler := testHandler(t, 1024)
	path := "note.md"
	firstData := []byte("first")
	firstRequest := fileRequest(http.MethodPut, path, 0, firstData)
	firstRequest.Header.Set("X-Content-SHA256", store.Hash(firstData))
	firstRequest.Header.Set("X-Operation-ID", "11111111-1111-4111-8111-111111111111")
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, firstRequest)
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first operation status=%d body=%s", firstResponse.Code, firstResponse.Body)
	}
	var first mutationPayload
	decodeJSON(t, firstResponse.Body.Bytes(), &first)

	secondData := []byte("second")
	secondRequest := fileRequest(http.MethodPut, path, first.Change.Revision, secondData)
	secondRequest.Header.Set("X-Content-SHA256", store.Hash(secondData))
	secondRequest.Header.Set("X-Operation-ID", "22222222-2222-4222-8222-222222222222")
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, secondRequest)
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("second operation status=%d body=%s", secondResponse.Code, secondResponse.Body)
	}

	retryRequest := fileRequest(http.MethodPut, path, 0, firstData)
	retryRequest.Header.Set("X-Content-SHA256", store.Hash(firstData))
	retryRequest.Header.Set("X-Operation-ID", "11111111-1111-4111-8111-111111111111")
	retryResponse := httptest.NewRecorder()
	handler.ServeHTTP(retryResponse, retryRequest)
	if retryResponse.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", retryResponse.Code, retryResponse.Body)
	}
	var retried mutationPayload
	decodeJSON(t, retryResponse.Body.Bytes(), &retried)
	if retried.Change.Revision != first.Change.Revision || !retried.Changed {
		t.Fatalf("retry did not return original result: first=%+v retried=%+v", first, retried)
	}

	reusedRequest := fileRequest(http.MethodPut, path, 0, firstData)
	reusedRequest.Header.Set("X-Content-SHA256", store.Hash(firstData))
	reusedRequest.Header.Set("X-Modified-At", "9999")
	reusedRequest.Header.Set("X-Operation-ID", "11111111-1111-4111-8111-111111111111")
	reusedResponse := httptest.NewRecorder()
	handler.ServeHTTP(reusedResponse, reusedRequest)
	if reusedResponse.Code != http.StatusBadRequest || !bytes.Contains(reusedResponse.Body.Bytes(), []byte("operation_id_reused")) {
		t.Fatalf("reuse status=%d body=%s", reusedResponse.Code, reusedResponse.Body)
	}
}

func TestServerInfoAndStableSnapshot(t *testing.T) {
	t.Parallel()
	handler := testHandler(t, 1024)

	infoResponse := httptest.NewRecorder()
	handler.ServeHTTP(infoResponse, authorizedRequest(http.MethodGet, "/api/v2/server-info", nil))
	if infoResponse.Code != http.StatusOK {
		t.Fatalf("server info status=%d body=%s", infoResponse.Code, infoResponse.Body)
	}
	var info struct {
		ServerVersion string   `json:"server_version"`
		Capabilities  []string `json:"capabilities"`
		Protocol      struct {
			Min int `json:"min"`
			Max int `json:"max"`
		} `json:"protocol"`
	}
	decodeJSON(t, infoResponse.Body.Bytes(), &info)
	if info.ServerVersion != "test" || info.Protocol.Min != 1 || info.Protocol.Max != 2 || len(info.Capabilities) == 0 {
		t.Fatalf("unexpected server info: %+v", info)
	}

	a1Data := []byte("a1")
	a1 := mutate(t, handler, http.MethodPut, "a.md", 0, a1Data, store.Hash(a1Data))
	bData := []byte("b")
	b := mutate(t, handler, http.MethodPut, "b.md", 0, bData, store.Hash(bData))
	a2Data := []byte("a2")
	_ = mutate(t, handler, http.MethodPut, "a.md", a1.Change.Revision, a2Data, store.Hash(a2Data))

	firstTarget := "/api/v2/vaults/test/snapshot?at=" + strconv.FormatInt(b.Change.Revision, 10) + "&limit=1"
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, authorizedRequest(http.MethodGet, firstTarget, nil))
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first snapshot status=%d body=%s", firstResponse.Code, firstResponse.Body)
	}
	var firstPage struct {
		Files            []store.Change `json:"files"`
		SnapshotRevision int64          `json:"snapshot_revision"`
		Cursor           string         `json:"cursor"`
		HasMore          bool           `json:"has_more"`
	}
	decodeJSON(t, firstResponse.Body.Bytes(), &firstPage)
	if len(firstPage.Files) != 1 || firstPage.Files[0].BlobHash != store.Hash(a1Data) || !firstPage.HasMore {
		t.Fatalf("unexpected first snapshot page: %+v", firstPage)
	}

	secondTarget := firstTarget + "&after=" + url.QueryEscape(firstPage.Cursor)
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, authorizedRequest(http.MethodGet, secondTarget, nil))
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("second snapshot status=%d body=%s", secondResponse.Code, secondResponse.Body)
	}
	var secondPage struct {
		Files   []store.Change `json:"files"`
		HasMore bool           `json:"has_more"`
	}
	decodeJSON(t, secondResponse.Body.Bytes(), &secondPage)
	if len(secondPage.Files) != 1 || secondPage.Files[0].Path != "b.md" || secondPage.HasMore {
		t.Fatalf("unexpected second snapshot page: %+v", secondPage)
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
