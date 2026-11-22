package main

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"akra/internal/testutil"
)

// Kerangka modul ujian (T157): setiap halaman staf /ujian di registry punya
// sub-navigasi modul, tepat satu <h1>, dan breadcrumb (bila ada) berakar di
// "Ujian" → /ujian. Diuji untuk admin TU, guru, dan kepala sekolah.
func TestExamPagesFrame(t *testing.T) {
	app, _, a, _ := newTenantApp(t)
	for _, u := range []testutil.User{a.Admin, a.Teacher, a.Principal} {
		cl := newClient(t, app)
		if rec := cl.login(a.Code, u.Email, testutil.Password); rec.Code != http.StatusSeeOther {
			t.Fatalf("login %s: %d", u.Role, rec.Code)
		}
		checked := 0
		for _, r := range testutil.Routes {
			if r.Method != http.MethodGet || !strings.HasPrefix(r.Pattern, "/ujian") || !slices.Contains(r.Roles, u.Role) {
				continue
			}
			var reqs []testutil.Request
			if r.Requests != nil {
				reqs = r.Requests(a, a)
			} else if !strings.Contains(r.Pattern, "{") {
				reqs = []testutil.Request{{Path: r.Pattern}}
			}
			for _, req := range reqs {
				rec := cl.get(req.Path)
				body := rec.Body.String()
				// Hanya halaman HTML berkerangka aplikasi (bukan PDF, Excel,
				// cetak, fragmen, atau layar penuh).
				if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(body, `class="app-chrome"`) {
					continue
				}
				checked++
				if !strings.Contains(body, `<nav class="module-nav" aria-label="Menu modul">`) {
					t.Errorf("%s %s: no module sub-navigation", u.Role, req.Path)
				}
				if n := strings.Count(body, "<h1"); n != 1 {
					t.Errorf("%s %s: %d <h1>", u.Role, req.Path, n)
				}
				if crumbs := between(body, `<ol class="crumbs">`, "</ol>"); crumbs != "" &&
					!strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(crumbs, `<ol class="crumbs">`)), `<li><a href="/ujian" data-restore>Ujian</a>`) {
					t.Errorf("%s %s: breadcrumb not rooted at Ujian: %s", u.Role, req.Path, crumbs)
				}
				if strings.Contains(body, `href="/ujian/paket/" data-restore>Ujian<`) {
					t.Errorf("%s %s: breadcrumb label Ujian points to exam list", u.Role, req.Path)
				}
			}
		}
		t.Logf("%s: %d exam pages checked", u.Role, checked)
		if checked < 15 {
			t.Errorf("%s: only %d exam pages checked", u.Role, checked)
		}
	}
}

// Isi sub-navigasi per peran (T157): sesuai hak akses.
func TestExamSubnavPerRole(t *testing.T) {
	app, _, a, _ := newTenantApp(t)
	for _, c := range []struct {
		user        testutil.User
		want, never []string
	}{
		{a.Admin, []string{"/ujian/bank/", "/ujian/paket/", "/ujian/agenda", "/ujian/sesi-saya", "/ujian/pengaturan", "/ujian/bantuan"}, nil},
		{a.Teacher, []string{"/ujian/bank/", "/ujian/paket/", "/ujian/sesi-saya", "/ujian/bantuan"}, []string{"/ujian/agenda", "/ujian/pengaturan"}},
		{a.Principal, []string{"/ujian/paket/", "/ujian/agenda", "/ujian/bantuan"}, []string{"/ujian/pengaturan", "/ujian/telaah"}},
	} {
		cl := newClient(t, app)
		if rec := cl.login(a.Code, c.user.Email, testutil.Password); rec.Code != http.StatusSeeOther {
			t.Fatalf("login %s: %d", c.user.Role, rec.Code)
		}
		body := cl.get("/ujian/bantuan").Body.String()
		nav := between(body, `<nav class="module-nav"`, "</nav>")
		for _, p := range c.want {
			if !strings.Contains(nav, `href="`+p+`"`) {
				t.Errorf("%s subnav lacks %s:\n%s", c.user.Role, p, nav)
			}
		}
		for _, p := range c.never {
			if strings.Contains(nav, `href="`+p+`"`) {
				t.Errorf("%s subnav has %s", c.user.Role, p)
			}
		}
		if !strings.Contains(nav, `class="is-end" aria-current="page">Bantuan`) {
			t.Errorf("%s: Bantuan not active on help page:\n%s", c.user.Role, nav)
		}
	}
}
