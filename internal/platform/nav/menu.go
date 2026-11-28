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

	// Kode tugas tambahan (sama dengan duty.ExamCoordinator).
	koordinatorUjian = "koordinator_ujian"
)

// dutyRole adalah peran yang dihidupkan haknya oleh tugas tambahan. Sama
// dengan matriks akses ujian: kolom koordinator hanya berlaku untuk guru,
// jadi kepala sekolah yang ditugasi koordinator tetap hanya membaca.
var dutyRole = map[string]string{koordinatorUjian: guru}

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
	// Duties adalah tugas tambahan yang juga memunculkan item ini (T125),
	// mis. guru dengan tugas koordinator_ujian.
	Duties []string
	// Module adalah modul pemilik item (ADR 0002). Item modul yang
	// tidak aktif untuk sekolah tidak tampil. Kosong berarti inti.
	Module string
	// Group adalah kelompok menu (T156); item dalam satu kelompok
	// bersebelahan di Items, kelompok berurutan sesuai GroupOrder.
	Group string
}

// Kelompok menu (T156), dalam urutan tampil. Kelompok tanpa nama berisi
// Beranda/Anak Saya di paling atas.
const (
	GroupHome        = ""
	GroupAcademic    = "Akademik"
	GroupExam        = "Ujian"
	GroupAdmission   = "Pendaftaran"
	GroupFinance     = "Keuangan"
	GroupDapodik     = "Dapodik"
	GroupInformation = "Informasi"
	GroupSettings    = "Pengaturan"
)

// GroupOrder adalah urutan kelompok di menu.
var GroupOrder = []string{GroupHome, GroupAcademic, GroupExam, GroupAdmission, GroupFinance, GroupDapodik, GroupInformation, GroupSettings}

// Modul (ADR 0002), sama dengan kode di katalog internal/platform/module.
// Item menu modul yang tidak aktif untuk sekolah disembunyikan (T163).
const (
	ModuleAttendance    = "attendance"
	ModuleSchedule      = "schedule"
	ModuleGrades        = "grades"
	ModuleBilling       = "billing"
	ModuleAdmission     = "admission"
	ModuleDapodik       = "dapodik"
	ModuleExam          = "exam"
	ModuleAnnouncements = "announcements"
	ModulePortal        = "portal"
)

// ShortLabel mengembalikan label pendek item (untuk bottom navigation).
func (m MenuItem) ShortLabel() string {
	if m.Short != "" {
		return m.Short
	}
	return m.Label
}

