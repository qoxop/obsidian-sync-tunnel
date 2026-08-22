package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"obsidian-sync-tunnel/internal/store"
)

type AdminAPI struct {
	store     *store.Store
	tokenHash [32]byte
	logger    *slog.Logger
}

func NewAdmin(db *store.Store, token string, logger *slog.Logger) http.Handler {
	api := &AdminAPI{store: db, tokenHash: sha256.Sum256([]byte(token)), logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.Handle("GET /admin/v1/vaults", api.auth(http.HandlerFunc(api.listVaults)))
	mux.Handle("POST /admin/v1/vaults", api.auth(http.HandlerFunc(api.createVault)))
	mux.Handle("PUT /admin/v1/vaults/{vault}", api.auth(http.HandlerFunc(api.updateVault)))
	mux.Handle("POST /admin/v1/vaults/{vault}/pairing-codes", api.auth(http.HandlerFunc(api.createPairingCode)))
	mux.Handle("GET /admin/v1/vaults/{vault}/devices", api.auth(http.HandlerFunc(api.listDevices)))
	mux.Handle("POST /admin/v1/vaults/{vault}/devices/{device}/status", api.auth(http.HandlerFunc(api.setDeviceStatus)))
	mux.Handle("GET /admin/v1/audit", api.auth(http.HandlerFunc(api.listAudit)))
	mux.Handle("POST /admin/v1/gc/plans", api.auth(http.HandlerFunc(api.planGC)))
	mux.Handle("POST /admin/v1/gc/plans/{plan}/execute", api.auth(http.HandlerFunc(api.executeGC)))
	mux.Handle("GET /admin/v1/doctor", api.auth(http.HandlerFunc(api.doctor)))
	mux.Handle("POST /admin/v1/backups", api.auth(http.HandlerFunc(api.backup)))
	mux.Handle("GET /admin/v1/stats", api.auth(http.HandlerFunc(api.stats)))
	return api.authLog(securityHeaders(mux))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (a *AdminAPI) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value := bearerToken(r)
		provided := sha256.Sum256([]byte(value))
		if value == "" || subtle.ConstantTimeCompare(provided[:], a.tokenHash[:]) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "valid local admin token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	value := r.Header.Get("Authorization")
	if len(value) > len(prefix) && value[:len(prefix)] == prefix {
		return value[len(prefix):]
	}
	return ""
}

func (a *AdminAPI) listVaults(w http.ResponseWriter, r *http.Request) {
	vaults, err := a.store.ListVaults(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"vaults": vaults})
}

func (a *AdminAPI) createVault(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		QuotaBytes  int64  `json:"quota_bytes"`
		MaxFiles    int64  `json:"max_files"`
	}
	if !decodeAdminJSON(w, r, &request) {
		return
	}
	vault, err := a.store.CreateVault(r.Context(), request.ID, request.DisplayName, request.QuotaBytes, request.MaxFiles)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, vault)
}

func (a *AdminAPI) updateVault(w http.ResponseWriter, r *http.Request) {
	var request struct {
		DisplayName string `json:"display_name"`
		Status      string `json:"status"`
		QuotaBytes  int64  `json:"quota_bytes"`
		MaxFiles    int64  `json:"max_files"`
	}
	if !decodeAdminJSON(w, r, &request) {
		return
	}
	vault, err := a.store.UpdateVault(r.Context(), r.PathValue("vault"), request.DisplayName, request.Status, request.QuotaBytes, request.MaxFiles)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, vault)
}

func (a *AdminAPI) createPairingCode(w http.ResponseWriter, r *http.Request) {
	var request struct {
		TTLSeconds int64  `json:"ttl_seconds"`
		Scopes     string `json:"scopes"`
	}
	if !decodeAdminJSON(w, r, &request) {
		return
	}
	if request.TTLSeconds == 0 {
		request.TTLSeconds = 600
	}
	code, expires, err := a.store.CreatePairingCode(r.Context(), r.PathValue("vault"), time.Duration(request.TTLSeconds)*time.Second, request.Scopes)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"code": code, "expires_at": expires})
}

func (a *AdminAPI) listDevices(w http.ResponseWriter, r *http.Request) {
	devices, err := a.store.ListDevices(r.Context(), r.PathValue("vault"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (a *AdminAPI) setDeviceStatus(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Status string `json:"status"`
	}
	if !decodeAdminJSON(w, r, &request) {
		return
	}
	if err := a.store.SetDeviceStatus(r.Context(), r.PathValue("vault"), r.PathValue("device"), request.Status); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminAPI) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, err := parseIntQuery(r, "limit", 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_limit", "limit is invalid")
		return
	}
	events, err := a.store.ListAudit(r.Context(), int(limit))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (a *AdminAPI) planGC(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RetentionDays int `json:"retention_days"`
		KeepVersions  int `json:"keep_versions"`
	}
	if !decodeAdminJSON(w, r, &request) {
		return
	}
	if request.RetentionDays == 0 {
		request.RetentionDays = 90
	}
	if request.KeepVersions == 0 {
		request.KeepVersions = 20
	}
	plan, err := a.store.BuildGCPlan(r.Context(), request.RetentionDays, request.KeepVersions)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

func (a *AdminAPI) executeGC(w http.ResponseWriter, r *http.Request) {
	var request struct {
		PlanHash string `json:"plan_hash"`
	}
	if !decodeAdminJSON(w, r, &request) {
		return
	}
	result, err := a.store.ExecuteGCPlan(r.Context(), r.PathValue("plan"), request.PlanHash)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (a *AdminAPI) doctor(w http.ResponseWriter, r *http.Request) {
	report, err := a.store.Doctor(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (a *AdminAPI) backup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Destination string `json:"destination"`
	}
	if !decodeAdminJSON(w, r, &request) {
		return
	}
	manifest, err := a.store.Backup(r.Context(), request.Destination)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, manifest)
}

func (a *AdminAPI) stats(w http.ResponseWriter, r *http.Request) {
	stats, err := a.store.Stats(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func decodeAdminJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON request")
		return false
	}
	return true
}

func (a *AdminAPI) authLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		a.logger.Info("admin request", "method", r.Method, "route", routeForLog(r.URL.Path), "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}
