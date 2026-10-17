// Package web berisi template HTML dan aset statis yang ikut masuk ke binary.
package web

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
)

// FS berisi folder templates/ dan static/.
//
//go:embed templates static
var FS embed.FS

// Load mengembalikan fs.FS untuk templates/ dan static/. Jika dir kosong,
// keduanya diambil dari FS (embed). Jika dir diisi (misalnya "web" saat
// development), keduanya dibaca dari disk agar perubahan template dan CSS
// cukup restart, tanpa rebuild.
func Load(dir string) (templates, static fs.FS, err error) {
	root := fs.FS(FS)
	if dir != "" {
		if _, err := os.Stat(dir); err != nil {
			return nil, nil, fmt.Errorf("web: WEB_DIR %q: %w", dir, err)
		}
		root = os.DirFS(dir)
	}
	if templates, err = fs.Sub(root, "templates"); err != nil {
		return nil, nil, err
	}
	if static, err = fs.Sub(root, "static"); err != nil {
		return nil, nil, err
	}
	return templates, static, nil
}