// Items adalah seluruh menu dalam urutan tampil, dikelompokkan sesuai
// GroupOrder (analisis menu, T156). Satu Path hanya muncul sekali per
// pengguna; peran yang boleh membukanya tercantum di Roles. Pengaturan
// sekolah tidak ada di sini: semuanya dibuka dari pusat Pengaturan
// (SettingsItems).
var Items = []MenuItem{
	{Label: "Beranda", Path: "/", Icon: "dashboard", Roles: []string{adminTU, bendahara, guru, kepala}},
	{Label: "Anak Saya", Path: "/portal", Icon: "child", Short: "Anak", Desc: "Profil, absensi, dan tagihan anak", Module: ModulePortal, Roles: []string{orangTua}},

	{Label: "Siswa", Path: "/siswa", Icon: "users", Desc: "Kelola data siswa dan wali", Also: []string{"/impor/siswa"}, Group: GroupAcademic, Roles: []string{adminTU, kepala}},
	{Label: "Kelas", Path: "/kelas", Icon: "school", Desc: "Kelas dan wali kelas", Group: GroupAcademic, Roles: []string{adminTU, kepala}},
	{Label: "Jadwal", Path: "/jadwal", Icon: "schedule", Desc: "Jadwal pelajaran mingguan per kelas", Module: ModuleSchedule, Group: GroupAcademic, Roles: []string{adminTU, kepala}},
	{Label: "Jadwal Saya", Path: "/jadwal/saya", Icon: "schedule", Short: "Jadwal", Desc: "Jadwal mengajar minggu ini", Module: ModuleSchedule, Group: GroupAcademic, Roles: []string{guru}},
	{Label: "Absensi", Path: "/absensi", Icon: "attendance", Desc: "Isi absensi kelas hari ini", Module: ModuleAttendance, Group: GroupAcademic, Roles: []string{guru}},
	{Label: "Rekap Absensi", Path: "/absensi/rekap", Icon: "chart", Short: "Absensi", Desc: "Rekap kehadiran per kelas", Module: ModuleAttendance, Group: GroupAcademic, Roles: []string{adminTU, kepala}},
	{Label: "Input Nilai", Path: "/nilai/input", Icon: "spreadsheet", Short: "Nilai", Desc: "Isi nilai siswa per komponen", Module: ModuleGrades, Group: GroupAcademic, Roles: []string{guru}},
	{Label: "Rekap Nilai", Path: "/nilai/rekap", Icon: "report", Short: "Nilai", Desc: "Nilai akhir per mapel dan kelas", Also: []string{"/impor/nilai"}, Module: ModuleGrades, Group: GroupAcademic, Roles: []string{adminTU, kepala, guru}},
	{Label: "Komponen Nilai", Path: "/nilai/komponen", Icon: "percent", Short: "Bobot", Desc: "Komponen dan bobot nilai per mapel", Module: ModuleGrades, Group: GroupAcademic, Roles: []string{adminTU, guru}},
	{Label: "Mapel Saya", Path: "/pengaturan/penugasan", Icon: "user-check", Short: "Mapel", Desc: "Mapel dan kelas yang Anda ampu", Group: GroupAcademic, Roles: []string{guru}},

	{Label: "Ujian", Path: "/ujian", Icon: "file-text", Desc: "Bank soal, ujian, dan hasil", Module: ModuleExam, Group: GroupExam, Roles: []string{adminTU, kepala, guru}},
	{Label: "Agenda Ujian", Path: "/ujian/agenda", Icon: "calendar", Short: "Agenda", Desc: "PTS/PAS: ruang, sesi, dan pengawas", Module: ModuleExam, Group: GroupExam, Roles: []string{adminTU, kepala}, Duties: []string{koordinatorUjian}},

	{Label: "Pendaftaran", Path: "/pendaftaran", Icon: "user-check", Short: "Daftar", Desc: "Periode, jalur, dan pendaftar siswa baru", Module: ModuleAdmission, Group: GroupAdmission, Roles: []string{adminTU, kepala}},
	{Label: "Biaya Pendaftaran", Path: "/pendaftaran/biaya", Icon: "receipt", Short: "Formulir", Desc: "Catat biaya formulir pendaftar", Module: ModuleAdmission, Group: GroupAdmission, Roles: []string{bendahara}},

	{Label: "Catat Pembayaran", Path: "/keuangan/pembayaran/baru", Icon: "wallet", Short: "Bayar", Desc: "Catat pembayaran siswa", Module: ModuleBilling, Group: GroupFinance, Roles: []string{bendahara}},
	{Label: "Tagihan Massal", Path: "/keuangan/tagihan-massal", Icon: "receipt", Short: "Tagihan", Desc: "Buat tagihan untuk satu kelas", Module: ModuleBilling, Group: GroupFinance, Roles: []string{bendahara}},
	{Label: "Rekap Pembayaran", Path: "/keuangan/rekap-pembayaran", Icon: "money", Short: "Rekap", Desc: "Total pembayaran harian dan bulanan", Module: ModuleBilling, Group: GroupFinance, Roles: []string{bendahara, kepala}},
	{Label: "Tunggakan", Path: "/keuangan/tunggakan", Icon: "arrears", Desc: "Tagihan yang lewat jatuh tempo", Module: ModuleBilling, Group: GroupFinance, Roles: []string{bendahara, adminTU, kepala}},
	{Label: "Jenis Tagihan", Path: "/keuangan/jenis-tagihan", Icon: "tags", Short: "Jenis", Desc: "SPP dan jenis biaya lain", Module: ModuleBilling, Group: GroupFinance, Roles: []string{bendahara, adminTU, kepala}},
	{Label: "Pembatalan", Path: "/keuangan/pembatalan", Icon: "undo", Desc: "Persetujuan pembatalan pembayaran", Module: ModuleBilling, Group: GroupFinance, Roles: []string{kepala}},
	{Label: "Audit Keuangan", Path: "/keuangan/audit", Icon: "shield", Short: "Audit", Desc: "Jejak setiap aksi keuangan", Module: ModuleBilling, Group: GroupFinance, Roles: []string{kepala, adminTU}},

	{Label: "Dapodik", Path: "/dapodik", Icon: "spreadsheet", Desc: "Lembar salin dan status data Dapodik", Module: ModuleDapodik, Group: GroupDapodik, Roles: []string{adminTU, kepala}},
	{Label: "Nilai ke Dapodik", Path: "/dapodik/nilai", Icon: "upload", Short: "Dapodik", Desc: "Isi format import nilai Dapodik", Module: ModuleDapodik, Group: GroupDapodik, Roles: []string{guru}},

	{Label: "Pengumuman", Path: "/pengumuman", Icon: "megaphone", Short: "Info", Desc: "Pengumuman untuk orang tua", Module: ModuleAnnouncements, Group: GroupInformation, Roles: []string{adminTU, kepala, bendahara, guru}},
	{Label: "Pengumuman", Path: "/portal/pengumuman", Icon: "megaphone", Short: "Info", Desc: "Informasi dari sekolah", Module: ModuleAnnouncements, Group: GroupInformation, Roles: []string{orangTua}},
	{Label: "Laporan", Path: "/laporan", Icon: "report", Desc: "Unduh laporan Excel dan PDF", Group: GroupInformation, Roles: []string{adminTU, bendahara, kepala}},

	{Label: "Pengaturan", Path: "/pengaturan", Icon: "settings", Short: "Atur", Desc: "Sekolah, tahun ajaran, mapel, dan pengaturan modul", Group: GroupSettings,
		Also: []string{"/pendaftaran/pengaturan", "/ujian/pengaturan"}, Roles: []string{adminTU, kepala, bendahara}, Duties: []string{koordinatorUjian}},
}

