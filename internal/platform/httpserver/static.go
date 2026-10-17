package httpserver

import (
	"io/fs"
	"net/http"
	"strings"
)

// StaticCacheControl adalah header cache untuk /static/. Aset belum memakai
// nama berhash, jadi perubahan CSS/JS bisa terlambat sampai 1 hari di browser
// yang sudah menyimpannya.
const StaticCacheControl = "public, max-age=86400"

// Static melayani file dari fsys dengan header cache, tanpa daftar isi
// folder. Pasang dengan http.StripPrefix("/static/", ...).
func Static(fsys fs.FS) http.Handler {
	files := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		if _, err := fs.Stat(fsys, strings.TrimPrefix(r.URL.Path, "/")); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", StaticCacheControl)
		files.ServeHTTP(w, r)
	})
}
