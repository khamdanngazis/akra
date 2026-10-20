package testutil

import (
	"net/url"
)

// Kind menentukan cara route diuji terhadap data sekolah lain.
type Kind int

const (
	// KindPublic: route tanpa login (login, reset password, aktivasi, aset).
	// Tidak diuji lintas sekolah karena tidak memakai sesi sekolah; token
	// aktivasi dan reset diuji di paket auth.
	KindPublic Kind = iota + 1
	// KindByID: path berisi ID milik sekolah lain. Peran yang diizinkan harus
	// mendapat 404, peran lain 403. Data sekolah lain tidak boleh berubah.
	KindByID
	// KindPage: halaman atau unduhan milik sekolah sendiri, termasuk dengan
	// filter berisi ID sekolah lain. Tidak boleh 5xx dan tidak boleh memuat
	// data sekolah lain.
	KindPage
	// KindFormRef: POST yang isi form-nya menunjuk ID sekolah lain. Peran
	// yang diizinkan harus ditolak dengan 4xx.
	KindFormRef
	// KindOwn: POST yang hanya mengubah data sekolah sendiri. Data sekolah
	// lain tidak boleh berubah.
	KindOwn
)

// Peran.
const (
	Admin     = "admin_tu"
	Treasurer = "bendahara"
	Teacher   = "guru"
	Principal = "kepala_sekolah"
	Parent    = "orang_tua"
)

// Request adalah satu request uji: path lengkap (dengan query) dan isi form
// untuk POST.
type Request struct {
	Path string
	Form url.Values
}

// Route adalah satu entri daftar route utama. Setiap route yang didaftarkan
// di router harus punya entri di sini (dicek otomatis), dengan Method dan
// Pattern persis seperti chi.Walk melaporkannya.
type Route struct {
	Method  string
	Pattern string
	Kind    Kind
	// Roles adalah peran yang boleh membuka route ini.
	Roles []string
	// ControlRoles adalah peran untuk kontrol positif (request dengan ID
	// sekolah sendiri harus sampai ke handler). Nil berarti Roles. Dipakai
	// jika data hanya terlihat oleh sebagian peran, mis. job laporan hanya
	// untuk pembuatnya dan kepala sekolah.
	ControlRoles []string
	// Requests membuat request yang menunjuk data sekolah other (own adalah
	// sekolah pengguna, untuk request campuran). Nil berarti satu request ke
	// Pattern apa adanya.
	Requests func(own, other *School) []Request
}

var (
	staff    = []string{Admin, Treasurer, Teacher, Principal}
	allUsers = []string{Admin, Treasurer, Teacher, Principal, Parent}
	adminTU  = []string{Admin}
	// Peran data sekolah (siswa, kelas, tahun ajaran).
	schoolData = []string{Admin, Principal}
	// Peran keuangan yang membaca.
	financeRead = []string{Admin, Treasurer, Principal}
	treasurer   = []string{Treasurer}
	// Jenis tagihan diatur bendahara dan admin/TU.
	feeTypeWrite = []string{Admin, Treasurer}
	principal    = []string{Principal}
	parent       = []string{Parent}
	// Job laporan hanya terlihat oleh pembuatnya (bendahara di data uji) dan
	// kepala sekolah.
	reportJobViewers = []string{Treasurer, Principal}
)

func paths(f func(o *School) []string) func(own, other *School) []Request {
	return func(_, o *School) []Request {
		var out []Request
		for _, p := range f(o) {
			out = append(out, Request{Path: p})
		}
		return out
	}
}

func post(p func(o *School) string, form func(o *School) url.Values) func(own, other *School) []Request {
	return func(_, o *School) []Request {
		f := url.Values{}
		if form != nil {
			f = form(o)
		}
		return []Request{{Path: p(o), Form: f}}
	}
}

