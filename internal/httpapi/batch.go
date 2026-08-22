package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"obsidian-sync-tunnel/internal/store"
)

func (a *API) batchDelete(w http.ResponseWriter, r *http.Request) {
	deviceID := requestPrincipal(r).DeviceID
	operationID := r.Header.Get("X-Operation-ID")
	if deviceID == "" {
		writeError(w, http.StatusUnauthorized, "unpaired_device", "paired device credential required")
		return
	}
	if operationID == "" {
		writeError(w, http.StatusBadRequest, "missing_operation", "X-Operation-ID is required")
		return
	}
	var request struct {
		Items []store.BatchDeleteItem `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid batch delete request")
		return
	}
	changes, changed, err := a.store.BatchDeleteWithOperation(r.Context(), r.PathValue("vault"), deviceID, operationID, request.Items)
	if err != nil {
		var primary store.Change
		if len(changes) > 0 {
			primary = changes[0]
		}
		writeMutationResult(w, primary, changed, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changes": changes, "changed": changed})
}
