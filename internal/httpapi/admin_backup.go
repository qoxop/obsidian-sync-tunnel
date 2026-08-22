package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (a *AdminAPI) listBackups(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimitQuery(r, "limit", 50)
	if err != nil || limit < 1 || limit > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 1000")
		return
	}
	runs, err := a.store.ListBackupRuns(r.Context(), limit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"backups": runs})
}

func (a *AdminAPI) verifyBackup(w http.ResponseWriter, r *http.Request) {
	destination, manifest, err := a.store.VerifyBackupRun(r.Context(), r.PathValue("backup"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"destination": destination, "manifest": manifest})
}

func (a *AdminAPI) newBackupDestination() (string, error) {
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

	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return "", errors.New("cannot generate backup identifier")
	}
	name := time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(suffix)
	return filepath.Join(root, name), nil
}

func pathWithinDirectory(candidate, root string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
