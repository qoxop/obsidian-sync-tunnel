package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"obsidian-sync-tunnel/internal/store"
)

func TestAdminLogTailReturnsNewestStructuredEntries(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	logPath := filepath.Join(root, "server.jsonl")
	content := []byte("not-json\n{\"time\":\"one\",\"level\":\"INFO\",\"msg\":\"first\"}\n{\"time\":\"two\",\"level\":\"WARN\",\"msg\":\"second\"}\n")
	if err := os.WriteFile(logPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	handler := NewAdmin(db, "", AdminOptions{LogPath: logPath}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := serve(handler, newLocalAdminRequest(http.MethodGet, "/admin/v1/logs?limit=1", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"msg":"second"`) || strings.Contains(response.Body.String(), `"msg":"first"`) {
		t.Fatalf("logs=%d %s", response.Code, response.Body)
	}
}
