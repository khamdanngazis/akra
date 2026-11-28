package nav

import (
	"strings"
	"testing"
)

func paths(items []MenuItem) string {
	var s []string
	for _, it := range items {
		s = append(s, it.Path)
	}
	return strings.Join(s, " ")
}

// exam: modul bawaan ditambah ujian.
var exam = append(append([]string(nil), DefaultModules...), ModuleExam)

// Menu per peran (analisis menu bagian 3, T156): urutan mengikuti kelompok.
func TestMenuFor(t *testing.T) {
	for role, want := range map[string]string{
		"admin_tu":       "/ /siswa /kelas /jadwal /absensi/rekap /nilai/rekap /nilai/komponen /pendaftaran /keuangan/tunggakan /keuangan/jenis-tagihan /keuangan/audit /dapodik /pengumuman /laporan /pengaturan",
		"bendahara":      "/ /pendaftaran/biaya /keuangan/pembayaran/baru /keuangan/tagihan-massal /keuangan/rekap-pembayaran /keuangan/tunggakan /keuangan/jenis-tagihan /pengumuman /laporan /pengaturan",
		"guru":           "/ /jadwal/saya /absensi /nilai/input /nilai/rekap /nilai/komponen /pengaturan/penugasan /dapodik/nilai /pengumuman",
		"kepala_sekolah": "/ /siswa /kelas /jadwal /absensi/rekap /nilai/rekap /pendaftaran /keuangan/rekap-pembayaran /keuangan/tunggakan /keuangan/jenis-tagihan /keuangan/pembatalan /keuangan/audit /dapodik /pengumuman /laporan /pengaturan",
		"orang_tua":      "/portal /portal/pengumuman",
	} {
		if got := paths(MenuFor([]string{role})); got != want {
			t.Errorf("%s:\n got %s\nwant %s", role, got, want)
		}
	}
	// Dengan modul ujian: jumlah sesuai analisis menu. Telaah Soal ada di
	// sub-navigasi modul ujian (T157).
	for role, want := range map[string]int{"admin_tu": 17, "kepala_sekolah": 18, "guru": 10, "bendahara": 10} {
		if got := len(MenuForUser([]string{role}, nil, exam)); got != want {
			t.Errorf("%s with exam: %d items, want %d", role, got, want)
		}
	}
	if got := MenuFor(nil); len(got) != 0 {
		t.Errorf("no roles: %v", got)
	}
	if got := MenuFor([]string{"tidak_dikenal"}); len(got) != 0 {
		t.Errorf("unknown role: %v", got)
	}
}

func TestMenuFor_Restrictions(t *testing.T) {
	guru := paths(MenuFor([]string{"guru"}))
	for _, p := range []string{"/keuangan", "/siswa", "/laporan", "/pengaturan "} {
		if strings.Contains(guru+" ", p) {
			t.Errorf("teacher menu contains %s: %s", p, guru)
		}
	}
	if b := paths(MenuFor([]string{"bendahara"})); strings.Contains(b, "/absensi") {
		t.Errorf("treasurer menu contains attendance: %s", b)
	}
	if p := paths(MenuFor([]string{"orang_tua"})); strings.Contains(p, "/keuangan") || strings.Contains(p, "/pengumuman ") || strings.HasPrefix(p, "/ ") {
		t.Errorf("parent menu leaks staff items: %s", p)
	}
	// Pengaturan sekolah tidak lagi di menu: dibuka dari pusat Pengaturan.
	for _, role := range []string{"admin_tu", "kepala_sekolah", "bendahara"} {
		for _, it := range MenuForUser([]string{role}, nil, exam) {
			if strings.HasPrefix(it.Path, "/pengaturan/") || it.Path == "/ujian/pengaturan" {
				t.Errorf("%s menu still has settings page %s", role, it.Path)
			}
		}
	}
}

