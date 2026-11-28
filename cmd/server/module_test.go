package main

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/module"
	"akra/internal/testutil"
)

// Modul per sekolah (T163): perubahan status modul terlihat di menu tanpa
// login ulang, paling lama module.CacheTTL (jam palsu).
func TestModuleChangeWithoutRelogin(t *testing.T) {
	app, tx, a, _ := newTenantApp(t)
	ctx := context.Background()
	clock := time.Now()
	module.Now = func() time.Time { return clock }
	t.Cleanup(func() { module.Now = time.Now; module.Forget(a.ID) })

	cl := newClient(t, app)
	if rec := cl.login(a.Code, a.Admin.Email, testutil.Password); rec.Code != http.StatusSeeOther {
		t.Fatalf("login: %d", rec.Code)
	}
	menu := func() string { return between(cl.get("/").Body.String(), `<nav id="app-nav"`, "</nav>") }
	if m := menu(); !strings.Contains(m, `href="/absensi/rekap"`) || !strings.Contains(m, `href="/ujian"`) {
		t.Fatalf("menu before:\n%s", m)
	}
	// Implementator mematikan absensi dan ujian langsung di database (replika
	// lain): menu replika ini berubah setelah cache habis, tanpa login ulang.
	if _, err := tx.Exec(ctx, `INSERT INTO school_modules (school_id, module, enabled) VALUES ($1, 'attendance', false)
		ON CONFLICT (school_id, module) DO UPDATE SET enabled = false`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE school_modules SET enabled = false WHERE school_id = $1 AND module = 'exam'`, a.ID); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(module.CacheTTL + time.Second)
	m := menu()
	if strings.Contains(m, `href="/absensi/rekap"`) || strings.Contains(m, `href="/ujian"`) {
		t.Errorf("menu still shows disabled modules:\n%s", m)
	}
	if !strings.Contains(m, `href="/siswa"`) || !strings.Contains(m, `href="/keuangan/tunggakan"`) {
		t.Errorf("menu lost core or billing:\n%s", m)
	}
	// Diaktifkan lagi lewat module.Save di replika ini: langsung terlihat.
	if err := module.Save(ctx, tx, a.ID, module.Attendance, module.State{Enabled: true}, "uji", nil); err != nil {
		t.Fatal(err)
	}
	if m := menu(); !strings.Contains(m, `href="/absensi/rekap"`) {
		t.Errorf("menu after re-enable:\n%s", m)
	}
}

// Registry route (T164): field Module setiap entri sama dengan pemilik
// paling spesifik di katalog modul.
func TestRegistryModules(t *testing.T) {
	for _, r := range testutil.Routes {
		want := string(module.Owner(r.Pattern))
		if want == string(module.Core) {
			want = ""
		}
		if r.Module != want {
			t.Errorf("%s %s: Module %q, catalog says %q", r.Method, r.Pattern, r.Module, want)
		}
	}
}

// Contoh halaman per modul yang harus tetap terbuka saat modul lain mati.
var moduleSample = map[module.ID]struct{ role, path string }{
	module.Attendance:    {testutil.Teacher, "/absensi/"},
	module.Schedule:      {testutil.Admin, "/jadwal/"},
	module.Grades:        {testutil.Admin, "/nilai/rekap/"},
	module.Billing:       {testutil.Admin, "/keuangan/tunggakan"},
	module.Admission:     {testutil.Admin, "/pendaftaran/"},
	module.Dapodik:       {testutil.Admin, "/dapodik/"},
	module.Exam:          {testutil.Admin, "/ujian/"},
	module.Announcements: {testutil.Admin, "/pengumuman"},
	module.Portal:        {testutil.Parent, "/portal/"},
}

// Penegakan modul (T164): untuk setiap modul opsional yang dimatikan di
// sekolah A, semua route modul itu 404 (halaman staf: pesan "modul tidak
// aktif" tanpa data), contoh route modul lain tetap 200, dan sekolah B tidak
// terpengaruh.
func TestModuleGate(t *testing.T) {
	app, tx, a, b := newTenantApp(t)
	ctx := context.Background()
	t.Cleanup(func() { module.Forget(a.ID); module.Forget(b.ID) })
	login := func(s *testutil.School) map[string]*client {
		out := map[string]*client{"": newClient(t, app)}
		for _, u := range []testutil.User{s.Admin, s.Treasurer, s.Teacher, s.Principal, s.Parent} {
			cl := newClient(t, app)
			if rec := cl.login(s.Code, u.Email, testutil.Password); rec.Code != http.StatusSeeOther {
				t.Fatalf("login %s: %d", u.Role, rec.Code)
			}
			cl.get("/") // token CSRF
			out[u.Role] = cl
		}
		return out
	}
	ca, cb := login(a), login(b)
	// Data yang tidak boleh muncul di halaman "modul tidak aktif".
	var secrets []string
	rows, err := tx.Query(ctx, `SELECT full_name FROM students WHERE school_id = $1`, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		rows.Scan(&n)
		secrets = append(secrets, n)
	}
	rows.Close()

	for _, info := range module.Catalog {
		m := info.ID
		if m == module.Core {
			continue
		}
		before, _, err := module.GetState(ctx, tx, a.ID, m)
		if err != nil {
			t.Fatal(err)
		}
		off := before
		off.Enabled = false
		if err := module.Save(ctx, tx, a.ID, m, off, "uji", nil); err != nil {
			t.Fatal(err)
		}
		checked := 0
		for _, r := range testutil.Routes {
			if !slicesContains(module.Required(r.Pattern), m) {
				continue
			}
			// Route publik tanpa sekolah di URL (cek perangkat, panduan,
			// halaman awal peserta) tidak terikat modul sekolah mana pun;
			// aktivasi wali diuji di paket auth.
			if r.Kind == testutil.KindPublic && !strings.Contains(r.Pattern, "{code}") {
				continue
			}
			var reqs []testutil.Request
			switch {
			case r.Requests != nil:
				reqs = r.Requests(a, a)
			case strings.Contains(r.Pattern, "{code}"):
				reqs = []testutil.Request{{Path: strings.ReplaceAll(r.Pattern, "{code}", a.Code)}}
			case !strings.Contains(r.Pattern, "{"):
				reqs = []testutil.Request{{Path: r.Pattern}}
			}
			role := ""
			if len(r.Roles) > 0 {
				role = r.Roles[0]
			}
			for _, req := range reqs {
				form := req.Form
				if r.Method == http.MethodPost && form == nil {
					form = url.Values{}
				}
				rec := ca[role].do(r.Method, req.Path, form)
				checked++
				if rec.Code != http.StatusNotFound {
					t.Errorf("%s off: %s %s as %q = %d, want 404", m, r.Method, req.Path, role, rec.Code)
				}
			}
		}
		if checked == 0 {
			t.Errorf("%s: no routes checked", m)
		}
		// Halaman staf: pesan modul tidak aktif, tanpa data modul.
		if s := moduleSample[m]; s.role != testutil.Parent {
			req := httptest.NewRequest(http.MethodGet, s.path, nil)
			req.Header.Set("Accept", "text/html")
			for _, c := range ca[s.role].cookies {
				req.AddCookie(c)
			}
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			body := rec.Body.String()
			if rec.Code != http.StatusNotFound || !strings.Contains(body, "Modul "+html.EscapeString(info.Name)+" tidak aktif untuk sekolah Anda") {
				t.Errorf("%s off: staff page %d without module message", m, rec.Code)
			}
			for _, sec := range secrets {
				if strings.Contains(body, sec) {
					t.Errorf("%s off: inactive page shows %q", m, sec)
				}
			}
		}
		// Modul lain tetap terbuka; sekolah B tidak terpengaruh.
		for other, s := range moduleSample {
			if other == m {
				if rec := cb[s.role].get(s.path); !open(rec.Code) {
					t.Errorf("%s off in A: school B %s = %d", m, s.path, rec.Code)
				}
				continue
			}
			if rec := ca[s.role].get(s.path); !open(rec.Code) {
				t.Errorf("%s off: %s (%s) = %d, want open", m, s.path, other, rec.Code)
			}
		}
		t.Logf("%s: %d requests closed", m, checked)
		if err := module.Save(ctx, tx, a.ID, m, before, "uji", nil); err != nil {
			t.Fatal(err)
		}
	}
}

// open: halaman terbuka (200 atau dialihkan), bukan 404 atau error.
func open(code int) bool { return code < 400 }

func slicesContains(ids []module.ID, id module.ID) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