// Routes adalah daftar route utama. Tambahkan entri setiap kali route baru
// didaftarkan.
var Routes = []Route{
	// Publik.
	{Method: "GET", Pattern: "/healthz", Kind: KindPublic},
	{Method: "GET", Pattern: "/healthz/db", Kind: KindPublic},
	{Method: "GET", Pattern: "/static/*", Kind: KindPublic},
	{Method: "HEAD", Pattern: "/static/*", Kind: KindPublic},
	{Method: "GET", Pattern: "/login", Kind: KindPublic},
	{Method: "POST", Pattern: "/login", Kind: KindPublic},
	{Method: "POST", Pattern: "/logout", Kind: KindPublic},
	{Method: "GET", Pattern: "/lupa-password", Kind: KindPublic},
	{Method: "POST", Pattern: "/lupa-password", Kind: KindPublic},
	{Method: "GET", Pattern: "/reset-password", Kind: KindPublic},
	{Method: "POST", Pattern: "/reset-password", Kind: KindPublic},
	{Method: "GET", Pattern: "/aktivasi", Kind: KindPublic},
	{Method: "POST", Pattern: "/aktivasi", Kind: KindPublic},
	// URL bertanda tangan driver storage local (hanya dev/uji): akses dijaga
	// tanda tangan dan masa berlaku, bukan sesi; diuji di paket storage.
	{Method: "GET", Pattern: "/storage/*", Kind: KindPublic},

	// Beranda.
	{Method: "GET", Pattern: "/", Kind: KindPage, Roles: allUsers}, // orang tua dialihkan ke portal

	// Absensi.
	{Method: "GET", Pattern: "/absensi/", Kind: KindPage, Roles: []string{Teacher},
		Requests: paths(func(o *School) []string {
			return []string{"/absensi/", "/absensi/?kelas=" + o.ClassID, "/absensi/?kelas=" + o.ClassID + "&tanggal=2026-10-16"}
		})},
	{Method: "POST", Pattern: "/absensi/{sessionID}", Kind: KindByID, Roles: []string{Teacher},
		Requests: post(func(o *School) string { return "/absensi/" + o.AttendanceSessionID },
			func(o *School) url.Values { return url.Values{"status_" + o.EnrollmentIDs[0]: {"alpa"}} })},
	{Method: "GET", Pattern: "/absensi/rekap", Kind: KindPage, Roles: []string{Admin, Teacher, Principal},
		Requests: paths(func(o *School) []string {
			return []string{"/absensi/rekap", "/absensi/rekap?kelas=" + o.ClassID}
		})},

	// Impor siswa.
	{Method: "GET", Pattern: "/impor/siswa", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/impor/siswa", Kind: KindOwn, Roles: adminTU, Requests: post(func(*School) string { return "/impor/siswa" }, nil)},
	{Method: "GET", Pattern: "/impor/siswa/template.xlsx", Kind: KindPage, Roles: adminTU},
	{Method: "GET", Pattern: "/impor/batch/{id}", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/impor/batch/" + o.ImportBatchID} })},
	{Method: "GET", Pattern: "/impor/batch/{id}/kesalahan.xlsx", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/impor/batch/" + o.ImportBatchID + "/kesalahan.xlsx"} })},
	{Method: "POST", Pattern: "/impor/batch/{id}/simpan", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/impor/batch/" + o.ImportBatchID + "/simpan" }, nil)},

	// Kelas.
	{Method: "GET", Pattern: "/kelas/", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string { return []string{"/kelas/", "/kelas/?tahun=" + o.YearID} })},
	{Method: "POST", Pattern: "/kelas/", Kind: KindFormRef, Roles: adminTU,
		Requests: post(func(*School) string { return "/kelas/" }, func(o *School) url.Values {
			return url.Values{"academic_year_id": {o.YearID}, "name": {"9Z"}, "grade_level": {"1"}}
		})},
	{Method: "GET", Pattern: "/kelas/baru", Kind: KindPage, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/kelas/baru", "/kelas/baru?tahun=" + o.YearID} })},
	{Method: "POST", Pattern: "/kelas/{id}", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/kelas/" + o.ClassID }, func(o *School) url.Values {
			return url.Values{"academic_year_id": {o.YearID}, "name": {"Diubah"}, "grade_level": {"2"}, "homeroom_teacher_id": {o.Teacher.ID}}
		})},
	{Method: "GET", Pattern: "/kelas/{id}/ubah", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/kelas/" + o.ClassID + "/ubah"} })},

	// Keuangan.
	{Method: "GET", Pattern: "/keuangan/audit", Kind: KindPage, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string {
			return []string{"/keuangan/audit", "/keuangan/audit?entity=" + o.PaymentID}
		})},
	{Method: "GET", Pattern: "/keuangan/bukti/{id}", Kind: KindByID, Roles: financeRead,
		Requests: paths(func(o *School) []string { return []string{"/keuangan/bukti/" + o.ProofID} })},
	{Method: "GET", Pattern: "/keuangan/jenis-tagihan/", Kind: KindPage, Roles: financeRead},
	{Method: "POST", Pattern: "/keuangan/jenis-tagihan/", Kind: KindOwn, Roles: feeTypeWrite,
		Requests: post(func(*School) string { return "/keuangan/jenis-tagihan/" }, func(*School) url.Values {
			return url.Values{"name": {"Uji lintas"}, "default_amount": {"1000"}}
		})},
	{Method: "GET", Pattern: "/keuangan/jenis-tagihan/baru", Kind: KindPage, Roles: feeTypeWrite},
	{Method: "POST", Pattern: "/keuangan/jenis-tagihan/{id}", Kind: KindByID, Roles: feeTypeWrite,
		Requests: post(func(o *School) string { return "/keuangan/jenis-tagihan/" + o.FeeTypeID }, func(*School) url.Values {
			return url.Values{"name": {"Diubah"}, "default_amount": {"1"}}
		})},
	{Method: "POST", Pattern: "/keuangan/jenis-tagihan/{id}/aktifkan", Kind: KindByID, Roles: feeTypeWrite,
		Requests: post(func(o *School) string { return "/keuangan/jenis-tagihan/" + o.FeeTypeID + "/aktifkan" }, nil)},
	{Method: "POST", Pattern: "/keuangan/jenis-tagihan/{id}/nonaktifkan", Kind: KindByID, Roles: feeTypeWrite,
		Requests: post(func(o *School) string { return "/keuangan/jenis-tagihan/" + o.FeeTypeID + "/nonaktifkan" }, nil)},
	{Method: "GET", Pattern: "/keuangan/jenis-tagihan/{id}/ubah", Kind: KindByID, Roles: feeTypeWrite,
		Requests: paths(func(o *School) []string { return []string{"/keuangan/jenis-tagihan/" + o.FeeTypeID + "/ubah"} })},
	{Method: "GET", Pattern: "/keuangan/pembatalan/", Kind: KindPage, Roles: principal},
	{Method: "POST", Pattern: "/keuangan/pembatalan/{id}/setujui", Kind: KindByID, Roles: principal,
		Requests: post(func(o *School) string { return "/keuangan/pembatalan/" + o.VoidID + "/setujui" }, nil)},
	{Method: "POST", Pattern: "/keuangan/pembatalan/{id}/tolak", Kind: KindByID, Roles: principal,
		Requests: post(func(o *School) string { return "/keuangan/pembatalan/" + o.VoidID + "/tolak" }, nil)},
	{Method: "POST", Pattern: "/keuangan/pembayaran/", Kind: KindFormRef, Roles: treasurer,
		Requests: post(func(*School) string { return "/keuangan/pembayaran/" }, func(o *School) url.Values {
			return url.Values{"invoice_id": {o.InvoiceID}, "student_id": {o.StudentIDs[0]}, "paid_on": {"2026-10-01"},
				"method": {"tunai"}, "amount": {"1000"}, "amount_" + o.InvoiceID: {"1000"}}
		})},
	{Method: "GET", Pattern: "/keuangan/pembayaran/baru", Kind: KindPage, Roles: treasurer,
		Requests: paths(func(o *School) []string {
			return []string{"/keuangan/pembayaran/baru", "/keuangan/pembayaran/baru?siswa=" + o.StudentIDs[0], "/keuangan/pembayaran/baru?q=Siswa"}
		})},
	{Method: "GET", Pattern: "/keuangan/pembayaran/cari", Kind: KindPage, Roles: treasurer,
		Requests: paths(func(*School) []string { return []string{"/keuangan/pembayaran/cari?q=Siswa"} })},
	{Method: "POST", Pattern: "/keuangan/pembayaran/{id}/batal", Kind: KindByID, Roles: treasurer,
		Requests: post(func(o *School) string { return "/keuangan/pembayaran/" + o.PaymentID + "/batal" }, func(*School) url.Values {
			return url.Values{"reason": {"Uji lintas sekolah"}}
		})},
	{Method: "POST", Pattern: "/keuangan/pembayaran/{id}/bukti", Kind: KindByID, Roles: treasurer,
		Requests: post(func(o *School) string { return "/keuangan/pembayaran/" + o.PaymentID + "/bukti" }, nil)},
	{Method: "GET", Pattern: "/keuangan/pembayaran/{id}/kuitansi", Kind: KindByID, Roles: []string{Admin, Treasurer},
		Requests: paths(func(o *School) []string { return []string{"/keuangan/pembayaran/" + o.PaymentID + "/kuitansi"} })},
	{Method: "GET", Pattern: "/keuangan/tagihan-massal/", Kind: KindPage, Roles: treasurer},
	{Method: "POST", Pattern: "/keuangan/tagihan-massal/", Kind: KindFormRef, Roles: treasurer,
		Requests: post(func(*School) string { return "/keuangan/tagihan-massal/" }, bulkForm)},
	{Method: "POST", Pattern: "/keuangan/tagihan-massal/pratinjau", Kind: KindFormRef, Roles: treasurer,
		Requests: post(func(*School) string { return "/keuangan/tagihan-massal/pratinjau" }, bulkForm)},
	{Method: "GET", Pattern: "/keuangan/tagihan-massal/{id}", Kind: KindByID, Roles: treasurer,
		Requests: paths(func(o *School) []string { return []string{"/keuangan/tagihan-massal/" + o.BulkRunID} })},
	{Method: "GET", Pattern: "/keuangan/tagihan/{id}", Kind: KindByID, Roles: financeRead,
		Requests: paths(func(o *School) []string { return []string{"/keuangan/tagihan/" + o.InvoiceID} })},
	{Method: "GET", Pattern: "/keuangan/tunggakan", Kind: KindPage, Roles: financeRead,
		Requests: paths(func(o *School) []string {
			return []string{"/keuangan/tunggakan", "/keuangan/tunggakan?kelas=" + o.ClassID + "&jenis=" + o.FeeTypeID}
		})},

	// Laporan.
	{Method: "GET", Pattern: "/laporan", Kind: KindPage, Roles: financeRead},
	{Method: "GET", Pattern: "/laporan/kehadiran", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/kehadiran", "/laporan/kehadiran?kelas=" + o.ClassID + "&dari=2026-10-01&sampai=2026-10-31"}
		})},
	{Method: "GET", Pattern: "/laporan/kehadiran.pdf", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/kehadiran.pdf?kelas=" + o.ClassID + "&dari=2026-10-01&sampai=2026-10-31"}
		})},
	{Method: "GET", Pattern: "/laporan/kehadiran.xlsx", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/kehadiran.xlsx?kelas=" + o.ClassID + "&dari=2026-10-01&sampai=2026-10-31"}
		})},
	{Method: "GET", Pattern: "/laporan/tunggakan.pdf", Kind: KindPage, Roles: financeRead,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/tunggakan.pdf", "/laporan/tunggakan.pdf?kelas=" + o.ClassID}
		})},
	{Method: "GET", Pattern: "/laporan/tunggakan.xlsx", Kind: KindPage, Roles: financeRead,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/tunggakan.xlsx", "/laporan/tunggakan.xlsx?kelas=" + o.ClassID}
		})},
	{Method: "GET", Pattern: "/laporan/jobs/{id}", Kind: KindByID, Roles: financeRead, ControlRoles: reportJobViewers,
		Requests: paths(func(o *School) []string { return []string{"/laporan/jobs/" + o.ReportJobID} })},
	{Method: "GET", Pattern: "/laporan/jobs/{id}/unduh", Kind: KindByID, Roles: financeRead, ControlRoles: reportJobViewers,
		Requests: paths(func(o *School) []string { return []string{"/laporan/jobs/" + o.ReportJobID + "/unduh"} })},

	// Pengaturan.
	{Method: "GET", Pattern: "/pengaturan/keuangan", Kind: KindPage, Roles: financeRead},
	{Method: "POST", Pattern: "/pengaturan/keuangan", Kind: KindOwn, Roles: principal,
		Requests: post(func(*School) string { return "/pengaturan/keuangan" }, func(*School) url.Values {
			return url.Values{"void_approval_threshold": {"500000"}}
		})},
	{Method: "GET", Pattern: "/pengaturan/tahun-ajaran/", Kind: KindPage, Roles: schoolData},
	{Method: "POST", Pattern: "/pengaturan/tahun-ajaran/", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/pengaturan/tahun-ajaran/" }, func(*School) url.Values {
			return url.Values{"name": {"2030/2031"}, "start_date": {"2030-07-15"}, "end_date": {"2031-06-20"}}
		})},
	{Method: "GET", Pattern: "/pengaturan/tahun-ajaran/baru", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/pengaturan/tahun-ajaran/{id}", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pengaturan/tahun-ajaran/" + o.YearID }, func(*School) url.Values {
			return url.Values{"name": {"Diubah"}, "start_date": {"2026-07-13"}, "end_date": {"2027-06-25"}}
		})},
	{Method: "POST", Pattern: "/pengaturan/tahun-ajaran/{id}/aktifkan", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pengaturan/tahun-ajaran/" + o.YearID + "/aktifkan" }, nil)},
	{Method: "GET", Pattern: "/pengaturan/tahun-ajaran/{id}/ubah", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/pengaturan/tahun-ajaran/" + o.YearID + "/ubah"} })},

	// Mata pelajaran (T64): hanya admin/TU.
	{Method: "GET", Pattern: "/pengaturan/mapel/", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/pengaturan/mapel/", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/pengaturan/mapel/" }, func(*School) url.Values {
			return url.Values{"name": {"Uji lintas"}, "code": {"UJI"}}
		})},
	{Method: "POST", Pattern: "/pengaturan/mapel/{id}", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pengaturan/mapel/" + o.SubjectID }, func(*School) url.Values {
			return url.Values{"name": {"Diubah"}, "code": {"UBH"}, "status": {"nonaktif"}}
		})},

	// Penugasan guru mapel (T65): admin/TU mengatur, guru melihat penugasannya.
	{Method: "GET", Pattern: "/pengaturan/penugasan/", Kind: KindPage, Roles: []string{Admin, Teacher},
		Requests: paths(func(o *School) []string {
			return []string{"/pengaturan/penugasan/", "/pengaturan/penugasan/?kelas=" + o.ClassID}
		})},
	{Method: "POST", Pattern: "/pengaturan/penugasan/{classID}", Kind: KindByID, Roles: adminTU,
		Requests: func(own, o *School) []Request {
			return []Request{
				// Kelas sekolah lain.
				{Path: "/pengaturan/penugasan/" + o.ClassID, Form: url.Values{"t_" + o.SubjectID: {o.Teacher.ID}}},
				// Kelas sendiri dengan mapel sekolah lain.
				{Path: "/pengaturan/penugasan/" + own.ClassID, Form: url.Values{"t_" + o.SubjectID: {own.Teacher.ID}}},
			}
		}},

	// Jadwal pelajaran (T66): admin/TU mengatur, kepala sekolah melihat.
	{Method: "GET", Pattern: "/jadwal/", Kind: KindPage, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/jadwal/", "/jadwal/?kelas=" + o.ClassID} })},
	{Method: "POST", Pattern: "/jadwal/kelas/{classID}", Kind: KindByID, Roles: adminTU,
		Requests: func(own, o *School) []Request {
			return []Request{
				// Kelas sekolah lain.
				{Path: "/jadwal/kelas/" + o.ClassID, Form: url.Values{"subject_id": {o.SubjectID}, "day": {"2"}, "start": {"09:00"}, "end": {"09:40"}}},
				// Kelas sendiri dengan mapel sekolah lain.
				{Path: "/jadwal/kelas/" + own.ClassID, Form: url.Values{"subject_id": {o.SubjectID}, "day": {"2"}, "start": {"09:00"}, "end": {"09:40"}}},
			}
		}},
	{Method: "GET", Pattern: "/jadwal/slot/{id}/ubah", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/jadwal/slot/" + o.SlotID + "/ubah"} })},
	{Method: "POST", Pattern: "/jadwal/slot/{id}", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/jadwal/slot/" + o.SlotID }, func(o *School) url.Values {
			return url.Values{"subject_id": {o.SubjectID}, "day": {"3"}, "start": {"10:00"}, "end": {"10:40"}}
		})},
	{Method: "POST", Pattern: "/jadwal/slot/{id}/hapus", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/jadwal/slot/" + o.SlotID + "/hapus" }, nil)},

	// Pengumuman.
	{Method: "GET", Pattern: "/pengumuman", Kind: KindPage, Roles: staff},
	{Method: "POST", Pattern: "/pengumuman", Kind: KindFormRef, Roles: schoolData,
		Requests: post(func(*School) string { return "/pengumuman" }, func(o *School) url.Values {
			return url.Values{"title": {"Uji lintas"}, "body": {"Isi"}, "class_id": {o.ClassID}}
		})},
	{Method: "GET", Pattern: "/pengumuman/baru", Kind: KindPage, Roles: schoolData},

	// Portal orang tua.
	{Method: "GET", Pattern: "/portal/", Kind: KindPage, Roles: parent},
	{Method: "GET", Pattern: "/portal/pengumuman", Kind: KindPage, Roles: parent},
	{Method: "GET", Pattern: "/portal/siswa/{id}", Kind: KindByID, Roles: parent,
		Requests: paths(func(o *School) []string { return []string{"/portal/siswa/" + o.StudentIDs[0]} })},
	{Method: "GET", Pattern: "/portal/siswa/{id}/tagihan", Kind: KindByID, Roles: parent,
		Requests: paths(func(o *School) []string { return []string{"/portal/siswa/" + o.StudentIDs[0] + "/tagihan"} })},
	{Method: "GET", Pattern: "/portal/pembayaran/{id}/kuitansi", Kind: KindByID, Roles: parent,
		Requests: paths(func(o *School) []string { return []string{"/portal/pembayaran/" + o.PaymentID + "/kuitansi"} })},

	// Siswa.
	{Method: "GET", Pattern: "/siswa/", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/siswa/", "/siswa/?q=Siswa", "/siswa/?tahun=" + o.YearID + "&kelas=" + o.ClassID}
		})},
	{Method: "POST", Pattern: "/siswa/", Kind: KindFormRef, Roles: adminTU,
		Requests: post(func(*School) string { return "/siswa/" }, func(o *School) url.Values {
			return url.Values{"nis": {"lintas-1"}, "full_name": {"Uji Lintas"}, "class_name": {o.ClassName}}
		})},
	{Method: "GET", Pattern: "/siswa/baru", Kind: KindPage, Roles: adminTU},
	{Method: "GET", Pattern: "/siswa/{id}", Kind: KindByID, Roles: schoolData,
		Requests: paths(func(o *School) []string { return []string{"/siswa/" + o.StudentIDs[0]} })},
	{Method: "POST", Pattern: "/siswa/{id}", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/siswa/" + o.StudentIDs[0] }, func(*School) url.Values {
			return url.Values{"nis": {"diubah"}, "full_name": {"Diubah"}, "status": {"keluar"}}
		})},
	{Method: "POST", Pattern: "/siswa/{id}/pindah-kelas", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/siswa/" + o.StudentIDs[1] + "/pindah-kelas" }, func(o *School) url.Values {
			return url.Values{"class_name": {o.ClassName}}
		})},
	{Method: "GET", Pattern: "/siswa/{id}/ubah", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/siswa/" + o.StudentIDs[0] + "/ubah"} })},
	{Method: "POST", Pattern: "/siswa/{id}/wali/{guardianID}/akun", Kind: KindByID, Roles: adminTU,
		Requests: guardianAccount("/akun")},
	{Method: "POST", Pattern: "/siswa/{id}/wali/{guardianID}/akun/kirim-ulang", Kind: KindByID, Roles: adminTU,
		Requests: guardianAccount("/akun/kirim-ulang")},
}

func bulkForm(o *School) url.Values {
	return url.Values{"fee_type_id": {o.FeeTypeID}, "class_id": {o.ClassID}, "period": {"2026-11"}}
}

// guardianAccount menguji akun wali dengan siswa dan wali sekolah lain, serta
// kombinasi siswa sekolah sendiri dengan wali sekolah lain (dan sebaliknya).
func guardianAccount(suffix string) func(own, other *School) []Request {
	return func(own, o *School) []Request {
		form := url.Values{}
		return []Request{
			{Path: "/siswa/" + o.StudentIDs[0] + "/wali/" + o.GuardianID + suffix, Form: form},
			{Path: "/siswa/" + own.StudentIDs[0] + "/wali/" + o.GuardianID + suffix, Form: form},
			{Path: "/siswa/" + o.StudentIDs[0] + "/wali/" + own.GuardianID + suffix, Form: form},
		}
	}
}
