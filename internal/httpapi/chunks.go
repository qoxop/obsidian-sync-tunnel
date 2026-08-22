package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"obsidian-sync-tunnel/internal/store"
)

func (a *API) missingChunks(w http.ResponseWriter, r *http.Request) {
	if err := store.ValidateID("vault ID", r.PathValue("vault")); err != nil {
		writeStoreError(w, err)
		return
	}
	var request struct {
		Hashes []string `json:"hashes"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 128*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid missing-chunks request")
		return
	}
	missing, err := a.store.MissingChunks(r.Context(), request.Hashes)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"missing": missing})
}

func (a *API) putChunk(w http.ResponseWriter, r *http.Request) {
	if err := store.ValidateID("vault ID", r.PathValue("vault")); err != nil {
		writeStoreError(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, store.ChunkSize)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "chunk_too_large", fmt.Sprintf("chunk exceeds %d bytes", store.ChunkSize))
			return
		}
		writeError(w, http.StatusBadRequest, "read_failed", "could not read chunk body")
		return
	}
	if r.ContentLength <= 0 {
		if allowed, retry := a.limiter.chargeBytes(requestPrincipal(r).TokenID, int64(len(data))); !allowed {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retry))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "device byte limit exceeded")
			return
		}
	}
	changed, err := a.store.PutChunk(r.Context(), r.PathValue("hash"), data)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": changed})
}

func (a *API) getChunk(w http.ResponseWriter, r *http.Request) {
	if err := store.ValidateID("vault ID", r.PathValue("vault")); err != nil {
		writeStoreError(w, err)
		return
	}
	data, err := a.store.GetChunkForVault(r.Context(), r.PathValue("vault"), r.PathValue("hash"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "chunk not found")
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *API) getManifest(w http.ResponseWriter, r *http.Request) {
	size, chunks, err := a.store.GetManifest(r.Context(), r.PathValue("vault"), r.PathValue("hash"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "manifest not found")
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"size": size, "chunks": chunks})
}

func (a *API) commitManifest(w http.ResponseWriter, r *http.Request) {
	if err := store.ValidateID("vault ID", r.PathValue("vault")); err != nil {
		writeStoreError(w, err)
		return
	}
	metadata, ok := parseMutationMetadata(w, r)
	if !ok {
		return
	}
	if metadata.operationID == "" {
		writeError(w, http.StatusBadRequest, "missing_operation", "X-Operation-ID is required for manifest commits")
		return
	}
	var request struct {
		Size   int64            `json:"size"`
		Chunks []store.ChunkRef `json:"chunks"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1024*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid manifest request")
		return
	}
	if request.Size < 1 || request.Size > a.maxFileBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", fmt.Sprintf("file must be between 1 and %d bytes", a.maxFileBytes))
		return
	}
	if err := a.store.CheckCommitAllowed(r.Context(), r.PathValue("vault"), r.URL.Query().Get("path"), request.Size); err != nil {
		writeStoreError(w, err)
		return
	}
	contentHash := r.Header.Get("X-Content-SHA256")
	data, err := a.store.AssembleChunks(r.Context(), contentHash, request.Size, request.Chunks)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	change, changed, err := a.store.PutWithOperation(
		r.Context(),
		r.PathValue("vault"),
		r.URL.Query().Get("path"),
		metadata.deviceID,
		metadata.operationID,
		metadata.baseRevision,
		metadata.modifiedAt,
		contentHash,
		data,
	)
	writeMutationResult(w, change, changed, err)
}
