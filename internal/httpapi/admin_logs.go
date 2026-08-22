package httpapi

import (
	"bufio"
	"encoding/json"
	"errors"
	"net/http"
	"os"
)

func (a *AdminAPI) listLogs(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimitQuery(r, "limit", 200)
	if err != nil || limit < 1 || limit > 1000 {
		writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 1000")
		return
	}
	entries, err := readJSONLogTail(a.logPath, limit)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func readJSONLogTail(path string, limit int) ([]map[string]any, error) {
	if path == "" {
		return []map[string]any{}, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	ring := make([]map[string]any, 0, limit)
	next := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var entry map[string]any
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if len(ring) < limit {
			ring = append(ring, entry)
			continue
		}
		ring[next] = entry
		next = (next + 1) % limit
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	result := make([]map[string]any, 0, len(ring))
	for offset := len(ring) - 1; offset >= 0; offset-- {
		index := offset
		if len(ring) == limit {
			index = (next + offset) % len(ring)
		}
		result = append(result, ring[index])
	}
	return result, nil
}
