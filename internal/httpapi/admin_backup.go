package httpapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"obsidian-sync-tunnel/internal/store"
)

func (a *AdminAPI) listBackups(w http.ResponseWriter, r *http.Request) {
	limit, err := parseIntQuery(r, "limit", 50)
	if err != nil || limit < 1 || limit > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 1000")
		return
	}
	runs, err := a.store.ListBackupRuns(r.Context(), int(limit))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": runs})
}

func (a *AdminAPI) verifyBackup(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Destination string `json:"destination"`
	}
	if !decodeAdminJSON(w, r, &request) {
		return
	}
	destination, err := a.resolveBackupDestination(request.Destination, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_backup_destination", err.Error())
		return
	}
	manifest, err := store.VerifyBackup(destination)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"destination": destination, "manifest": manifest})
}

func (a *AdminAPI) resolveBackupDestination(requested string, create bool) (string, error) {
	root := strings.TrimSpace(a.backupDirectory)
	if root == "" {
		return "", errors.New("managed backup directory is not configured")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", errors.New("managed backup directory is invalid")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", errors.New("managed backup directory cannot be created")
	}

	requested = strings.TrimSpace(requested)
	if requested == "" {
		if !create {
			return "", errors.New("backup destination is required")
		}
		requested = time.Now().UTC().Format("20060102-150405")
	}
	if !filepath.IsAbs(requested) {
		requested = filepath.Join(root, requested)
	}
	destination, err := filepath.Abs(requested)
	if err != nil || !pathWithinDirectory(destination, root) || destination == root {
		return "", errors.New("backup destination must be inside the managed backup directory")
	}
	return destination, nil
}

func pathWithinDirectory(candidate, root string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
