// Package render merender halaman HTML dari template di sebuah fs.FS.
//
// Struktur template:
//   - layout.html mendefinisikan template "layout" (kerangka halaman) dan
//     memanggil blok "title" dan "content".
//   - partials/*.html (opsional) berisi template kecil yang dipakai bersama.
//   - Semua file .html lain adalah halaman. Nama halaman adalah path tanpa
//     ".html", misalnya "auth/login" untuk auth/login.html. Halaman
//     mendefinisikan blok "title" dan "content".
package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"sort"
	"strings"

	"akra/internal/platform/csrf"
	"akra/internal/platform/nav"
)

const (
	layoutFile  = "layout.html"
	partialsDir = "partials"
)

// Renderer menyimpan semua halaman yang sudah di-parse.
type Renderer struct {
	pages  map[string]*template.Template
	logger *slog.Logger
}

// New mem-parse semua template di fsys. Error parse dikembalikan dengan nama
// file-nya agar server gagal start dengan pesan yang jelas.
//
// static adalah isi folder /static/ (boleh nil). Dari situ dihitung versi
// tiap file untuk fungsi template {{ asset "css/app.css" }}, yang
// menghasilkan "/static/css/app.css?v=<hash>". URL berubah setiap isi file
// berubah, sehingga cache browser (max-age 1 hari) tidak menahan versi lama.
func New(fsys, static fs.FS, logger *slog.Logger) (*Renderer, error) {
	versions, err := assetVersions(static)
	if err != nil {
		return nil, err
	}
	base, err := template.New(layoutFile).Funcs(funcs(versions)).ParseFS(fsys, layoutFile)
	if err != nil {
		return nil, fmt.Errorf("render: parse %s: %w", layoutFile, err)
	}
	if base.Lookup("layout") == nil {
		return nil, fmt.Errorf("render: %s does not define a \"layout\" template", layoutFile)
	}

	var pageFiles []string
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path.Ext(p) != ".html" || p == layoutFile {
			return nil
		}
		if strings.HasPrefix(p, partialsDir+"/") {
			if _, err := base.ParseFS(fsys, p); err != nil {
				return fmt.Errorf("render: parse %s: %w", p, err)
			}
			return nil
		}
		pageFiles = append(pageFiles, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(pageFiles)

	pages := make(map[string]*template.Template, len(pageFiles))
	for _, p := range pageFiles {
		t, err := template.Must(base.Clone()).ParseFS(fsys, p)
		if err != nil {
			return nil, fmt.Errorf("render: parse %s: %w", p, err)
		}
		pages[strings.TrimSuffix(p, ".html")] = t
	}
	return &Renderer{pages: pages, logger: logger}, nil
}

// funcs berisi fungsi template yang tersedia di semua halaman. Fungsi CSRF
// diganti per request di Render.
func funcs(versions map[string]string) template.FuncMap {
	m := template.FuncMap{
		// inc menambah satu; mis. nomor urut {{inc $i}} dari indeks range.
		"inc": func(i int) int { return i + 1 },
		// seq menghasilkan from, from+1, ..., to; mis. {{range seq 1 12}}.
		"seq": func(from, to int) []int {
			var s []int
			for i := from; i <= to; i++ {
				s = append(s, i)
			}
			return s
		},
		"asset": func(p string) (string, error) {
			if versions == nil {
				return "/static/" + p, nil
			}
			v, ok := versions[p]
			if !ok {
				return "", fmt.Errorf("render: unknown static asset %q", p)
			}
			return "/static/" + p + "?v=" + v, nil
		},
	}
	// menu mengembalikan item navigasi untuk peran pengguna (T51).
	m["menu"] = nav.MenuFor
	// currentMenu mengembalikan Path item menu yang aktif untuk request ini.
	m["currentMenu"] = func([]nav.MenuItem) string { return "" }
	for k, v := range csrf.PlaceholderFuncs {
		m[k] = v
	}
	return m
}

// Render merender halaman name dengan data dan status HTTP status. Output
// ditulis ke buffer dulu, jadi jika template gagal dieksekusi, klien
// menerima 500 (bukan halaman setengah jadi) dan error dicatat di log.
func (rd *Renderer) Render(w http.ResponseWriter, r *http.Request, status int, name string, data any) {
	if err := rd.render(w, r, status, name, data); err != nil {
		rd.logger.ErrorContext(r.Context(), "render page", "page", name, "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

// RenderFragment merender satu blok (define) dari halaman name tanpa layout,
// mis. hasil pencarian yang dimuat Alpine.js ke halaman yang sudah terbuka.
func (rd *Renderer) RenderFragment(w http.ResponseWriter, r *http.Request, status int, name, block string, data any) {
	if err := rd.execute(w, r, status, name, block, data); err != nil {
		rd.logger.ErrorContext(r.Context(), "render fragment", "page", name, "block", block, "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func (rd *Renderer) render(w http.ResponseWriter, r *http.Request, status int, name string, data any) error {
	return rd.execute(w, r, status, name, "layout", data)
}

func (rd *Renderer) execute(w http.ResponseWriter, r *http.Request, status int, name, block string, data any) error {
	page, ok := rd.pages[name]
	if !ok {
		return fmt.Errorf("render: unknown page %q", name)
	}
	t, err := page.Clone()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	reqFuncs := csrf.FuncMap(r)
	reqFuncs["currentMenu"] = func(items []nav.MenuItem) string { return nav.Current(items, r.URL.Path) }
	if err := t.Funcs(reqFuncs).ExecuteTemplate(&buf, block, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err = buf.WriteTo(w)
	return err
}

// Pages mengembalikan nama semua halaman yang terdaftar, terurut.
func (rd *Renderer) Pages() []string {
	names := make([]string, 0, len(rd.pages))
	for n := range rd.pages {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// assetVersions menghitung 12 karakter pertama SHA-256 setiap file di
// static. nil jika static nil.
func assetVersions(static fs.FS) (map[string]string, error) {
	if static == nil {
		return nil, nil
	}
	versions := map[string]string{}
	err := fs.WalkDir(static, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := fs.ReadFile(static, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		versions[p] = hex.EncodeToString(sum[:])[:12]
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("render: hash static assets: %w", err)
	}
	return versions, nil
}
