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
	Icon  string
	Roles []string
}

// Items adalah seluruh menu dalam urutan tampil. Satu Path hanya muncul
// sekali; peran yang boleh membukanya tercantum di Roles.
var Items = []MenuItem{
	{Label: "Beranda", Path: "/", Icon: "home", Roles: []string{adminTU, bendahara, guru, kepala}},
	{Label: "Anak Saya", Path: "/portal", Icon: "child", Roles: []string{orangTua}},
	{Label: "Absensi", Path: "/absensi", Icon: "attendance", Roles: []string{guru}},
	{Label: "Rekap Absensi", Path: "/absensi/rekap", Icon: "chart", Roles: []string{adminTU, kepala}},
	{Label: "Siswa", Path: "/siswa", Icon: "users", Roles: []string{adminTU, kepala}},
	{Label: "Kelas", Path: "/kelas", Icon: "school", Roles: []string{adminTU, kepala}},
	{Label: "Pembayaran", Path: "/keuangan/pembayaran/baru", Icon: "wallet", Roles: []string{bendahara}},
	{Label: "Tagihan Massal", Path: "/keuangan/tagihan-massal", Icon: "receipt", Roles: []string{bendahara}},
	{Label: "Tunggakan", Path: "/keuangan/tunggakan", Icon: "arrears", Roles: []string{bendahara, adminTU, kepala}},
	{Label: "Jenis Tagihan", Path: "/keuangan/jenis-tagihan", Icon: "tags", Roles: []string{bendahara, adminTU, kepala}},
	{Label: "Pembatalan", Path: "/keuangan/pembatalan", Icon: "undo", Roles: []string{kepala}},
	{Label: "Laporan", Path: "/laporan", Icon: "report", Roles: []string{adminTU, bendahara, kepala}},
	{Label: "Pengumuman", Path: "/pengumuman", Icon: "megaphone", Roles: []string{adminTU, kepala, bendahara, guru}},
	{Label: "Pengumuman", Path: "/portal/pengumuman", Icon: "megaphone", Roles: []string{orangTua}},
	{Label: "Audit", Path: "/keuangan/audit", Icon: "shield", Roles: []string{kepala, adminTU}},
	{Label: "Pengaturan Keuangan", Path: "/pengaturan/keuangan", Icon: "settings", Roles: []string{bendahara, kepala}},
	{Label: "Tahun Ajaran", Path: "/pengaturan/tahun-ajaran", Icon: "calendar", Roles: []string{adminTU, kepala}},
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

// Current mengembalikan Path item menu yang aktif untuk path request: sama
// persis, atau path berada di bawahnya (prefix terpanjang menang). Beranda
// hanya aktif di halamannya sendiri. Kosong jika tidak ada.
func Current(items []MenuItem, path string) string {
	best := ""
	for _, it := range items {
		switch {
		case it.Path == path:
			return it.Path
		case it.Path == "/":
			continue
		case len(path) > len(it.Path) && path[:len(it.Path)] == it.Path && path[len(it.Path)] == '/' && len(it.Path) > len(best):
			best = it.Path
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
