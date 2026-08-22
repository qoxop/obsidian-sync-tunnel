package httpapi

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

func newAdminUIHandler(directory string) http.Handler {
	if strings.TrimSpace(directory) == "" {
		return nil
	}
	root := os.DirFS(directory)
	if _, err := fs.Stat(root, "index.html"); err != nil {
		return nil
	}
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		relative := strings.TrimPrefix(r.URL.Path, "/admin/")
		clean := path.Clean("/" + relative)
		clean = strings.TrimPrefix(clean, "/")
		if clean == "." || clean == "" {
			clean = "index.html"
		}

		info, err := fs.Stat(root, clean)
		if err != nil {
			if !strings.Contains(path.Base(clean), ".") {
				clean = "index.html"
			} else {
				http.NotFound(w, r)
				return
			}
		} else if info.IsDir() {
			http.NotFound(w, r)
			return
		}

		clone := r.Clone(r.Context())
		clone.URL.Path = "/" + clean
		if clean == "index.html" {
			content, readErr := fs.ReadFile(root, clean)
			if readErr != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(content)
			return
		}
		files.ServeHTTP(w, clone)
	})
}
