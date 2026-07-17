package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/imprun/windforce-core/internal/webui"
)

var (
	webUIFS     = mustWebUIAssets()
	webUIAssets = http.FileServer(http.FS(webUIFS))
)

func mustWebUIAssets() fs.FS {
	assets, err := fs.Sub(webui.FS, "assets")
	if err != nil {
		panic(err)
	}
	return assets
}

func (h *Handler) handleWebUI(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	switch r.URL.Path {
	case "/":
		http.Redirect(w, r, "/ui/", http.StatusFound)
		return true
	case "/ui":
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
		return true
	}
	if !strings.HasPrefix(r.URL.Path, "/ui/") {
		return false
	}
	assetPath := strings.TrimPrefix(r.URL.Path, "/ui/")
	r = r.Clone(r.Context())
	if assetPath != "" && !webUIAssetExists(assetPath) && !looksLikeAssetPath(assetPath) {
		// The Web UI is a single-page app: client-side routes such as
		// /ui/jobs/{id} fall back to index.html. Paths with a file
		// extension stay 404 so a stale browser asking for an old hashed
		// bundle gets an error instead of index.html with a 200.
		r.URL.Path = "/"
	} else {
		r.URL.Path = "/" + assetPath
	}
	webUIAssets.ServeHTTP(w, r)
	return true
}

func webUIAssetExists(assetPath string) bool {
	info, err := fs.Stat(webUIFS, assetPath)
	return err == nil && !info.IsDir()
}

func looksLikeAssetPath(assetPath string) bool {
	return strings.Contains(path.Base(assetPath), ".")
}
