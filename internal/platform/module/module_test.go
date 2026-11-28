package module

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
	"akra/internal/platform/nav"
)

// Katalog: kode unik, rujukan dikenal, core tanpa ketergantungan, dan
// ketergantungan wajib tidak melingkar.
func TestCatalog(t *testing.T) {
	seen := map[ID]bool{}
	for _, m := range Catalog {
		if seen[m.ID] {
			t.Errorf("duplicate module %s", m.ID)
		}
		seen[m.ID] = true
		if m.Name == "" || m.Desc == "" || len(m.Routes) == 0 {
			t.Errorf("%s: name, description, or route prefixes missing", m.ID)
		}
		for _, r := range append(append([]ID(nil), m.Requires...), m.Optional...) {
			if !Known(r) || r == m.ID {
				t.Errorf("%s refers to %q", m.ID, r)
			}
		}
	}
	if c, _ := Get(Core); len(c.Requires) > 0 || !c.Default {
		t.Error("core must be default and have no requirements")
	}
	// DFS pada ketergantungan wajib.
	state := map[ID]int{} // 1 = sedang dikunjungi, 2 = selesai
	var visit func(id ID, path []ID)
	visit = func(id ID, path []ID) {
		switch state[id] {
		case 1:
			t.Errorf("circular requirement: %v", append(path, id))
			return
		case 2:
			return
		}
		state[id] = 1
		m, _ := Get(id)
		for _, r := range m.Requires {
			visit(r, append(path, id))
		}
		state[id] = 2
	}
	for _, m := range Catalog {
		visit(m.ID, nil)
	}
}

// Setiap item menu dan pengaturan yang bermodul merujuk modul di katalog.
func TestMenuModulesInCatalog(t *testing.T) {
	for _, it := range append(append([]nav.MenuItem(nil), nav.Items...), nav.SettingsItems...) {
		if it.Module != "" && !Known(ID(it.Module)) {
			t.Errorf("menu %s: unknown module %q", it.Path, it.Module)
		}
		for _, n := range it.Needs {
			if !Known(ID(n)) {
				t.Errorf("menu %s: unknown needed module %q", it.Path, n)
			}
		}
	}
	want := Defaults().IDs()[1:] // tanpa core
	if strings.Join(nav.DefaultModules, ",") != strings.Join(want, ",") {
		t.Errorf("nav.DefaultModules = %v, want %v", nav.DefaultModules, want)
	}
	for _, c := range []string{nav.ModuleAttendance, nav.ModuleSchedule, nav.ModuleGrades, nav.ModuleBilling, nav.ModuleAdmission,
		nav.ModuleDapodik, nav.ModuleExam, nav.ModuleAnnouncements, nav.ModulePortal} {
		if !Known(ID(c)) {
			t.Errorf("nav module %q not in catalog", c)
		}
	}
}

func TestSet(t *testing.T) {
	d := Defaults()
	if !d.Has(Core) || !d.Has(Billing) || d.Has(Exam) || !d.HasName("") || d.HasName("exam") {
		t.Errorf("defaults = %v", d.IDs())
	}
	var zero Set
	if !zero.Has(Core) || zero.Has(Attendance) || strings.Join(zero.IDs(), ",") != "core" {
		t.Errorf("zero set = %v", zero.IDs())
	}
	s := NewSet(Exam, Grades)
	if got := strings.Join(s.IDs(), ","); got != "core,grades,exam" {
		t.Errorf("IDs = %s", got)
	}
	if From(context.Background()).Has(Exam) || !From(With(context.Background(), s)).Has(Exam) {
		t.Error("From without/with middleware")
	}
}

func testTx(t *testing.T) pgx.Tx {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return tx
}

