// Package nav menentukan menu navigasi per peran. Menyembunyikan menu
// bukan kontrol akses: setiap route tetap dilindungi RequireRole.
package nav

import "strings"

// Kode peran (sama dengan auth.Role*; paket platform tidak mengimpor auth).
const (
	adminTU   = "admin_tu"
	bendahara = "bendahara"
	guru      = "guru"
	kepala    = "kepala_sekolah"
	orangTua  = "orang_tua"
)

// MenuItem adalah satu tautan menu.
type MenuItem struct {
	Label string
	Path  string
	// Icon adalah nama ikon di sprite (partials/icons.html).
	Icon string
	// Short adalah label bottom navigation di HP (kosong = Label).
	Short string
	// Desc adalah keterangan singkat di kartu pintasan beranda.
	Desc string
	// Also adalah path lain yang juga mengaktifkan item ini, untuk halaman
	// di luar prefix Path (mis. /impor/siswa milik menu Siswa).
	Also  []string
	Roles []string
}

// ShortLabel mengembalikan label pendek item (untuk bottom navigation).
func (m MenuItem) ShortLabel() string {
	if m.Short != "" {
		return m.Short
	}
	return m.Label
}

// Items adalah seluruh menu dalam urutan tampil. Satu Path hanya muncul
// sekali; peran yang boleh membukanya tercantum di Roles.
var Items = []MenuItem{
	{Label: "Beranda", Path: "/", Icon: "dashboard", Roles: []string{adminTU, bendahara, guru, kepala}},
	{Label: "Anak Saya", Path: "/portal", Icon: "child", Short: "Anak", Desc: "Profil, absensi, dan tagihan anak", Roles: []string{orangTua}},
	{Label: "Absensi", Path: "/absensi", Icon: "attendance", Desc: "Isi absensi kelas hari ini", Roles: []string{guru}},
	{Label: "Rekap Absensi", Path: "/absensi/rekap", Icon: "chart", Short: "Rekap", Desc: "Rekap kehadiran per kelas", Roles: []string{adminTU, kepala}},
	{Label: "Siswa", Path: "/siswa", Icon: "users", Desc: "Kelola data siswa dan wali", Also: []string{"/impor/siswa"}, Roles: []string{adminTU, kepala}},
	{Label: "Kelas", Path: "/kelas", Icon: "school", Desc: "Kelas dan wali kelas", Roles: []string{adminTU, kepala}},
	{Label: "Jadwal", Path: "/jadwal", Icon: "schedule", Desc: "Jadwal pelajaran mingguan per kelas", Roles: []string{adminTU, kepala}},
	{Label: "Jadwal Saya", Path: "/jadwal/saya", Icon: "schedule", Short: "Jadwal", Desc: "Jadwal mengajar minggu ini", Roles: []string{guru}},
	{Label: "Pembayaran", Path: "/keuangan/pembayaran/baru", Icon: "wallet", Short: "Bayar", Desc: "Catat pembayaran siswa", Roles: []string{bendahara}},
	{Label: "Tagihan Massal", Path: "/keuangan/tagihan-massal", Icon: "receipt", Short: "Tagihan", Desc: "Buat tagihan untuk satu kelas", Roles: []string{bendahara}},
	{Label: "Tunggakan", Path: "/keuangan/tunggakan", Icon: "arrears", Desc: "Tagihan yang lewat jatuh tempo", Roles: []string{bendahara, adminTU, kepala}},
	{Label: "Jenis Tagihan", Path: "/keuangan/jenis-tagihan", Icon: "tags", Short: "Jenis", Desc: "SPP dan jenis biaya lain", Roles: []string{bendahara, adminTU, kepala}},
	{Label: "Pembatalan", Path: "/keuangan/pembatalan", Icon: "undo", Desc: "Persetujuan pembatalan pembayaran", Roles: []string{kepala}},
	{Label: "Laporan", Path: "/laporan", Icon: "report", Desc: "Unduh laporan Excel dan PDF", Roles: []string{adminTU, bendahara, kepala}},
	{Label: "Pengumuman", Path: "/pengumuman", Icon: "megaphone", Short: "Info", Desc: "Pengumuman untuk orang tua", Roles: []string{adminTU, kepala, bendahara, guru}},
	{Label: "Pengumuman", Path: "/portal/pengumuman", Icon: "megaphone", Short: "Info", Desc: "Informasi dari sekolah", Roles: []string{orangTua}},
	{Label: "Audit", Path: "/keuangan/audit", Icon: "shield", Desc: "Jejak setiap aksi keuangan", Roles: []string{kepala, adminTU}},
	{Label: "Pengaturan Keuangan", Path: "/pengaturan/keuangan", Icon: "settings", Short: "Atur", Desc: "Ambang persetujuan pembatalan", Roles: []string{bendahara, kepala}},
	{Label: "Tahun Ajaran", Path: "/pengaturan/tahun-ajaran", Icon: "calendar", Short: "Tahun", Desc: "Atur tahun ajaran aktif", Roles: []string{adminTU, kepala}},
	{Label: "Penugasan Guru", Path: "/pengaturan/penugasan", Icon: "user-check", Short: "Mapel", Desc: "Guru pengampu mapel per kelas", Roles: []string{adminTU, guru}},
	{Label: "Komponen Nilai", Path: "/nilai/komponen", Icon: "percent", Short: "Nilai", Desc: "Komponen dan bobot nilai per mapel", Roles: []string{adminTU, guru}},
	{Label: "Mata Pelajaran", Path: "/pengaturan/mapel", Icon: "teacher", Short: "Mapel", Desc: "Daftar mata pelajaran sekolah", Roles: []string{adminTU}},
}

