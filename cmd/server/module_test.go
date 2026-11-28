package main

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

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
	saveModule(t, tx, a.ID, module.Attendance, module.State{Enabled: true})
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
		saveModule(t, tx, a.ID, m, off)
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
		saveModule(t, tx, a.ID, m, before)
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

var linkRe = regexp.MustCompile(`(?:href|action|src|hx-get|data-[a-z-]*url)="(/[^"#?]*)`)

// Halaman lintas modul (T165): untuk setiap modul yang dimatikan, beranda
// setiap peran, portal orang tua, hub laporan, pembuat laporan, dan impor
// dirender tanpa error dan tanpa tautan ke route modul itu.
func TestCrossModulePagesFollowModules(t *testing.T) {
	app, tx, a, _ := newTenantApp(t)
	ctx := context.Background()
	t.Cleanup(func() { module.Forget(a.ID) })
	clients := map[string]*client{}
	for _, u := range []testutil.User{a.Admin, a.Treasurer, a.Teacher, a.Principal, a.Parent} {
		cl := newClient(t, app)
		if rec := cl.login(a.Code, u.Email, testutil.Password); rec.Code != http.StatusSeeOther {
			t.Fatalf("login %s: %d", u.Role, rec.Code)
		}
		clients[u.Role] = cl
	}
	child := a.StudentIDs[0] // anak wali Parent
	pages := map[string][]string{
		testutil.Admin:     {"/", "/laporan", "/laporan/bangun/", "/impor/siswa", "/impor/mapping", "/pengaturan", "/siswa/" + child},
		testutil.Treasurer: {"/", "/laporan", "/pengaturan"},
		testutil.Teacher:   {"/"},
		testutil.Principal: {"/", "/laporan"},
		testutil.Parent:    {"/portal/", "/portal/siswa/" + child},
	}
	for _, info := range module.Catalog {
		m := info.ID
		if m == module.Core {
			continue
		}
		before, _, _ := module.GetState(ctx, tx, a.ID, m)
		off := before
		off.Enabled = false
		saveModule(t, tx, a.ID, m, off)
		for role, list := range pages {
			for _, p := range list {
				rec := clients[role].get(p)
				if m == module.Portal && role == testutil.Parent {
					continue // seluruh portal tertutup (T164)
				}
				if rec.Code >= 400 {
					t.Errorf("%s off: %s %s = %d", m, role, p, rec.Code)
					continue
				}
				seen := map[string]bool{}
				for _, mm := range linkRe.FindAllStringSubmatch(rec.Body.String(), -1) {
					link := mm[1]
					if seen[link] {
						continue
					}
					seen[link] = true
					if slicesContains(module.Required(link), m) {
						t.Errorf("%s off: %s %s links to %s", m, role, p, link)
					}
				}
			}
		}
		saveModule(t, tx, a.ID, m, before)
	}
}

// Pembuat laporan dan impor (T165): sumber dan target modul mati
// disembunyikan; konfigurasi lama terkunci tanpa error.
func TestReportsAndImportFollowModules(t *testing.T) {
	app, tx, a, _ := newTenantApp(t)
	ctx := context.Background()
	t.Cleanup(func() { module.Forget(a.ID) })
	if _, err := tx.Exec(ctx, `INSERT INTO import_mappings (school_id, name, target, column_map, created_by)
		VALUES ($1, 'Format nilai rapor lama', 'grades', '{}', $2)`, a.ID, a.Admin.ID); err != nil {
		t.Fatal(err)
	}
	admin := newClient(t, app)
	if rec := admin.login(a.Code, a.Admin.Email, testutil.Password); rec.Code != http.StatusSeeOther {
		t.Fatal(rec.Code)
	}
	cfg := a.ReportConfigID // sumber kehadiran
	if body := admin.get("/laporan/konfigurasi/").Body.String(); strings.Contains(body, "Terkunci") || !strings.Contains(body, "konfigurasi="+cfg) {
		t.Fatal("config before: locked or not openable")
	}
	for _, m := range []module.ID{module.Attendance, module.Grades} {
		saveModule(t, tx, a.ID, m, module.State{Enabled: false})
	}
	rec := admin.get("/laporan/konfigurasi/")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Terkunci") || strings.Contains(body, "konfigurasi="+cfg) || strings.Contains(body, cfg+".xlsx") {
		t.Errorf("locked config list = %d", rec.Code)
	}
	if strings.Contains(body, `value="attendance"`) || strings.Contains(body, `value="grades"`) {
		t.Error("inactive sources offered in config form")
	}
	for _, p := range []string{"/laporan/bangun/?konfigurasi=" + cfg, "/laporan/konfigurasi/" + cfg + ".xlsx"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		for _, c := range admin.cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "terkunci") {
			t.Errorf("%s = %d, want 404 locked", p, rec.Code)
		}
	}
	if body := admin.get("/laporan/bangun/").Body.String(); strings.Contains(body, `&#34;name&#34;:&#34;attendance&#34;`) || strings.Contains(body, `"name":"attendance"`) || !strings.Contains(body, "arrears") {
		t.Error("builder offers inactive source or lost active one")
	}
	if body := admin.get("/impor/mapping").Body.String(); strings.Contains(body, "Format nilai rapor lama") || strings.Contains(body, `<option value="grades">`) {
		t.Error("import mapping shows grades target")
	}
	if rec := admin.do(http.MethodPost, "/impor/mapping", url.Values{"name": {"X"}, "target": {"grades"}}); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("post grades mapping = %d", rec.Code)
	}
}

