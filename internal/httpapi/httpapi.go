package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"obsidian-sync-tunnel/internal/store"
)

type API struct {
	store          *store.Store
	tokenHash      [32]byte
	maxUploadBytes int64
	version        string
	logger         *slog.Logger
}

func New(db *store.Store, token string, maxUploadBytes int64, version string, logger *slog.Logger) http.Handler {
	api := &API{store: db, tokenHash: sha256.Sum256([]byte(token)), maxUploadBytes: maxUploadBytes, version: version, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("GET /api/v1/vaults/{vault}/status", api.status)
	mux.HandleFunc("GET /api/v1/vaults/{vault}/changes", api.changes)
	mux.HandleFunc("GET /api/v1/vaults/{vault}/blobs/{hash}", api.blob)
	mux.HandleFunc("PUT /api/v1/vaults/{vault}/file", api.putFile)
	mux.HandleFunc("DELETE /api/v1/vaults/{vault}/file", api.deleteFile)
	return api.securityHeaders(api.accessLog(api.authenticate(mux)))
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": a.version})
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	revision, err := a.store.LatestRevision(r.Context(), r.PathValue("vault"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"latest_revision": revision, "max_upload_bytes": a.maxUploadBytes})
}

func (a *API) changes(w http.ResponseWriter, r *http.Request) {
	after, err := parseIntQuery(r, "after", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cursor", err.Error())
		return
	}
	limit64, err := parseIntQuery(r, "limit", 250)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	changes, hasMore, err := a.store.ListChanges(r.Context(), r.PathValue("vault"), after, int(limit64))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	cursor := after
	if len(changes) > 0 {
		cursor = changes[len(changes)-1].Revision
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": changes, "cursor": cursor, "has_more": hasMore})
}

func (a *API) blob(w http.ResponseWriter, r *http.Request) {
	data, err := a.store.GetBlob(r.Context(), r.PathValue("vault"), r.PathValue("hash"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "blob not found")
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *API) putFile(w http.ResponseWriter, r *http.Request) {
	metadata, ok := parseMutationMetadata(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.maxUploadBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", fmt.Sprintf("file exceeds %d bytes", a.maxUploadBytes))
			return
		}
		writeError(w, http.StatusBadRequest, "read_failed", "could not read request body")
		return
	}
	change, changed, err := a.store.Put(r.Context(), r.PathValue("vault"), r.URL.Query().Get("path"), metadata.deviceID, metadata.baseRevision, metadata.modifiedAt, r.Header.Get("X-Content-SHA256"), data)
	writeMutationResult(w, change, changed, err)
}

func (a *API) deleteFile(w http.ResponseWriter, r *http.Request) {
	metadata, ok := parseMutationMetadata(w, r)
	if !ok {
		return
	}
	change, changed, err := a.store.Delete(r.Context(), r.PathValue("vault"), r.URL.Query().Get("path"), metadata.deviceID, metadata.baseRevision, metadata.modifiedAt)
	writeMutationResult(w, change, changed, err)
}

type mutationMetadata struct {
	deviceID     string
	baseRevision int64
	modifiedAt   int64
}

func parseMutationMetadata(w http.ResponseWriter, r *http.Request) (mutationMetadata, bool) {
	deviceID := r.Header.Get("X-Device-ID")
	if deviceID == "" {
		writeError(w, http.StatusBadRequest, "missing_device", "X-Device-ID is required")
		return mutationMetadata{}, false
	}
	baseRevision, err := strconv.ParseInt(r.Header.Get("X-Base-Revision"), 10, 64)
	if err != nil || baseRevision < 0 {
		writeError(w, http.StatusBadRequest, "invalid_base_revision", "X-Base-Revision must be a non-negative integer")
		return mutationMetadata{}, false
	}
	modifiedAt := time.Now().UnixMilli()
	if value := r.Header.Get("X-Modified-At"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			writeError(w, http.StatusBadRequest, "invalid_modified_at", "X-Modified-At must be a non-negative integer")
			return mutationMetadata{}, false
		}
		modifiedAt = parsed
	}
	return mutationMetadata{deviceID: deviceID, baseRevision: baseRevision, modifiedAt: modifiedAt}, true
}

func writeMutationResult(w http.ResponseWriter, change store.Change, changed bool, err error) {
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   map[string]string{"code": "revision_conflict", "message": conflict.Error()},
			"current": conflict.Current,
		})
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"change": change, "changed": changed})
}

func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		parts := strings.Fields(r.Header.Get("Authorization"))
		value := ""
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			value = parts[1]
		}
		provided := sha256.Sum256([]byte(value))
		if value == "" || subtle.ConstantTimeCompare(provided[:], a.tokenHash[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "valid bearer token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (a *API) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		a.logger.Info("http request", "method", r.Method, "path", r.URL.Path, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func parseIntQuery(r *http.Request, key string, fallback int64) (int64, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func writeStoreError(w http.ResponseWriter, err error) {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "invalid") || strings.Contains(message, "cannot be negative") || strings.Contains(message, "must be between") || strings.Contains(message, "too long") || strings.Contains(message, "does not match") {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSONStatus(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	writeJSONStatus(w, status, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