func TestMenuFor_TwoRolesMergedWithoutDuplicates(t *testing.T) {
	got := MenuFor([]string{"guru", "bendahara"})
	seen := map[string]bool{}
	for _, it := range got {
		if seen[it.Path] {
			t.Errorf("duplicate %s", it.Path)
		}
		seen[it.Path] = true
	}
	for _, p := range []string{"/", "/absensi", "/keuangan/pembayaran/baru", "/pengumuman"} {
		if !seen[p] {
			t.Errorf("merged menu misses %s", p)
		}
	}
	if len(got) != len(MenuFor([]string{"bendahara"}))+7 { // + /jadwal/saya, /absensi, /nilai/input, /nilai/rekap, /nilai/komponen, /pengaturan/penugasan, /dapodik/nilai
		t.Errorf("merged menu = %s", paths(got))
	}
	// Urutan mengikuti Items, bukan urutan peran.
	if paths(MenuFor([]string{"bendahara", "guru"})) != paths(got) {
		t.Error("menu order depends on role order")
	}
}

// Item satu kelompok bersebelahan, kelompok sesuai GroupOrder, dan setiap
// item punya modul yang dikenal (kosong hanya untuk inti).
func TestItemsGroupedAndModules(t *testing.T) {
	rank := map[string]int{}
	for i, g := range GroupOrder {
		rank[g] = i
	}
	known := map[string]bool{ModuleAttendance: true, ModuleSchedule: true, ModuleGrades: true, ModuleBilling: true, ModuleAdmission: true,
		ModuleDapodik: true, ModuleExam: true, ModuleAnnouncements: true, ModulePortal: true}
	core := map[string]bool{"/": true, "/siswa": true, "/kelas": true, "/pengaturan/penugasan": true, "/laporan": true, "/pengaturan": true}
	last := -1
	for _, it := range Items {
		r, ok := rank[it.Group]
		if !ok {
			t.Errorf("%s: unknown group %q", it.Path, it.Group)
		}
		if r < last {
			t.Errorf("%s: group %q out of order", it.Path, it.Group)
		}
		last = r
		if it.Module == "" && !core[it.Path] {
			t.Errorf("%s has no module", it.Path)
		}
		if it.Module != "" && !known[it.Module] {
			t.Errorf("%s: unknown module %q", it.Path, it.Module)
		}
		if l := it.ShortLabel(); l == "" || len(l) > 10 {
			t.Errorf("short label of %s = %q", it.Path, l)
		}
	}
}

func TestGroups(t *testing.T) {
	g := Groups(MenuForUser([]string{"admin_tu"}, nil, exam))
	var names, titled []string
	for _, x := range g {
		names = append(names, x.Name)
		if x.Titled() {
			titled = append(titled, x.Name)
		}
	}
	if got := strings.Join(names, "|"); got != "|Akademik|Ujian|Pendaftaran|Keuangan|Dapodik|Informasi|Pengaturan" {
		t.Errorf("groups = %s", got)
	}
	// Kelompok satu item (Pendaftaran, Dapodik, Pengaturan) dan Beranda tanpa judul.
	if got := strings.Join(titled, "|"); got != "Akademik|Ujian|Keuangan|Informasi" {
		t.Errorf("titled = %s", got)
	}
	// Kelompok kosong tidak ada: orang tua hanya dua kelompok tanpa judul.
	for _, x := range Groups(MenuFor([]string{"orang_tua"})) {
		if x.Titled() || len(x.Items) == 0 {
			t.Errorf("parent group %+v", x)
		}
	}
}

func TestCurrent(t *testing.T) {
	items := MenuFor([]string{"admin_tu", "guru"})
	for path, want := range map[string]string{
		"/":                          "/",
		"/siswa":                     "/siswa",
		"/siswa/123/ubah":            "/siswa",
		"/absensi":                   "/absensi",
		"/absensi/rekap":             "/absensi/rekap",
		"/keuangan/tagihan/1":        "",
		"/laporan/kehadiran":         "/laporan",
		"/pengaturan":                "/pengaturan",
		"/pengaturan/tahun-ajaran":   "/pengaturan",
		"/pengaturan/sekolah":        "/pengaturan",
		"/pengaturan/absensi":        "/pengaturan",
		"/pengaturan/penugasan":      "/pengaturan/penugasan",
		"/pendaftaran/pengaturan":    "/pengaturan",
		"/pendaftaran/rekap":         "/pendaftaran",
		"/siswax":                    "",
		"/impor/siswa":               "/siswa",
		"/impor/siswa/template.xlsx": "/siswa",
		"/impor/nilai":               "/nilai/rekap",
	} {
		if got := Current(items, path); got != want {
			t.Errorf("Current(%q) = %q, want %q", path, got, want)
		}
	}
	parent := MenuFor([]string{"orang_tua"})
	if Current(parent, "/portal/siswa/1/tagihan") != "/portal" || Current(parent, "/portal/pengumuman") != "/portal/pengumuman" {
		t.Error("portal current item")
	}
}