// Halaman Modul & Paket (T167): modul aktif dan tidak aktif sesuai
// school_modules, baca saja untuk admin TU dan kepala sekolah; pusat
// Pengaturan hanya menampilkan bagian modul aktif.
func TestModulesPage(t *testing.T) {
	app, tx, a, _ := newTenantApp(t)
	ctx := context.Background()
	t.Cleanup(func() { module.Forget(a.ID) })
	if _, err := tx.Exec(ctx, `UPDATE schools SET package = 'lengkap' WHERE id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	saveModule(t, tx, a.ID, module.Billing, module.State{Enabled: false})
	clients := map[string]*client{}
	for _, u := range []testutil.User{a.Admin, a.Principal, a.Treasurer, a.Teacher} {
		cl := newClient(t, app)
		if rec := cl.login(a.Code, u.Email, testutil.Password); rec.Code != http.StatusSeeOther {
			t.Fatalf("login %s: %d", u.Role, rec.Code)
		}
		clients[u.Role] = cl
	}
	for _, role := range []string{testutil.Admin, testutil.Principal} {
		rec := clients[role].get("/pengaturan/modul")
		body := rec.Body.String()
		if rec.Code != http.StatusOK {
			t.Fatalf("%s modules page = %d", role, rec.Code)
		}
		active := between(body, `id="mod-active"`, "</section>")
		inactive := between(body, `id="mod-inactive"`, "</section>")
		for _, name := range []string{"Inti", "Absensi", "Ujian Online", "Portal Orang Tua"} {
			if !strings.Contains(active, "<strong>"+name+"</strong>") {
				t.Errorf("%s: %s not listed as active", role, name)
			}
		}
		if !strings.Contains(inactive, "<strong>Keuangan</strong>") || strings.Contains(active, "<strong>Keuangan</strong>") {
			t.Errorf("%s: Keuangan not listed as not included", role)
		}
		// Baca saja: tidak ada form di isi halaman.
		if !strings.Contains(body, "Paket Lengkap") || !strings.Contains(body, "Hubungi tim implementator AKRA") || strings.Contains(between(body, "<main", "</main>"), "<form") {
			t.Errorf("%s: package label, contact note, or a form on a read-only page", role)
		}
	}
	for _, role := range []string{testutil.Treasurer, testutil.Teacher} {
		if rec := clients[role].get("/pengaturan/modul"); rec.Code != http.StatusForbidden {
			t.Errorf("%s modules page = %d, want 403", role, rec.Code)
		}
	}
	// Pusat Pengaturan: tautan Modul & Paket untuk admin; bagian Keuangan
	// hilang karena modulnya mati.
	hub := clients[testutil.Admin].get("/pengaturan").Body.String()
	if !strings.Contains(hub, `href="/pengaturan/modul"`) || strings.Contains(hub, `href="/pengaturan/keuangan"`) || !strings.Contains(hub, `href="/pengaturan/rapor"`) {
		t.Error("settings hub does not follow modules")
	}
}

// saveModule mengatur status modul sekolah langsung di database untuk uji
// (tanpa penjaga module.Admin), lalu membuang cache status modul.
func saveModule(t testing.TB, tx pgx.Tx, schoolID string, id module.ID, st module.State) {
	t.Helper()
	opts, err := json.Marshal(st.Options)
	if err != nil || st.Options == nil {
		opts = []byte("{}")
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO school_modules (school_id, module, enabled, options) VALUES ($1, $2, $3, $4)
		ON CONFLICT (school_id, module) DO UPDATE SET enabled = EXCLUDED.enabled, options = EXCLUDED.options`, schoolID, id, st.Enabled, opts); err != nil {
		t.Fatal(err)
	}
	module.Forget(schoolID)
}