// SettingsItems adalah isi pusat Pengaturan (/pengaturan), dikelompokkan per
// bagian (Group). Aturan tampilnya sama dengan menu: peran, tugas tambahan,
// dan modul, mengikuti hak akses route masing-masing.
var SettingsItems = []MenuItem{
	{Label: "Profil Sekolah", Path: "/pengaturan/sekolah", Icon: "school", Desc: "Nama, alamat, logo, dan zona waktu sekolah", Group: "Sekolah", Roles: []string{adminTU, kepala}},
	{Label: "Tahun Ajaran", Path: "/pengaturan/tahun-ajaran", Icon: "calendar", Desc: "Tahun ajaran dan semester aktif", Group: "Sekolah", Roles: []string{adminTU, kepala}},
	{Label: "Mata Pelajaran", Path: "/pengaturan/mapel", Icon: "teacher", Desc: "Daftar mata pelajaran dan KKM", Group: "Sekolah", Roles: []string{adminTU}},
	{Label: "Penugasan Guru", Path: "/pengaturan/penugasan", Icon: "user-check", Desc: "Guru pengampu mapel per kelas", Group: "Sekolah", Roles: []string{adminTU}},
	{Label: "Mapel Saya", Path: "/pengaturan/penugasan", Icon: "user-check", Desc: "Mapel dan kelas yang Anda ampu", Group: "Sekolah", Roles: []string{guru}},
	{Label: "Tugas Tambahan", Path: "/pengaturan/tugas", Icon: "user-check", Desc: "Koordinator ujian per tahun ajaran", Module: ModuleExam, Group: "Sekolah", Roles: []string{adminTU, kepala}},
	{Label: "Absensi per Mapel", Path: "/pengaturan/absensi", Icon: "attendance", Desc: "Izinkan guru mengisi absensi per mapel selain harian", Module: ModuleAttendance, Group: "Absensi", Roles: []string{adminTU}},
	{Label: "Pengaturan Rapor", Path: "/pengaturan/rapor", Icon: "report", Desc: "Skala huruf, KKM, format, dan penanda tangan", Module: ModuleGrades, Group: "Nilai & Rapor", Roles: []string{adminTU}},
	{Label: "Pengaturan Keuangan", Path: "/pengaturan/keuangan", Icon: "wallet", Desc: "Ambang persetujuan pembatalan pembayaran", Module: ModuleBilling, Group: "Keuangan", Roles: []string{adminTU, bendahara, kepala}},
	{Label: "Pengaturan Pendaftaran", Path: "/pendaftaran/pengaturan", Icon: "user-check", Desc: "Format NIS dan daftar ulang", Module: ModuleAdmission, Group: "Pendaftaran", Roles: []string{adminTU, kepala}},
	{Label: "Pengaturan Ujian", Path: "/ujian/pengaturan", Icon: "file-text", Desc: "Telaah soal dan target kisi-kisi", Module: ModuleExam, Group: "Ujian", Roles: []string{adminTU}, Duties: []string{koordinatorUjian}},
}