// Bottom navigation dan pintasan beranda per peran, dengan cadangan saat
// modul ujian nonaktif.
func TestBottomAndShortcuts(t *testing.T) {
	for _, c := range []struct {
		roles   []string
		modules []string
		bottom  string
	}{
		{[]string{"admin_tu"}, exam, "/ /siswa /keuangan/tunggakan /ujian"},
		{[]string{"admin_tu"}, DefaultModules, "/ /siswa /keuangan/tunggakan /absensi/rekap"},
		{[]string{"kepala_sekolah"}, exam, "/ /absensi/rekap /laporan /keuangan/tunggakan"},
		{[]string{"guru"}, exam, "/ /absensi /nilai/input /ujian"},
		{[]string{"guru"}, DefaultModules, "/ /absensi /nilai/input /jadwal/saya"},
		{[]string{"bendahara"}, DefaultModules, "/ /keuangan/pembayaran/baru /keuangan/tunggakan /keuangan/rekap-pembayaran"},
		{[]string{"orang_tua"}, DefaultModules, "/portal /portal/pengumuman"},
		// Modul item utama mati (T165): item berikutnya menjadi cadangan.
		{[]string{"admin_tu"}, []string{ModuleAttendance, ModuleAnnouncements}, "/ /siswa /absensi/rekap /pengumuman"},
		{[]string{"guru"}, []string{ModuleGrades, ModuleSchedule}, "/ /nilai/input /jadwal/saya"},
		// Beberapa peran: peran berprioritas tertinggi didahulukan.
		{[]string{"bendahara", "guru"}, exam, "/ /absensi /nilai/input /ujian"},
	} {
		items := MenuForUser(c.roles, nil, c.modules)
		if got := paths(Bottom(items, c.roles)); got != c.bottom {
			t.Errorf("Bottom(%v, %v) = %s, want %s", c.roles, c.modules, got, c.bottom)
		}
	}
	guru := []string{"guru"}
	sc := Shortcuts(MenuForUser(guru, nil, exam), guru)
	if got := paths(sc); got != "/absensi /nilai/input /ujian /jadwal/saya /nilai/rekap /pengumuman" {
		t.Errorf("teacher shortcuts = %s", got)
	}
	for _, role := range []string{"admin_tu", "kepala_sekolah", "bendahara"} {
		sc := Shortcuts(MenuForUser([]string{role}, nil, DefaultModules), []string{role})
		if len(sc) != MaxShortcuts || strings.HasPrefix(paths(sc), "/ ") {
			t.Errorf("%s shortcuts = %s", role, paths(sc))
		}
	}
	if sc := Shortcuts(MenuFor([]string{"orang_tua"}), []string{"orang_tua"}); paths(sc) != "/portal /portal/pengumuman" {
		t.Errorf("parent shortcuts = %s", paths(sc))
	}
}

