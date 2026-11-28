package main

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"akra/internal/platform/module"
	"akra/internal/testutil"
)

// combo adalah satu kombinasi modul sekolah untuk uji (T169).
type combo struct {
	name string
	on   []module.ID
}

// moduleCombos: inti saja, setiap paket templat, dan semua kecuali X untuk
// setiap modul opsional X.
func moduleCombos() []combo {
	out := []combo{{name: "core only"}}
	for _, p := range module.Packages {
		out = append(out, combo{name: "package " + p.Code, on: p.Modules})
	}
	for _, m := range module.Catalog {
		if m.ID == module.Core {
			continue
		}
		var on []module.ID
		for _, o := range module.Catalog {
			if o.ID != module.Core && o.ID != m.ID {
				on = append(on, o.ID)
			}
		}
		out = append(out, combo{name: "all but " + string(m.ID), on: on})
	}
	return out
}

// skipCombo: route yang tidak bisa dibuka di lingkungan uji ini, bukan
// karena modul. Dokumen pendaftar dibaca dari storage terenkripsi, dan
// fixture hanya mencatat kuncinya tanpa mengunggah berkas.
var skipCombo = map[string]bool{"/pendaftaran/{id}/dokumen/{docID}": true}

// Uji kombinasi modul (T169): untuk setiap kombinasi, semua route GET di
// registry yang boleh dibuka peran (dan halaman publik pendaftaran) dibuka
// tanpa 5xx, dan halaman HTML-nya tidak memuat tautan ke route modul yang
// mati. Menu dan beranda setiap peran ikut diperiksa karena termasuk route
// GET.
func TestModuleCombinations(t *testing.T) {
	app, tx, a, _ := newTenantApp(t)
	t.Cleanup(func() { module.Forget(a.ID) })
	clients := map[string]*client{"": newClient(t, app)}
	for _, u := range []testutil.User{a.Admin, a.Treasurer, a.Teacher, a.Principal, a.Parent} {
		cl := newClient(t, app)
		if rec := cl.login(a.Code, u.Email, testutil.Password); rec.Code != http.StatusSeeOther {
			t.Fatalf("login %s: %d", u.Role, rec.Code)
		}
		clients[u.Role] = cl
	}
	type visit struct{ role, path string }
	var visits []visit
	for _, r := range testutil.Routes {
		if r.Method != http.MethodGet || skipCombo[r.Pattern] {
			continue
		}
		// Halaman publik tanpa sekolah di URL (/u/, cek perangkat, panduan)
		// tidak terikat modul sekolah mana pun.
		if r.Kind == testutil.KindPublic && !strings.Contains(r.Pattern, "{code}") {
			continue
		}
		var paths []string
		switch {
		case r.Requests != nil:
			for _, q := range r.Requests(a, a) {
				paths = append(paths, q.Path)
			}
		case strings.Contains(r.Pattern, "{code}"):
			paths = []string{strings.ReplaceAll(r.Pattern, "{code}", a.Code)}
		case !strings.Contains(r.Pattern, "{") && !strings.Contains(r.Pattern, "*"):
			paths = []string{r.Pattern}
		}
		roles := r.Roles
		if r.Kind == testutil.KindPublic {
			roles = []string{""}
		}
		for _, p := range paths {
			for _, role := range roles {
				visits = append(visits, visit{role, p})
			}
		}
	}
	for _, c := range moduleCombos() {
		t.Run(c.name, func(t *testing.T) {
			for _, m := range module.Catalog {
				if m.ID != module.Core {
					saveModule(t, tx, a.ID, m.ID, module.State{Enabled: slices.Contains(c.on, m.ID), Options: map[string]string{"release": "r3"}})
				}
			}
			set := module.NewSet(c.on...)
			checked, links := 0, 0
			for _, v := range visits {
				rec := clients[v.role].get(v.path)
				checked++
				if rec.Code >= 500 {
					t.Errorf("%s %s = %d", v.role, v.path, rec.Code)
					continue
				}
				if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
					continue
				}
				seen := map[string]bool{}
				for _, mm := range linkRe.FindAllStringSubmatch(rec.Body.String(), -1) {
					link := mm[1]
					if seen[link] || strings.HasPrefix(link, "/static/") {
						continue
					}
					seen[link] = true
					links++
					if miss := set.Missing(link); len(miss) > 0 {
						t.Errorf("%s %s links to %s (module %s off)", v.role, v.path, link, miss[0])
					}
				}
			}
			t.Logf("%d pages, %d links checked", checked, links)
		})
	}
}
