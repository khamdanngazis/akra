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
	"sync"

	"akra/internal/platform/csrf"
	"akra/internal/platform/nav"
	"akra/internal/platform/subnav"
)

const (
	layoutFile  = "layout.html"
	partialsDir = "partials"
)

// Renderer menyimpan semua halaman yang sudah di-parse.
type Renderer struct {
	pages map[string]*template.Template
	// clones menyimpan salinan template per halaman untuk dipakai ulang
	// (T152): fungsi khusus request (token CSRF, menu aktif) dipasang ke
	// salinan yang sedang dipakai satu request saja, sehingga Clone yang
	// mahal tidak terjadi di setiap request.
	clones map[string]*sync.Pool
	logger *slog.Logger
	// mapsKey adalah API key Google Maps untuk peta lokasi rumah (T112);
	// kosong berarti fitur peta tidak tampil.
	mapsKey string
}

// SetMapsKey mengatur API key Google Maps yang dibaca template lewat
// {{mapsKey}}. Dipanggil sekali saat start, sebelum melayani request.
func (r *Renderer) SetMapsKey(key string) { r.mapsKey = key }

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
	rd := &Renderer{logger: logger}
	fm := funcs(versions)
	fm["mapsKey"] = func() string { return rd.mapsKey }
	base, err := template.New(layoutFile).Funcs(fm).ParseFS(fsys, layoutFile)
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
	rd.pages = pages
	rd.clones = make(map[string]*sync.Pool, len(pages))
	for name := range pages {
		rd.clones[name] = &sync.Pool{}
	}
	return rd, nil
}

// Crumb adalah satu tingkat jejak navigasi; URL kosong = halaman sekarang.
type Crumb struct{ Label, URL string }

// Trail adalah jejak navigasi beserta tingkat induk terdekat (tujuan
// tombol kembali).
type Trail struct {
	Items  []Crumb
	Parent Crumb
}

// funcs berisi fungsi template yang tersedia di semua halaman. Fungsi CSRF
// diganti per request di Render.
func funcs(versions map[string]string) template.FuncMap {
	m := template.FuncMap{
		// dict membuat map untuk argumen partial, mis.
		// {{template "password_field" dict "id" "password" "name" "password"}}.
		"dict": func(kv ...any) (map[string]any, error) {
			if len(kv)%2 != 0 {
				return nil, fmt.Errorf("render: dict needs key/value pairs")
			}
			m := make(map[string]any, len(kv)/2)
			for i := 0; i < len(kv); i += 2 {
				k, ok := kv[i].(string)
				if !ok {
					return nil, fmt.Errorf("render: dict key %v is not a string", kv[i])
				}
				m[k] = kv[i+1]
			}
			return m, nil
		},
		// crumbs membuat jejak navigasi dari pasangan label dan URL, mis.
		// {{template "back_nav" crumbs "Pendaftar" "/pendaftaran" .Name ""}}.
		// URL kosong menandai halaman sekarang (T121).
		"crumbs": func(kv ...string) (Trail, error) {
			if len(kv)%2 != 0 {
				return Trail{}, fmt.Errorf("render: crumbs needs label/URL pairs")
			}
			var t Trail
			for i := 0; i < len(kv); i += 2 {
				c := Crumb{Label: kv[i], URL: kv[i+1]}
				t.Items = append(t.Items, c)
				if c.URL != "" {
					t.Parent = c
				}
			}
			return t, nil
		},
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
	m["menu"] = nav.MenuForUser
	m["roleLabel"] = nav.RoleLabel
	// menuGroups mengelompokkan item menu (T156); bottomMenu dan shortcuts
	// memilih item per prioritas peran.
	m["menuGroups"] = nav.Groups
	m["bottomMenu"] = nav.Bottom
	m["shortcuts"] = nav.Shortcuts
	m["initials"] = nav.Initials
	// currentMenu mengembalikan Path item menu yang aktif untuk request ini.
	m["currentMenu"] = func([]nav.MenuItem) string { return "" }
	// subnav mengembalikan sub-navigasi modul request ini (T157), bila ada.
	m["subnav"] = func() []subnav.Item { return nil }
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
	pool := rd.clones[name]
	t, _ := pool.Get().(*template.Template)
	if t == nil {
		var err error
		if t, err = page.Clone(); err != nil {
			return err
		}
	}
	var buf bytes.Buffer
	reqFuncs := csrf.FuncMap(r)
	reqFuncs["currentMenu"] = func(items []nav.MenuItem) string { return nav.Current(items, r.URL.Path) }
	reqFuncs["subnav"] = func() []subnav.Item { return subnav.From(r.Context()) }
	err := t.Funcs(reqFuncs).ExecuteTemplate(&buf, block, data)
	// Fungsi request dilepas sebelum salinan kembali ke pool agar tidak
	// menahan request lama.
	t.Funcs(csrf.PlaceholderFuncs)
	t.Funcs(template.FuncMap{"currentMenu": func([]nav.MenuItem) string { return "" }, "subnav": func() []subnav.Item { return nil }})
	pool.Put(t)
	if err != nil {
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