// Load dengan cache per replika: perubahan di luar Save terlihat setelah
// CacheTTL (jam palsu); Save langsung terlihat dan tercatat lewat auditor.
func TestLoadCacheAndSave(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	var sid string
	if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('SD Modul', 'test-modul-cache') RETURNING id`).Scan(&sid); err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)
	Now = func() time.Time { return clock }
	t.Cleanup(func() { Now = time.Now; Forget(sid) })

	// Sekolah tanpa baris: bawaan katalog.
	s, err := Load(ctx, tx, sid)
	if err != nil || !s.Has(Attendance) || s.Has(Exam) {
		t.Fatalf("defaults = %v, %v", s.IDs(), err)
	}
	// Diubah langsung di database: masih dari cache sampai 30 detik lewat.
	if _, err := tx.Exec(ctx, `INSERT INTO school_modules (school_id, module, enabled) VALUES ($1, 'attendance', false)`, sid); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(29 * time.Second)
	if s, _ := Load(ctx, tx, sid); !s.Has(Attendance) {
		t.Error("cache expired before CacheTTL")
	}
	clock = clock.Add(2 * time.Second)
	if s, _ := Load(ctx, tx, sid); s.Has(Attendance) {
		t.Error("change not visible after CacheTTL")
	}
	// Save: langsung terlihat, pilihan tersimpan, audit dipanggil.
	var audited []string
	audit := func(_ context.Context, school, action string, before, after State) error {
		audited = append(audited, action)
		if before.Enabled || !after.Enabled {
			t.Errorf("audit before %+v after %+v", before, after)
		}
		return nil
	}
	if err := Save(ctx, tx, sid, Exam, State{Enabled: true, Options: map[string]string{"release": "r2"}, Note: "pilot"}, "uji", audit); err != nil {
		t.Fatal(err)
	}
	s, _ = Load(ctx, tx, sid)
	if !s.Has(Exam) || s.Option(Exam, "release") != "r2" || !s.HasOption(Exam, "release") || len(audited) != 1 || audited[0] != "module.exam" {
		t.Errorf("after save = %v %q, audit %v", s.IDs(), s.Option(Exam, "release"), audited)
	}
	if st, ok, _ := GetState(ctx, tx, sid, Exam); !ok || st.Note != "pilot" {
		t.Errorf("state = %+v", st)
	}
	if err := Save(ctx, tx, sid, Core, State{}, "uji", nil); err != ErrCore {
		t.Errorf("save core = %v", err)
	}
	if err := Save(ctx, tx, sid, "keuangan", State{}, "uji", nil); err == nil {
		t.Error("save unknown module accepted")
	}
}

// Migrasi 089: sekolah lama mendapat semua modul non-ujian aktif, dan ujian
// dari kunci exam.enabled / exam.release.
func TestMigrationSchoolModules(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	var on, off string
	if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('SD Pilot', 'test-modul-pilot') RETURNING id`).Scan(&on); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO schools (name, code) VALUES ('SD Biasa', 'test-modul-biasa') RETURNING id`).Scan(&off); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO school_settings (school_id, key, value) VALUES ($1, 'exam.enabled', 'true'), ($1, 'exam.release', 'r2')`, on); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../../db/migrations/000089_school_modules.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Bagian data migrasi saja (tabel sudah ada); baris lama dikosongkan dulu
	// di transaksi uji.
	data := string(up)[strings.Index(string(up), "-- Sekolah yang sudah ada"):]
	if _, err := tx.Exec(ctx, `DELETE FROM school_modules`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, data); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		school  string
		exam    bool
		release string
	}{{on, true, "r2"}, {off, false, ""}} {
		Forget(c.school)
		s, err := Load(ctx, tx, c.school)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range Catalog {
			if m.ID != Exam && !s.Has(m.ID) {
				t.Errorf("%s lacks %s", c.school, m.ID)
			}
		}
		if s.Has(Exam) != c.exam || s.Option(Exam, "release") != c.release {
			t.Errorf("exam = %v %q, want %v %q", s.Has(Exam), s.Option(Exam, "release"), c.exam, c.release)
		}
		var n int
		tx.QueryRow(ctx, `SELECT count(*) FROM school_modules WHERE school_id = $1`, c.school).Scan(&n)
		if n != len(Catalog)-1 {
			t.Errorf("%s: %d rows, want %d", c.school, n, len(Catalog)-1)
		}
		Forget(c.school)
	}
}

// Pencocokan route ke modul (T164): per segmen, "*" dan "{...}" untuk satu
// segmen, akhiran ".ext" pada segmen terakhir, dan beberapa modul sekaligus.
func TestRequiredAndOwner(t *testing.T) {
	for _, c := range []struct {
		path, required string
		owner          ID
	}{
		{"/siswa/123", "", Core},
		{"/laporan", "", Core},
		{"/laporan/kehadiran.pdf", "attendance", Attendance},
		{"/laporan/tunggakan.xlsx", "billing", Billing},
		{"/pengaturan/absensi", "attendance", Attendance},
		{"/ujian/paket/1", "exam", Exam},
		{"/u/sekolah-a", "exam", Exam},
		{"/umum", "", Core},
		{"/s/sd-a", "", Core},
		{"/s/sd-a/daftar/isi/1", "admission", Admission},
		{"/portal/siswa/{id}/tagihan", "billing,portal", Billing},
		{"/portal/siswa/9/rapor.pdf", "grades,portal", Grades},
		{"/portal/pengumuman", "announcements,portal", Announcements},
		{"/siswa/{id}/wali/{guardianID}/akun", "portal", Portal},
	} {
		var got []string
		for _, id := range Required(c.path) {
			got = append(got, string(id))
		}
		if strings.Join(got, ",") != c.required || Owner(c.path) != c.owner {
			t.Errorf("%s: required %v owner %s, want %s %s", c.path, got, Owner(c.path), c.required, c.owner)
		}
	}
	if miss := NewSet(Portal).Missing("/portal/siswa/1/tagihan"); len(miss) != 1 || miss[0] != Billing {
		t.Errorf("missing = %v", miss)
	}
}