// DefaultModules adalah modul bawaan sekolah (semua kecuali ujian), sama
// dengan module.Defaults (diuji di paket module).
var DefaultModules = []string{ModuleAttendance, ModuleSchedule, ModuleGrades, ModuleBilling, ModuleAdmission,
	ModuleDapodik, ModuleAnnouncements, ModulePortal}

// MenuFor mengembalikan item menu untuk pengguna dengan peran roles di
// sekolah dengan modul bawaan, dalam urutan Items. Pengguna dengan beberapa
// peran mendapat gabungan menu tanpa duplikat.
func MenuFor(roles []string) []MenuItem {
	return MenuForUser(roles, nil, DefaultModules)
}

// MenuForUser seperti MenuFor, ditambah item yang muncul karena tugas
// tambahan (duties) dan item modul opsional yang aktif (modules).
func MenuForUser(roles, duties, modules []string) []MenuItem {
	return filter(Items, roles, duties, modules)
}

// SettingsFor mengembalikan isi pusat Pengaturan untuk pengguna, per bagian.
func SettingsFor(roles, duties, modules []string) []Group {
	return Groups(filter(SettingsItems, roles, duties, modules))
}

// moduleOn melaporkan apakah item modul m tampil untuk set modul aktif on.
func moduleOn(m string, on map[string]bool) bool {
	return m == "" || on[m]
}

// filter memilih item list yang cocok dengan peran, tugas, dan modul aktif,
// dalam urutan list dan tanpa Path ganda.
func filter(list []MenuItem, roles, duties, modules []string) []MenuItem {
	has := make(map[string]bool, len(roles)+len(duties))
	for _, r := range roles {
		has["role:"+r] = true
	}
	for _, d := range duties {
		has["duty:"+d] = true
	}
	on := make(map[string]bool, len(modules))
	for _, m := range modules {
		on[m] = true
	}
	var out []MenuItem
	seen := map[string]bool{}
	for _, it := range list {
		if seen[it.Path] || !moduleOn(it.Module, on) {
			continue
		}
		match := false
		for _, r := range it.Roles {
			match = match || has["role:"+r]
		}
		for _, d := range it.Duties {
			match = match || (has["duty:"+d] && has["role:"+dutyRole[d]])
		}
		if match {
			out = append(out, it)
			seen[it.Path] = true
		}
	}
	return out
}

// Group adalah satu kelompok menu yang sudah disaring untuk pengguna.
type Group struct {
	Name  string
	Items []MenuItem
}

// Titled melaporkan apakah judul kelompok ditampilkan: kelompok bernama
// dengan lebih dari satu item. Kelompok satu item tampil tanpa judul agar
// menu yang pendek tetap ringkas.
func (g Group) Titled() bool { return g.Name != "" && len(g.Items) > 1 }

// Groups mengelompokkan items (hasil MenuForUser atau filter lain) per
// Group, dalam urutan kemunculan. Kelompok kosong tidak ada.
func Groups(items []MenuItem) []Group {
	var out []Group
	for _, it := range items {
		if n := len(out); n > 0 && out[n-1].Name == it.Group {
			out[n-1].Items = append(out[n-1].Items, it)
			continue
		}
		out = append(out, Group{Name: it.Group, Items: []MenuItem{it}})
	}
	return out
}