// MenuFor mengembalikan item menu untuk pengguna dengan peran roles, dalam
// urutan Items. Pengguna dengan beberapa peran mendapat gabungan menu tanpa
// duplikat.
func MenuFor(roles []string) []MenuItem {
	has := make(map[string]bool, len(roles))
	for _, r := range roles {
		has[r] = true
	}
	var out []MenuItem
	seen := map[string]bool{}
	for _, it := range Items {
		if seen[it.Path] {
			continue
		}
		for _, r := range it.Roles {
			if has[r] {
				out = append(out, it)
				seen[it.Path] = true
				break
			}
		}
	}
	return out
}

// Bottom mengembalikan item bottom navigation di HP: maksimal empat item
// pertama menu. Sisanya ada di drawer (tombol "Menu").
func Bottom(items []MenuItem) []MenuItem {
	if len(items) > 4 {
		return items[:4]
	}
	return items
}

// MaxShortcuts adalah jumlah kartu pintasan maksimal di beranda.
const MaxShortcuts = 6

// Shortcuts mengembalikan item untuk kartu pintasan beranda: item yang punya
// keterangan, tanpa Beranda, paling banyak MaxShortcuts sesuai urutan menu.
func Shortcuts(items []MenuItem) []MenuItem {
	var out []MenuItem
	for _, it := range items {
		if len(out) == MaxShortcuts {
			break
		}
		if it.Desc != "" && it.Path != "/" {
			out = append(out, it)
		}
	}
	return out
}

// Current mengembalikan Path item menu yang aktif untuk path request: sama
// persis, atau path berada di bawahnya (prefix terpanjang menang). Beranda
// hanya aktif di halamannya sendiri. Kosong jika tidak ada.
func Current(items []MenuItem, path string) string {
	best, bestLen := "", 0
	for _, it := range items {
		if it.Path == path {
			return it.Path
		}
		if it.Path == "/" {
			continue
		}
		for _, p := range append([]string{it.Path}, it.Also...) {
			if (path == p || strings.HasPrefix(path, p+"/")) && len(p) > bestLen {
				best, bestLen = it.Path, len(p)
			}
		}
	}
	return best
}

var roleLabels = map[string]string{
	adminTU: "Admin/TU", bendahara: "Bendahara", guru: "Guru", kepala: "Kepala Sekolah", orangTua: "Orang Tua",
}

// RoleLabel menggabungkan nama peran untuk tampilan, mis. "Guru · Bendahara".
func RoleLabel(roles []string) string {
	out := ""
	for _, r := range roles {
		l, ok := roleLabels[r]
		if !ok {
			continue
		}
		if out != "" {
			out += " · "
		}
		out += l
	}
	return out
}

// Initials mengembalikan inisial nama (maks. dua huruf), mis. "Siti Aminah"
// menjadi "SA".
func Initials(name string) string {
	out := []rune{}
	start := true
	for _, r := range name {
		switch {
		case r == ' ' || r == '.' || r == '-':
			start = true
		case start:
			out = append(out, r)
			start = false
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return strings.ToUpper(string(out))
}
