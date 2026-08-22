package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"obsidian-sync-tunnel/internal/store"
)

type Options struct {
	MaxFileBytes      int64
	RequestsPerMinute int
	BytesPerMinute    int64
	Version           string
}

type API struct {
	store        *store.Store
	maxFileBytes int64
	version      string
	logger       *slog.Logger
	limiter      *rateLimiter
	pairLimiter  *rateLimiter
}

type principalContextKey struct{}
type requestIDContextKey struct{}

func New(db *store.Store, options Options, logger *slog.Logger) http.Handler {
	api := &API{
		store: db, maxFileBytes: options.MaxFileBytes, version: options.Version, logger: logger,
		limiter:     newRateLimiter(options.RequestsPerMinute, options.BytesPerMinute),
		pairLimiter: newRateLimiter(20, 1024*1024),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", api.health)
	mux.HandleFunc("POST /api/v1/pair", api.pair)
	mux.Handle("GET /api/v1/server-info", api.authenticate("sync:read", http.HandlerFunc(api.serverInfo)))
	mux.Handle("GET /api/v1/vaults/{vault}", api.authenticate("sync:read", http.HandlerFunc(api.vaultInfo)))
	mux.Handle("GET /api/v1/vaults/{vault}/status", api.authenticate("sync:read", http.HandlerFunc(api.status)))
	mux.Handle("GET /api/v1/vaults/{vault}/changes", api.authenticate("sync:read", http.HandlerFunc(api.changes)))
	mux.Handle("GET /api/v1/vaults/{vault}/snapshot", api.authenticate("sync:read", http.HandlerFunc(api.snapshot)))
	mux.Handle("GET /api/v1/vaults/{vault}/operations/{operation}", api.authenticate("sync:read", http.HandlerFunc(api.operation)))
	mux.Handle("POST /api/v1/vaults/{vault}/chunks/missing", api.authenticate("sync:write", http.HandlerFunc(api.missingChunks)))
	mux.Handle("PUT /api/v1/vaults/{vault}/chunks/{hash}", api.authenticate("sync:write", http.HandlerFunc(api.putChunk)))
	mux.Handle("GET /api/v1/vaults/{vault}/chunks/{hash}", api.authenticate("sync:read", http.HandlerFunc(api.getChunk)))
	mux.Handle("GET /api/v1/vaults/{vault}/manifests/{hash}", api.authenticate("sync:read", http.HandlerFunc(api.getManifest)))
	mux.Handle("GET /api/v1/vaults/{vault}/blobs/{hash}", api.authenticate("sync:read", http.HandlerFunc(api.blob)))
	mux.Handle("POST /api/v1/vaults/{vault}/files/commit", api.authenticate("sync:write", http.HandlerFunc(api.commitManifest)))
	mux.Handle("PUT /api/v1/vaults/{vault}/files/content", api.authenticate("sync:write", http.HandlerFunc(api.putFile)))
	mux.Handle("DELETE /api/v1/vaults/{vault}/files/content", api.authenticate("sync:write", http.HandlerFunc(api.deleteFile)))
	mux.Handle("POST /api/v1/vaults/{vault}/rename", api.authenticate("sync:write", http.HandlerFunc(api.renameFile)))
	mux.Handle("POST /api/v1/vaults/{vault}/batch/delete", api.authenticate("sync:write", http.HandlerFunc(api.batchDelete)))
	mux.Handle("POST /api/v1/vaults/{vault}/ack", api.authenticate("sync:read", http.HandlerFunc(api.acknowledge)))
	mux.Handle("GET /api/v1/vaults/{vault}/history", api.authenticate("history:read", http.HandlerFunc(api.history)))
	mux.Handle("POST /api/v1/vaults/{vault}/restore", api.authenticate("restore:write", http.HandlerFunc(api.restore)))
	mux.Handle("POST /api/v1/vaults/{vault}/credential/rotate", api.authenticate("sync:read", http.HandlerFunc(api.rotateCredential)))
	return api.securityHeaders(api.requestID(api.accessLog(mux)))
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": a.version})
}

func (a *API) pair(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host == "" {
		host = r.RemoteAddr
	}
	if allowed, retry := a.pairLimiter.allow("pair:"+host, requestBodySize(r)); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "pairing request limit exceeded")
		return
	}
	var request struct {
		VaultID       string `json:"vault_id"`
		Code          string `json:"code"`
		DeviceName    string `json:"device_name"`
		Platform      string `json:"platform"`
		ClientVersion string `json:"client_version"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid pairing request")
		return
	}
	result, err := a.store.PairDevice(r.Context(), request.VaultID, request.Code, request.DeviceName, request.Platform, request.ClientVersion)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "pairing_failed", "pairing code is invalid, expired, or already used")
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (a *API) serverInfo(w http.ResponseWriter, r *http.Request) {
	schemaVersion, err := a.store.SchemaVersion(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_version": a.version,
		"protocol":       map[string]int{"version": 1},
		"capabilities": []string{
			"snapshot", "idempotent-operations", "whole-file", "chunk-transfer", "rename", "batch-delete",
			"device-pairing", "device-ack", "history", "restore", "scoped-credentials",
		},
		"database": map[string]int{"schema_version": schemaVersion},
		"limits": map[string]any{
			"max_file_bytes": a.maxFileBytes, "max_page_size": 1000, "chunk_size": store.ChunkSize,
			"max_chunk_query": 1000, "chunk_concurrency": 3,
		},
	})
}

func (a *API) vaultInfo(w http.ResponseWriter, r *http.Request) {
	vault, err := a.store.GetVault(r.Context(), r.PathValue("vault"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "vault not found")
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, vault)
}

func (a *API) status(w http.ResponseWriter, r *http.Request) {
	revision, err := a.store.LatestRevision(r.Context(), r.PathValue("vault"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"latest_revision": revision, "max_file_bytes": a.maxFileBytes})
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

func (a *API) snapshot(w http.ResponseWriter, r *http.Request) {
	latest, err := a.store.LatestRevision(r.Context(), r.PathValue("vault"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	at := latest
	if value := r.URL.Query().Get("at"); value != "" {
		at, err = strconv.ParseInt(value, 10, 64)
		if err != nil || at < 0 || at > latest {
			writeError(w, http.StatusBadRequest, "invalid_snapshot_revision", "at must be a revision returned by this vault")
			return
		}
	}
	limit64, err := parseIntQuery(r, "limit", 250)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	after := r.URL.Query().Get("after")
	files, hasMore, err := a.store.ListSnapshot(r.Context(), r.PathValue("vault"), at, after, int(limit64))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	cursor := after
	if len(files) > 0 {
		cursor = files[len(files)-1].Path
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "snapshot_revision": at, "cursor": cursor, "has_more": hasMore})
}

func (a *API) operation(w http.ResponseWriter, r *http.Request) {
	principal := requestPrincipal(r)
	change, related, changed, found, err := a.store.GetOperationDetails(r.Context(), r.PathValue("vault"), principal.DeviceID, r.PathValue("operation"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "operation not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"change": change, "related_changes": related, "changed": changed})
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
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *API) putFile(w http.ResponseWriter, r *http.Request) {
	metadata, ok := parseMutationMetadata(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, a.maxFileBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", fmt.Sprintf("file exceeds %d bytes", a.maxFileBytes))
			return
		}
		writeError(w, http.StatusBadRequest, "read_failed", "could not read request body")
		return
	}
	if r.ContentLength <= 0 {
		if allowed, retry := a.limiter.chargeBytes(requestPrincipal(r).TokenID, int64(len(data))); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "device byte limit exceeded")
			return
		}
	}
	change, changed, err := a.store.PutWithOperation(r.Context(), r.PathValue("vault"), r.URL.Query().Get("path"), metadata.deviceID, metadata.operationID, metadata.baseRevision, metadata.modifiedAt, r.Header.Get("X-Content-SHA256"), data)
	writeMutationResult(w, change, changed, err)
}

func (a *API) deleteFile(w http.ResponseWriter, r *http.Request) {
	metadata, ok := parseMutationMetadata(w, r)
	if !ok {
		return
	}
	change, changed, err := a.store.DeleteWithOperation(r.Context(), r.PathValue("vault"), r.URL.Query().Get("path"), metadata.deviceID, metadata.operationID, metadata.baseRevision, metadata.modifiedAt)
	writeMutationResult(w, change, changed, err)
}

func (a *API) acknowledge(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Revision int64 `json:"revision"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid acknowledgement")
		return
	}
	principal := requestPrincipal(r)
	if err := a.store.AcknowledgeDevice(r.Context(), r.PathValue("vault"), principal.DeviceID, request.Revision); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	before, err := parseIntQuery(r, "before", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_cursor", "before must be a non-negative revision")
		return
	}
	limit, err := parseIntQuery(r, "limit", 50)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", "limit is invalid")
		return
	}
	page, err := a.store.ListHistory(r.Context(), r.PathValue("vault"), r.URL.Query().Get("path"), before, int(limit), r.URL.Query().Get("deleted") == "true")
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (a *API) restore(w http.ResponseWriter, r *http.Request) {
	metadata, ok := parseMutationMetadata(w, r)
	if !ok {
		return
	}
	var request struct {
		Path           string `json:"path"`
		SourceRevision int64  `json:"source_revision"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid restore request")
		return
	}
	change, changed, err := a.store.RestoreVersion(r.Context(), r.PathValue("vault"), request.Path, metadata.deviceID, metadata.operationID, request.SourceRevision, metadata.baseRevision, metadata.modifiedAt)
	writeMutationResult(w, change, changed, err)
}

func (a *API) rotateCredential(w http.ResponseWriter, r *http.Request) {
	token, err := a.store.RotateToken(r.Context(), requestPrincipal(r))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

type mutationMetadata struct {
	deviceID     string
	operationID  string
	baseRevision int64
	modifiedAt   int64
}

func parseMutationMetadata(w http.ResponseWriter, r *http.Request) (mutationMetadata, bool) {
	principal := requestPrincipal(r)
	if principal.DeviceID == "" {
		writeError(w, http.StatusUnauthorized, "unpaired_device", "paired device credential required")
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
	return mutationMetadata{deviceID: principal.DeviceID, operationID: r.Header.Get("X-Operation-ID"), baseRevision: baseRevision, modifiedAt: modifiedAt}, true
}

func writeMutationResult(w http.ResponseWriter, change store.Change, changed bool, err error) {
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		writeJSON(w, http.StatusConflict, map[string]any{"error": map[string]string{"code": "revision_conflict", "message": conflict.Error()}, "current": conflict.Current})
		return
	}
	var reused *store.OperationReuseError
	if errors.As(err, &reused) {
		writeError(w, http.StatusBadRequest, "operation_id_reused", reused.Error())
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"change": change, "changed": changed})
}

func (a *API) authenticate(scope string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Fields(r.Header.Get("Authorization"))
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, http.StatusUnauthorized, "unauthorized", "paired device credential required")
			return
		}
		principal, err := a.store.AuthenticateToken(r.Context(), parts[1], r.PathValue("vault"), scope)
		if err != nil {
			status, code := http.StatusUnauthorized, "unauthorized"
			if strings.Contains(err.Error(), "scope") || strings.Contains(err.Error(), "vault") {
				status, code = http.StatusForbidden, "forbidden"
			}
			writeError(w, status, code, "credential is invalid or does not permit this operation")
			return
		}
		if allowed, retry := a.limiter.allow(principal.TokenID, requestBodySize(r)); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "device request or byte limit exceeded")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalContextKey{}, principal)))
	})
}

func requestPrincipal(r *http.Request) store.Principal {
	value, _ := r.Context().Value(principalContextKey{}).(store.Principal)
	return value
}

func requestBodySize(r *http.Request) int64 {
	if r.ContentLength > 0 {
		return r.ContentLength
	}
	return 0
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
		a.logger.Info("http request", "request_id", requestIDFrom(r), "method", r.Method, "route", routeForLog(r.URL.Path), "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

func (a *API) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := make([]byte, 12)
		_, _ = rand.Read(value)
		requestID := hex.EncodeToString(value)
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey{}, requestID)))
	})
}

func requestIDFrom(r *http.Request) string {
	value, _ := r.Context().Value(requestIDContextKey{}).(string)
	return value
}

func routeForLog(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for index, part := range parts {
		if index > 1 && (parts[index-1] == "vaults" || parts[index-1] == "chunks" || parts[index-1] == "operations") {
			parts[index] = ":id"
		} else if len(part) == 64 {
			parts[index] = ":hash"
		}
	}
	return "/" + strings.Join(parts, "/")
}

func parseIntQuery(r *http.Request, key string, fallback int64) (int64, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func writeStoreError(w http.ResponseWriter, err error) {
	var resource *store.ResourceLimitError
	if errors.As(err, &resource) {
		status := http.StatusRequestEntityTooLarge
		if resource.Code == "insufficient_storage" {
			status = http.StatusInsufficientStorage
		}
		writeError(w, status, resource.Code, resource.Message)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "invalid") || strings.Contains(message, "cannot be negative") || strings.Contains(message, "must be") || strings.Contains(message, "too long") || strings.Contains(message, "does not match") {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSONStatus(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) { writeJSONStatus(w, status, value) }

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