// MaxBottom adalah jumlah item bottom navigation di HP; sisanya di drawer
// (tombol "Menu").
const MaxBottom = 4

// MaxShortcuts adalah jumlah kartu pintasan maksimal di beranda.
const MaxShortcuts = 6

// rolePriority adalah urutan peran saat pengguna punya beberapa peran: peran
// pertama yang dimiliki menentukan prioritas utama.
var rolePriority = []string{adminTU, kepala, guru, bendahara, orangTua}

// bottomByRole adalah prioritas bottom navigation per peran (analisis menu
// bagian 3). Path yang tidak ada di menu pengguna (mis. modul nonaktif)
// dilewati, sehingga item berikutnya menjadi cadangan.
var bottomByRole = map[string][]string{
	adminTU:   {"/", "/siswa", "/keuangan/tunggakan", "/ujian", "/absensi/rekap", "/pengumuman"},
	kepala:    {"/", "/absensi/rekap", "/laporan", "/keuangan/tunggakan", "/siswa", "/pengumuman"},
	guru:      {"/", "/absensi", "/nilai/input", "/ujian", "/jadwal/saya", "/pengumuman"},
	bendahara: {"/", "/keuangan/pembayaran/baru", "/keuangan/tunggakan", "/keuangan/rekap-pembayaran", "/laporan"},
	orangTua:  {"/portal", "/portal/pengumuman"},
}

// shortcutsByRole adalah prioritas kartu pintasan beranda per peran.
var shortcutsByRole = map[string][]string{
	adminTU:   {"/siswa", "/ujian", "/keuangan/tunggakan", "/absensi/rekap", "/pendaftaran", "/pengumuman", "/kelas", "/dapodik"},
	kepala:    {"/absensi/rekap", "/nilai/rekap", "/keuangan/tunggakan", "/laporan", "/ujian", "/pengumuman", "/siswa"},
	guru:      {"/absensi", "/nilai/input", "/ujian", "/jadwal/saya", "/nilai/rekap", "/pengumuman", "/nilai/komponen"},
	bendahara: {"/keuangan/pembayaran/baru", "/keuangan/tunggakan", "/keuangan/tagihan-massal", "/keuangan/rekap-pembayaran", "/laporan", "/pendaftaran/biaya"},
	orangTua:  {"/portal", "/portal/pengumuman"},
}

// pick memilih paling banyak max item dari items sesuai prioritas peran
// pengguna (per rolePriority), tanpa ganda. Bila prioritas habis sebelum
// max, sisanya diisi item menu berketerangan dalam urutan menu.
func pick(items []MenuItem, roles []string, byRole map[string][]string, max int, fill bool) []MenuItem {
	byPath := make(map[string]MenuItem, len(items))
	for _, it := range items {
		byPath[it.Path] = it
	}
	has := map[string]bool{}
	for _, r := range roles {
		has[r] = true
	}
	var out []MenuItem
	seen := map[string]bool{}
	add := func(it MenuItem) {
		if !seen[it.Path] && len(out) < max {
			seen[it.Path] = true
			out = append(out, it)
		}
	}
	for _, r := range rolePriority {
		if !has[r] {
			continue
		}
		for _, p := range byRole[r] {
			if it, ok := byPath[p]; ok {
				add(it)
			}
		}
	}
	if fill {
		for _, it := range items {
			if it.Desc != "" {
				add(it)
			}
		}
	}
	return out
}

// Bottom mengembalikan item bottom navigation di HP untuk pengguna dengan
// peran roles: maksimal MaxBottom item sesuai prioritas peran.
func Bottom(items []MenuItem, roles []string) []MenuItem {
	return pick(items, roles, bottomByRole, MaxBottom, false)
}

// Shortcuts mengembalikan kartu pintasan beranda (tanpa Beranda): maksimal
// MaxShortcuts item sesuai prioritas peran, ditambah item menu lain bila
// kurang.
func Shortcuts(items []MenuItem, roles []string) []MenuItem {
	var rest []MenuItem
	for _, it := range items {
		if it.Path != "/" {
			rest = append(rest, it)
		}
	}
	return pick(rest, roles, shortcutsByRole, MaxShortcuts, true)
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
