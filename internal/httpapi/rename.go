package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
)

func (a *API) renameFile(w http.ResponseWriter, r *http.Request) {
	metadata, ok := parseMutationMetadata(w, r)
	if !ok {
		return
	}
	if metadata.operationID == "" {
		writeError(w, http.StatusBadRequest, "missing_operation", "X-Operation-ID is required for rename")
		return
	}
	var request struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 16*1024)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid rename request")
		return
	}
	change, related, changed, err := a.store.RenameWithOperation(
		r.Context(), r.PathValue("vault"), request.From, request.To, metadata.deviceID,
		metadata.operationID, metadata.baseRevision, metadata.modifiedAt,
	)
	if err != nil {
		writeMutationResult(w, change, changed, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"change": change, "related_changes": related, "changed": changed})
}
