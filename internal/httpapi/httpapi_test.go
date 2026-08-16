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
	queryRequest := authorizedRequest(http.MethodGet, "/api/v2/vaults/test/operations/11111111-1111-4111-8111-111111111111", nil)
	queryRequest.Header.Set("X-Device-ID", "test-device")
	queryResponse := httptest.NewRecorder()
	handler.ServeHTTP(queryResponse, queryRequest)
	if queryResponse.Code != http.StatusOK {
		t.Fatalf("operation query status=%d body=%s", queryResponse.Code, queryResponse.Body)
	}
	var queried mutationPayload
	decodeJSON(t, queryResponse.Body.Bytes(), &queried)
	if queried.Change.Revision != first.Change.Revision || !queried.Changed {
		t.Fatalf("unexpected queried operation: %+v", queried)
	}

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
	missingRequest := authorizedRequest(http.MethodGet, "/api/v2/vaults/test/operations/33333333-3333-4333-8333-333333333333", nil)
	missingRequest.Header.Set("X-Device-ID", "test-device")
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing operation status=%d body=%s", missingResponse.Code, missingResponse.Body)
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

func TestChunkUploadManifestCommitAndWholeFileCompatibility(t *testing.T) {
	t.Parallel()
	handler := testHandler(t, 1024)
	data := []byte("chunk content")
	hash := store.Hash(data)

	missingBody, _ := json.Marshal(map[string]any{"hashes": []string{hash}})
	missingResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, authorizedRequest(http.MethodPost, "/api/v2/vaults/test/chunks/missing", bytes.NewReader(missingBody)))
	if missingResponse.Code != http.StatusOK || !bytes.Contains(missingResponse.Body.Bytes(), []byte(hash)) {
		t.Fatalf("missing chunks status=%d body=%s", missingResponse.Code, missingResponse.Body)
	}

	chunkResponse := httptest.NewRecorder()
	handler.ServeHTTP(chunkResponse, authorizedRequest(http.MethodPut, "/api/v2/vaults/test/chunks/"+hash, bytes.NewReader(data)))
	if chunkResponse.Code != http.StatusOK {
		t.Fatalf("put chunk status=%d body=%s", chunkResponse.Code, chunkResponse.Body)
	}

	missingResponse = httptest.NewRecorder()
	handler.ServeHTTP(missingResponse, authorizedRequest(http.MethodPost, "/api/v2/vaults/test/chunks/missing", bytes.NewReader(missingBody)))
	if missingResponse.Code != http.StatusOK || bytes.Contains(missingResponse.Body.Bytes(), []byte(hash)) {
		t.Fatalf("missing chunks after upload status=%d body=%s", missingResponse.Code, missingResponse.Body)
	}

	manifestBody, _ := json.Marshal(map[string]any{
		"size":   len(data),
		"chunks": []store.ChunkRef{{Hash: hash, Size: int64(len(data))}},
	})
	commitRequest := authorizedRequest(http.MethodPost, "/api/v2/vaults/test/files/commit?path=note.md", bytes.NewReader(manifestBody))
	commitRequest.Header.Set("X-Device-ID", "test-device")
	commitRequest.Header.Set("X-Operation-ID", "11111111-1111-4111-8111-111111111111")
	commitRequest.Header.Set("X-Base-Revision", "0")
	commitRequest.Header.Set("X-Modified-At", "1234")
	commitRequest.Header.Set("X-Content-SHA256", hash)
	commitResponse := httptest.NewRecorder()
	handler.ServeHTTP(commitResponse, commitRequest)
	if commitResponse.Code != http.StatusOK {
		t.Fatalf("commit manifest status=%d body=%s", commitResponse.Code, commitResponse.Body)
	}

	chunkDownload := httptest.NewRecorder()
	handler.ServeHTTP(chunkDownload, authorizedRequest(http.MethodGet, "/api/v2/vaults/test/chunks/"+hash, nil))
	if chunkDownload.Code != http.StatusOK || !bytes.Equal(chunkDownload.Body.Bytes(), data) {
		t.Fatalf("get chunk status=%d body=%q", chunkDownload.Code, chunkDownload.Body.Bytes())
	}
	wholeDownload := httptest.NewRecorder()
	handler.ServeHTTP(wholeDownload, authorizedRequest(http.MethodGet, "/api/v1/vaults/test/blobs/"+hash, nil))
	if wholeDownload.Code != http.StatusOK || !bytes.Equal(wholeDownload.Body.Bytes(), data) {
		t.Fatalf("whole-file compatibility status=%d body=%q", wholeDownload.Code, wholeDownload.Body.Bytes())
	}
	manifestResponse := httptest.NewRecorder()
	handler.ServeHTTP(manifestResponse, authorizedRequest(http.MethodGet, "/api/v2/vaults/test/manifests/"+hash, nil))
	if manifestResponse.Code != http.StatusOK || !bytes.Contains(manifestResponse.Body.Bytes(), []byte(hash)) {
		t.Fatalf("get manifest status=%d body=%s", manifestResponse.Code, manifestResponse.Body)
	}
}