// Isi pusat Pengaturan per peran (T156), mengikuti hak akses route.
func TestSettingsFor(t *testing.T) {
	show := func(roles, duties, modules []string) string {
		var parts []string
		for _, g := range SettingsFor(roles, duties, modules) {
			parts = append(parts, g.Name+":"+paths(g.Items))
		}
		return strings.Join(parts, " | ")
	}
	for _, c := range []struct {
		roles, duties, modules []string
		want                   string
	}{
		{[]string{"admin_tu"}, nil, DefaultModules, "Sekolah:/pengaturan/sekolah /pengaturan/tahun-ajaran /pengaturan/mapel /pengaturan/penugasan | Absensi:/pengaturan/absensi | Nilai & Rapor:/pengaturan/rapor | Keuangan:/pengaturan/keuangan | Pendaftaran:/pendaftaran/pengaturan"},
		{[]string{"admin_tu"}, nil, exam, "Sekolah:/pengaturan/sekolah /pengaturan/tahun-ajaran /pengaturan/mapel /pengaturan/penugasan /pengaturan/tugas | Absensi:/pengaturan/absensi | Nilai & Rapor:/pengaturan/rapor | Keuangan:/pengaturan/keuangan | Pendaftaran:/pendaftaran/pengaturan | Ujian:/ujian/pengaturan"},
		{[]string{"kepala_sekolah"}, nil, exam, "Sekolah:/pengaturan/sekolah /pengaturan/tahun-ajaran /pengaturan/tugas | Keuangan:/pengaturan/keuangan | Pendaftaran:/pendaftaran/pengaturan"},
		// Tugas koordinator hanya menambah hak guru (sama dengan matriks
		// akses ujian): kepala sekolah koordinator tetap tanpa Pengaturan Ujian.
		{[]string{"kepala_sekolah"}, []string{"koordinator_ujian"}, exam, "Sekolah:/pengaturan/sekolah /pengaturan/tahun-ajaran /pengaturan/tugas | Keuangan:/pengaturan/keuangan | Pendaftaran:/pendaftaran/pengaturan"},
		{[]string{"bendahara"}, nil, DefaultModules, "Keuangan:/pengaturan/keuangan"},
		// Keuangan nonaktif (T163): bagian Keuangan hilang.
		{[]string{"bendahara"}, nil, []string{ModuleAdmission}, ""},
		{[]string{"guru"}, nil, exam, "Sekolah:/pengaturan/penugasan"},
		{[]string{"guru"}, []string{"koordinator_ujian"}, exam, "Sekolah:/pengaturan/penugasan | Ujian:/ujian/pengaturan"},
		{[]string{"orang_tua"}, nil, exam, ""},
	} {
		if got := show(c.roles, c.duties, c.modules); got != c.want {
			t.Errorf("SettingsFor(%v, %v, %v):\n got %s\nwant %s", c.roles, c.duties, c.modules, got, c.want)
		}
	}
}

// Menu modul ujian (T125): hanya tampil bila modul aktif; "Agenda Ujian"
// juga untuk guru dengan tugas koordinator_ujian.
func TestMenuForUser_ModuleAndDuty(t *testing.T) {
	has := func(items []MenuItem, path string) bool {
		for _, it := range items {
			if it.Path == path {
				return true
			}
		}
		return false
	}
	guru := []string{"guru"}
	if m := MenuForUser(guru, []string{"koordinator_ujian"}, DefaultModules); has(m, "/ujian") || has(m, "/ujian/agenda") {
		t.Error("exam menu shown while module is off")
	}
	m := MenuForUser(guru, nil, exam)
	if !has(m, "/ujian") || has(m, "/ujian/agenda") || has(m, "/pengaturan") {
		t.Errorf("teacher with module on: %s", paths(m))
	}
	// Koordinator: Agenda Ujian dan pusat Pengaturan (untuk Pengaturan Ujian).
	m = MenuForUser(guru, []string{"koordinator_ujian"}, exam)
	if !has(m, "/ujian/agenda") || !has(m, "/pengaturan") {
		t.Errorf("coordinator menu: %s", paths(m))
	}
	if m := MenuForUser([]string{"admin_tu"}, nil, exam); !has(m, "/ujian/agenda") {
		t.Errorf("admin with module on: %s", paths(m))
	}
	// Semua modul disaring per sekolah (T163). Bendahara tanpa keuangan dan
	// pendaftaran (T166): hanya Beranda dan Pengumuman; hub Laporan dan
	// Pengaturan yang kosong ikut hilang.
	if m := MenuForUser([]string{"bendahara"}, nil, []string{ModuleAnnouncements}); paths(m) != "/ /pengumuman" {
		t.Errorf("treasurer without billing: %s", paths(m))
	}
	if m := MenuForUser([]string{"bendahara"}, nil, []string{ModuleAdmission, ModuleAnnouncements}); paths(m) != "/ /pendaftaran/biaya /pengumuman" {
		t.Errorf("treasurer with admission only: %s", paths(m))
	}
	// Nilai ke Dapodik butuh modul nilai (T166).
	if m := MenuForUser(guru, nil, []string{ModuleDapodik}); has(m, "/dapodik/nilai") {
		t.Errorf("Nilai ke Dapodik without grades: %s", paths(m))
	}
	// MenuFor sama dengan MenuForUser tanpa tugas dengan modul bawaan.
	if got, want := paths(MenuFor(guru)), paths(MenuForUser(guru, nil, DefaultModules)); got != want {
		t.Errorf("MenuFor = %s, want %s", got, want)
	}
}
