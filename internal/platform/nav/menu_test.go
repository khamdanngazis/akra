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

func TestMenuFor(t *testing.T) {
	for role, want := range map[string]string{
		"admin_tu":       "/ /absensi/rekap /siswa /kelas /jadwal /keuangan/tunggakan /keuangan/jenis-tagihan /laporan /pengumuman /keuangan/audit /pengaturan/tahun-ajaran /pengaturan/penugasan /nilai/rekap /nilai/komponen /pengaturan/rapor /pengaturan/mapel",
		"bendahara":      "/ /keuangan/pembayaran/baru /keuangan/tagihan-massal /keuangan/tunggakan /keuangan/jenis-tagihan /laporan /pengumuman /pengaturan/keuangan",
		"guru":           "/ /absensi /jadwal/saya /pengumuman /pengaturan/penugasan /nilai/input /nilai/rekap /nilai/komponen",
		"kepala_sekolah": "/ /absensi/rekap /siswa /kelas /jadwal /keuangan/tunggakan /keuangan/jenis-tagihan /keuangan/pembatalan /laporan /pengumuman /keuangan/audit /pengaturan/keuangan /pengaturan/tahun-ajaran /nilai/rekap",
		"orang_tua":      "/portal /portal/pengumuman",
	} {
		if got := paths(MenuFor([]string{role})); got != want {
			t.Errorf("%s:\n got %s\nwant %s", role, got, want)
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
	for _, p := range []string{"/keuangan", "/siswa", "/laporan"} {
		if strings.Contains(guru, p) {
			t.Errorf("teacher menu contains %s: %s", p, guru)
		}
	}
	if b := paths(MenuFor([]string{"bendahara"})); strings.Contains(b, "/absensi") {
		t.Errorf("treasurer menu contains attendance: %s", b)
	}
	if p := paths(MenuFor([]string{"orang_tua"})); strings.Contains(p, "/keuangan") || strings.Contains(p, "/pengumuman ") || strings.HasPrefix(p, "/ ") {
		t.Errorf("parent menu leaks staff items: %s", p)
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
	if len(got) != len(MenuFor([]string{"bendahara"}))+6 { // + /absensi, /jadwal/saya, /pengaturan/penugasan, /nilai/input, /nilai/rekap, /nilai/komponen
		t.Errorf("merged menu = %s", paths(got))
	}
	// Urutan mengikuti Items, bukan urutan peran.
	if paths(MenuFor([]string{"bendahara", "guru"})) != paths(got) {
		t.Error("menu order depends on role order")
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
		"/pengaturan/tahun-ajaran":   "/pengaturan/tahun-ajaran",
		"/siswax":                    "",
		"/impor/siswa":               "/siswa",
		"/impor/siswa/template.xlsx": "/siswa",
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

func TestBottomAndShortcuts(t *testing.T) {
	kepala := MenuFor([]string{"kepala_sekolah"})
	if got := Bottom(kepala); len(got) != 4 || got[0].Path != "/" {
		t.Errorf("Bottom = %s", paths(got))
	}
	sc := Shortcuts(kepala)
	if len(sc) != MaxShortcuts || strings.Contains(paths(sc), "/ ") || sc[0].Path == "/" {
		t.Errorf("Shortcuts = %s", paths(sc))
	}
	parent := MenuFor([]string{"orang_tua"})
	if got := Bottom(parent); len(got) != len(parent) {
		t.Errorf("parent Bottom = %s", paths(got))
	}
	for _, it := range kepala {
		if it.ShortLabel() == "" || len(it.ShortLabel()) > 10 {
			t.Errorf("short label of %s = %q", it.Path, it.ShortLabel())
		}
	}
}