func TestRenameEndpoint(t *testing.T) {
	t.Parallel()
	handler := testHandler(t, 1024)
	data := []byte("rename")
	source := mutate(t, handler, http.MethodPut, "old.md", 0, data, store.Hash(data))
	body, _ := json.Marshal(map[string]string{"from": "old.md", "to": "new.md"})
	request := authorizedRequest(http.MethodPost, "/api/v2/vaults/test/rename", bytes.NewReader(body))
	request.Header.Set("X-Device-ID", "test-device")
	request.Header.Set("X-Operation-ID", "11111111-1111-4111-8111-111111111111")
	request.Header.Set("X-Base-Revision", strconv.FormatInt(source.Change.Revision, 10))
	request.Header.Set("X-Modified-At", "1234")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("rename status=%d body=%s", response.Code, response.Body)
	}
	var result struct {
		Change         store.Change   `json:"change"`
		RelatedChanges []store.Change `json:"related_changes"`
		Changed        bool           `json:"changed"`
	}
	decodeJSON(t, response.Body.Bytes(), &result)
	if !result.Changed || result.Change.Path != "new.md" || len(result.RelatedChanges) != 1 || !result.RelatedChanges[0].Deleted {
		t.Fatalf("unexpected rename result: %+v", result)
	}

	operationRequest := authorizedRequest(http.MethodGet, "/api/v2/vaults/test/operations/11111111-1111-4111-8111-111111111111", nil)
	operationRequest.Header.Set("X-Device-ID", "test-device")
	operationResponse := httptest.NewRecorder()
	handler.ServeHTTP(operationResponse, operationRequest)
	if operationResponse.Code != http.StatusOK || !bytes.Contains(operationResponse.Body.Bytes(), []byte("related_changes")) {
		t.Fatalf("rename operation status=%d body=%s", operationResponse.Code, operationResponse.Body)
	}
}

func TestBatchDeleteEndpoint(t *testing.T) {
	t.Parallel()
	handler := testHandler(t, 1024)
	a := mutate(t, handler, http.MethodPut, "a.md", 0, []byte("a"), store.Hash([]byte("a")))
	b := mutate(t, handler, http.MethodPut, "b.md", 0, []byte("b"), store.Hash([]byte("b")))
	body, _ := json.Marshal(map[string]any{"items": []store.BatchDeleteItem{
		{Path: "a.md", BaseRevision: a.Change.Revision, ModifiedAt: 2},
		{Path: "b.md", BaseRevision: b.Change.Revision, ModifiedAt: 2},
	}})
	request := authorizedRequest(http.MethodPost, "/api/v2/vaults/test/batch/delete", bytes.NewReader(body))
	request.Header.Set("X-Device-ID", "test-device")
	request.Header.Set("X-Operation-ID", "11111111-1111-4111-8111-111111111111")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("batch delete status=%d body=%s", response.Code, response.Body)
	}
	var result struct {
		Changes []store.Change `json:"changes"`
		Changed bool           `json:"changed"`
	}
	decodeJSON(t, response.Body.Bytes(), &result)
	if !result.Changed || len(result.Changes) != 2 || !result.Changes[0].Deleted || !result.Changes[1].Deleted {
		t.Fatalf("unexpected batch result: %+v", result)
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
