package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"akra/internal/testutil"
)

var hrefRe = regexp.MustCompile(`href="(/[^"#?]*)"`)

// between mengembalikan isi body di antara start dan end pertama setelahnya.
func between(body, start, end string) string {
	i := strings.Index(body, start)
	if i < 0 {
		return ""
	}
	rest := body[i:]
	if j := strings.Index(rest, end); j >= 0 {
		return rest[:j]
	}
	return rest
}

// Menu berkelompok dan pusat Pengaturan (T156): sidebar tanpa halaman
// pengaturan, dan setiap tautan pusat Pengaturan bisa dibuka peran yang
// melihatnya.
func TestMenuGroupsAndSettingsHub(t *testing.T) {
	app, _, a, _ := newTenantApp(t)
	for _, c := range []struct {
		user   testutil.User
		groups []string // judul kelompok yang wajib ada di sidebar
		hub    []string // tautan wajib di pusat Pengaturan
		bottom string   // tautan wajib di bottom nav
	}{
		{a.Admin, []string{"Akademik", "Keuangan", "Informasi"}, []string{"/pengaturan/sekolah", "/pengaturan/absensi", "/pengaturan/rapor", "/pengaturan/keuangan", "/pendaftaran/pengaturan", "/pengaturan/penugasan"}, "/siswa"},
		{a.Principal, []string{"Akademik", "Keuangan"}, []string{"/pengaturan/sekolah", "/pengaturan/keuangan"}, "/laporan"},
		{a.Treasurer, []string{"Keuangan", "Informasi"}, []string{"/pengaturan/keuangan"}, "/keuangan/pembayaran/baru"},
		{a.Teacher, []string{"Akademik"}, []string{"/pengaturan/penugasan"}, "/nilai/input"},
	} {
		cl := newClient(t, app)
		if rec := cl.login(a.Code, c.user.Email, testutil.Password); rec.Code != http.StatusSeeOther {
			t.Fatalf("login %s: %d", c.user.Role, rec.Code)
		}
		home := cl.get("/").Body.String()
		side := between(home, `<nav id="app-nav"`, "</nav>")
		for _, g := range c.groups {
			if !strings.Contains(side, `class="app-nav-group" role="presentation">`+g+`</p>`) {
				t.Errorf("%s sidebar lacks group %q", c.user.Role, g)
			}
		}
		for _, m := range hrefRe.FindAllStringSubmatch(side, -1) {
			if strings.HasPrefix(m[1], "/pengaturan/") && m[1] != "/pengaturan/penugasan" {
				t.Errorf("%s sidebar still links settings page %s", c.user.Role, m[1])
			}
		}
		if bottom := between(home, `<nav class="bottom-nav"`, "</nav>"); !strings.Contains(bottom, `href="`+c.bottom+`"`) {
			t.Errorf("%s bottom nav lacks %s:\n%s", c.user.Role, c.bottom, bottom)
		}

		rec := cl.get("/pengaturan")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s /pengaturan = %d", c.user.Role, rec.Code)
		}
		main := between(rec.Body.String(), "<h1>Pengaturan</h1>", "</main>")
		links := map[string]bool{}
		for _, m := range hrefRe.FindAllStringSubmatch(main, -1) {
			links[m[1]] = true
		}
		for _, want := range c.hub {
			if !links[want] {
				t.Errorf("%s hub lacks %s", c.user.Role, want)
			}
		}
		// Setiap tautan pusat Pengaturan bisa dibuka peran itu.
		for l := range links {
			if code := cl.get(l).Code; code != http.StatusOK {
				t.Errorf("%s hub link %s = %d", c.user.Role, l, code)
			}
		}
	}
}
