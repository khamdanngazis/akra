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
	// Module adalah modul paling spesifik pemilik route (T164), sama dengan
	// module.Owner(Pattern); kosong untuk route inti. Route ditutup bila
	// salah satu modul di module.Required(Pattern) tidak aktif.
	Module string
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
	// Modul ujian: staf yang membuka bank soal, dan yang boleh menulis.
	examStaff   = []string{Admin, Teacher, Principal}
	examWriters = []string{Admin, Teacher}
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
	// Halaman status layanan (T145) publik; /metrics dan /internal dijaga
	// token tim (OPS_TOKEN, bukan sesi), diuji di paket ops.
	{Method: "GET", Pattern: "/status", Kind: KindPublic},
	{Method: "GET", Pattern: "/metrics", Kind: KindPublic},
	{Method: "GET", Pattern: "/internal/", Kind: KindPublic},
	{Method: "GET", Pattern: "/internal/active-exams", Kind: KindPublic},
	{Method: "POST", Pattern: "/internal/alert-test", Kind: KindPublic},
	{Method: "GET", Pattern: "/static/*", Kind: KindPublic},
	// Formulir pendaftaran publik (T90). Akses draf dibatasi sesi; diuji di
	// paket admission (TestPublicFlow).
	{Method: "GET", Pattern: "/s/{code}/daftar/", Module: "admission", Kind: KindPublic},
	{Method: "POST", Pattern: "/s/{code}/daftar/", Module: "admission", Kind: KindPublic},
	{Method: "GET", Pattern: "/s/{code}/daftar/lanjut", Module: "admission", Kind: KindPublic},
	{Method: "POST", Pattern: "/s/{code}/daftar/lanjut", Module: "admission", Kind: KindPublic},
	{Method: "GET", Pattern: "/s/{code}/daftar/isi/{step}", Module: "admission", Kind: KindPublic},
	{Method: "POST", Pattern: "/s/{code}/daftar/isi/{step}", Module: "admission", Kind: KindPublic},
	{Method: "POST", Pattern: "/s/{code}/daftar/dokumen/{type}", Module: "admission", Kind: KindPublic},
	{Method: "POST", Pattern: "/s/{code}/daftar/kirim", Module: "admission", Kind: KindPublic},
	{Method: "GET", Pattern: "/s/{code}/daftar/selesai", Module: "admission", Kind: KindPublic},
	// Cek status publik (T95): akses dibatasi nomor + tanggal lahir di sesi.
	{Method: "GET", Pattern: "/s/{code}/pindah/", Module: "admission", Kind: KindPublic},
	{Method: "POST", Pattern: "/s/{code}/pindah/", Module: "admission", Kind: KindPublic},
	{Method: "GET", Pattern: "/s/{code}/status/", Module: "admission", Kind: KindPublic},
	{Method: "POST", Pattern: "/s/{code}/status/", Module: "admission", Kind: KindPublic},
	{Method: "GET", Pattern: "/s/{code}/status/bukti.pdf", Module: "admission", Kind: KindPublic},
	{Method: "POST", Pattern: "/s/{code}/status/keluar", Module: "admission", Kind: KindPublic},
	{Method: "GET", Pattern: "/s/{code}/logo", Kind: KindPublic},
	{Method: "GET", Pattern: "/wilayah", Kind: KindPublic},
	{Method: "HEAD", Pattern: "/static/*", Kind: KindPublic},
	{Method: "GET", Pattern: "/login", Kind: KindPublic},
	{Method: "POST", Pattern: "/login", Kind: KindPublic},
	{Method: "POST", Pattern: "/logout", Kind: KindPublic},
	{Method: "GET", Pattern: "/s/{code}", Kind: KindPublic},
	{Method: "GET", Pattern: "/lupa-password", Kind: KindPublic},
	{Method: "POST", Pattern: "/lupa-password", Kind: KindPublic},
	{Method: "GET", Pattern: "/reset-password", Kind: KindPublic},
	{Method: "POST", Pattern: "/reset-password", Kind: KindPublic},
	{Method: "GET", Pattern: "/aktivasi", Module: "portal", Kind: KindPublic},
	{Method: "POST", Pattern: "/aktivasi", Module: "portal", Kind: KindPublic},
	// URL bertanda tangan driver storage local (hanya dev/uji): akses dijaga
	// tanda tangan dan masa berlaku, bukan sesi; diuji di paket storage.
	{Method: "GET", Pattern: "/storage/*", Kind: KindPublic},

	// Beranda.
	{Method: "GET", Pattern: "/", Kind: KindPage, Roles: allUsers}, // orang tua dialihkan ke portal

	// Absensi.
	{Method: "GET", Pattern: "/absensi/", Module: "attendance", Kind: KindPage, Roles: []string{Teacher},
		Requests: paths(func(o *School) []string {
			return []string{"/absensi/", "/absensi/?kelas=" + o.ClassID, "/absensi/?kelas=" + o.ClassID + "&tanggal=2026-10-16",
				"/absensi/?kelas=" + o.ClassID + "&mapel=" + o.SubjectID}
		})},
	{Method: "POST", Pattern: "/absensi/{sessionID}", Module: "attendance", Kind: KindByID, Roles: []string{Teacher},
		Requests: post(func(o *School) string { return "/absensi/" + o.AttendanceSessionID },
			func(o *School) url.Values { return url.Values{"status_" + o.EnrollmentIDs[0]: {"alpa"}} })},
	{Method: "GET", Pattern: "/absensi/rekap", Module: "attendance", Kind: KindPage, Roles: []string{Admin, Teacher, Principal},
		Requests: paths(func(o *School) []string {
			return []string{"/absensi/rekap", "/absensi/rekap?kelas=" + o.ClassID, "/absensi/rekap?kelas=" + o.ClassID + "&mapel=" + o.SubjectID}
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
	{Method: "GET", Pattern: "/keuangan/audit", Module: "billing", Kind: KindPage, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string {
			return []string{"/keuangan/audit", "/keuangan/audit?entity=" + o.PaymentID}
		})},
	{Method: "GET", Pattern: "/keuangan/bukti/{id}", Module: "billing", Kind: KindByID, Roles: financeRead,
		Requests: paths(func(o *School) []string { return []string{"/keuangan/bukti/" + o.ProofID} })},
	{Method: "GET", Pattern: "/keuangan/jenis-tagihan/", Module: "billing", Kind: KindPage, Roles: financeRead},
	{Method: "POST", Pattern: "/keuangan/jenis-tagihan/", Module: "billing", Kind: KindOwn, Roles: feeTypeWrite,
		Requests: post(func(*School) string { return "/keuangan/jenis-tagihan/" }, func(*School) url.Values {
			return url.Values{"name": {"Uji lintas"}, "default_amount": {"1000"}}
		})},
	{Method: "GET", Pattern: "/keuangan/jenis-tagihan/baru", Module: "billing", Kind: KindPage, Roles: feeTypeWrite},
	{Method: "POST", Pattern: "/keuangan/jenis-tagihan/{id}", Module: "billing", Kind: KindByID, Roles: feeTypeWrite,
		Requests: post(func(o *School) string { return "/keuangan/jenis-tagihan/" + o.FeeTypeID }, func(*School) url.Values {
			return url.Values{"name": {"Diubah"}, "default_amount": {"1"}}
		})},
	{Method: "POST", Pattern: "/keuangan/jenis-tagihan/{id}/aktifkan", Module: "billing", Kind: KindByID, Roles: feeTypeWrite,
		Requests: post(func(o *School) string { return "/keuangan/jenis-tagihan/" + o.FeeTypeID + "/aktifkan" }, nil)},
	{Method: "POST", Pattern: "/keuangan/jenis-tagihan/{id}/nonaktifkan", Module: "billing", Kind: KindByID, Roles: feeTypeWrite,
		Requests: post(func(o *School) string { return "/keuangan/jenis-tagihan/" + o.FeeTypeID + "/nonaktifkan" }, nil)},
	{Method: "GET", Pattern: "/keuangan/jenis-tagihan/{id}/ubah", Module: "billing", Kind: KindByID, Roles: feeTypeWrite,
		Requests: paths(func(o *School) []string { return []string{"/keuangan/jenis-tagihan/" + o.FeeTypeID + "/ubah"} })},
	{Method: "GET", Pattern: "/keuangan/pembatalan/", Module: "billing", Kind: KindPage, Roles: principal},
	{Method: "POST", Pattern: "/keuangan/pembatalan/{id}/setujui", Module: "billing", Kind: KindByID, Roles: principal,
		Requests: post(func(o *School) string { return "/keuangan/pembatalan/" + o.VoidID + "/setujui" }, nil)},
	{Method: "POST", Pattern: "/keuangan/pembatalan/{id}/tolak", Module: "billing", Kind: KindByID, Roles: principal,
		Requests: post(func(o *School) string { return "/keuangan/pembatalan/" + o.VoidID + "/tolak" }, nil)},
	{Method: "POST", Pattern: "/keuangan/pembayaran/", Module: "billing", Kind: KindFormRef, Roles: treasurer,
		Requests: post(func(*School) string { return "/keuangan/pembayaran/" }, func(o *School) url.Values {
			return url.Values{"invoice_id": {o.InvoiceID}, "student_id": {o.StudentIDs[0]}, "paid_on": {"2026-10-01"},
				"method": {"tunai"}, "amount": {"1000"}, "amount_" + o.InvoiceID: {"1000"}}
		})},
	{Method: "GET", Pattern: "/keuangan/pembayaran/baru", Module: "billing", Kind: KindPage, Roles: treasurer,
		Requests: paths(func(o *School) []string {
			return []string{"/keuangan/pembayaran/baru", "/keuangan/pembayaran/baru?siswa=" + o.StudentIDs[0], "/keuangan/pembayaran/baru?q=Siswa"}
		})},
	{Method: "GET", Pattern: "/keuangan/pembayaran/cari", Module: "billing", Kind: KindPage, Roles: treasurer,
		Requests: paths(func(*School) []string { return []string{"/keuangan/pembayaran/cari?q=Siswa"} })},
	{Method: "POST", Pattern: "/keuangan/pembayaran/{id}/batal", Module: "billing", Kind: KindByID, Roles: treasurer,
		Requests: post(func(o *School) string { return "/keuangan/pembayaran/" + o.PaymentID + "/batal" }, func(*School) url.Values {
			return url.Values{"reason": {"Uji lintas sekolah"}}
		})},
	{Method: "POST", Pattern: "/keuangan/pembayaran/{id}/bukti", Module: "billing", Kind: KindByID, Roles: treasurer,
		Requests: post(func(o *School) string { return "/keuangan/pembayaran/" + o.PaymentID + "/bukti" }, nil)},
	{Method: "GET", Pattern: "/keuangan/pembayaran/{id}/kuitansi", Module: "billing", Kind: KindByID, Roles: []string{Admin, Treasurer},
		Requests: paths(func(o *School) []string { return []string{"/keuangan/pembayaran/" + o.PaymentID + "/kuitansi"} })},
	{Method: "GET", Pattern: "/keuangan/tagihan-massal/", Module: "billing", Kind: KindPage, Roles: treasurer},
	{Method: "POST", Pattern: "/keuangan/tagihan-massal/", Module: "billing", Kind: KindFormRef, Roles: treasurer,
		Requests: post(func(*School) string { return "/keuangan/tagihan-massal/" }, bulkForm)},
	{Method: "POST", Pattern: "/keuangan/tagihan-massal/pratinjau", Module: "billing", Kind: KindFormRef, Roles: treasurer,
		Requests: post(func(*School) string { return "/keuangan/tagihan-massal/pratinjau" }, bulkForm)},
	{Method: "GET", Pattern: "/keuangan/tagihan-massal/{id}", Module: "billing", Kind: KindByID, Roles: treasurer,
		Requests: paths(func(o *School) []string { return []string{"/keuangan/tagihan-massal/" + o.BulkRunID} })},
	{Method: "GET", Pattern: "/keuangan/tagihan/{id}", Module: "billing", Kind: KindByID, Roles: financeRead,
		Requests: paths(func(o *School) []string { return []string{"/keuangan/tagihan/" + o.InvoiceID} })},
	{Method: "GET", Pattern: "/keuangan/tunggakan", Module: "billing", Kind: KindPage, Roles: financeRead,
		Requests: paths(func(o *School) []string {
			return []string{"/keuangan/tunggakan", "/keuangan/tunggakan?kelas=" + o.ClassID + "&jenis=" + o.FeeTypeID}
		})},

	// Laporan.
	{Method: "GET", Pattern: "/laporan", Kind: KindPage, Roles: financeRead},
	{Method: "GET", Pattern: "/laporan/kehadiran", Module: "attendance", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/kehadiran", "/laporan/kehadiran?kelas=" + o.ClassID + "&dari=2026-10-01&sampai=2026-10-31"}
		})},
	{Method: "GET", Pattern: "/laporan/kehadiran.pdf", Module: "attendance", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/kehadiran.pdf?kelas=" + o.ClassID + "&dari=2026-10-01&sampai=2026-10-31"}
		})},
	{Method: "GET", Pattern: "/laporan/kehadiran.xlsx", Module: "attendance", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/kehadiran.xlsx?kelas=" + o.ClassID + "&dari=2026-10-01&sampai=2026-10-31"}
		})},
	{Method: "GET", Pattern: "/laporan/tunggakan.pdf", Module: "billing", Kind: KindPage, Roles: financeRead,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/tunggakan.pdf", "/laporan/tunggakan.pdf?kelas=" + o.ClassID}
		})},
	{Method: "GET", Pattern: "/laporan/tunggakan.xlsx", Module: "billing", Kind: KindPage, Roles: financeRead,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/tunggakan.xlsx", "/laporan/tunggakan.xlsx?kelas=" + o.ClassID}
		})},
	{Method: "GET", Pattern: "/laporan/jobs/{id}", Kind: KindByID, Roles: financeRead, ControlRoles: reportJobViewers,
		Requests: paths(func(o *School) []string { return []string{"/laporan/jobs/" + o.ReportJobID} })},
	{Method: "GET", Pattern: "/laporan/jobs/{id}/unduh", Kind: KindByID, Roles: financeRead, ControlRoles: reportJobViewers,
		Requests: paths(func(o *School) []string { return []string{"/laporan/jobs/" + o.ReportJobID + "/unduh"} })},

	// Pengaturan.
	{Method: "GET", Pattern: "/pengaturan/keuangan", Module: "billing", Kind: KindPage, Roles: financeRead},
	{Method: "POST", Pattern: "/pengaturan/keuangan", Module: "billing", Kind: KindOwn, Roles: principal,
		Requests: post(func(*School) string { return "/pengaturan/keuangan" }, func(*School) url.Values {
			return url.Values{"void_approval_threshold": {"500000"}}
		})},
	// Tugas tambahan (T125).
	{Method: "GET", Pattern: "/pengaturan/tugas/", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/pengaturan/tugas/", "/pengaturan/tugas/?tahun=" + o.YearID}
		})},
	{Method: "POST", Pattern: "/pengaturan/tugas/", Kind: KindFormRef, Roles: adminTU,
		Requests: post(func(*School) string { return "/pengaturan/tugas/" }, func(o *School) url.Values {
			return url.Values{"academic_year_id": {o.YearID}, "user_id": {o.Principal.ID}, "duty": {"koordinator_ujian"}}
		})},
	{Method: "POST", Pattern: "/pengaturan/tugas/{id}/hapus", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pengaturan/tugas/" + o.DutyID + "/hapus" }, nil)},

	// Modul ujian (MVP 4). Sekolah uji mengaktifkan modul (fixtures).
	{Method: "GET", Pattern: "/ujian/", Module: "exam", Kind: KindPage, Roles: []string{Admin, Teacher, Principal}},
	// Guru di data uji bukan koordinator, jadi 403.

	// Peserta ujian (T124): sesi peserta terpisah, bukan sesi staf. Login
	// dan isolasi diuji di paket exam dan TestParticipant_* di cmd/server.
	{Method: "GET", Pattern: "/u/", Module: "exam", Kind: KindPublic},
	{Method: "GET", Pattern: "/u/{code}", Module: "exam", Kind: KindPublic},
	{Method: "POST", Pattern: "/u/{code}", Module: "exam", Kind: KindPublic},
	{Method: "GET", Pattern: "/u/beranda", Module: "exam", Kind: KindPublic},
	{Method: "POST", Pattern: "/u/keluar", Module: "exam", Kind: KindPublic},
	// Mengerjakan ujian (T138): hanya sesi peserta; kepemilikan attempt
	// diuji di paket exam (TestTake*).
	{Method: "GET", Pattern: "/u/ujian/{session}", Module: "exam", Kind: KindPublic},
	{Method: "POST", Pattern: "/u/ujian/{session}/mulai", Module: "exam", Kind: KindPublic},
	{Method: "GET", Pattern: "/u/a/{attempt}", Module: "exam", Kind: KindPublic},
	// Token attempt di header, di luar sesi dan CSRF (T139).
	{Method: "POST", Pattern: "/u/a/{attempt}/simpan", Module: "exam", Kind: KindPublic},
	// Service worker halaman ujian (T139).
	{Method: "GET", Pattern: "/u/sw.js", Module: "exam", Kind: KindPublic},
	{Method: "POST", Pattern: "/u/a/{attempt}/kumpul", Module: "exam", Kind: KindPublic},
	{Method: "GET", Pattern: "/u/a/{attempt}/selesai", Module: "exam", Kind: KindPublic},
	{Method: "GET", Pattern: "/u/a/{attempt}/gambar/{asset}", Module: "exam", Kind: KindPublic},
	// Pembahasan siswa (T151): hanya sesi peserta; kepemilikan dan aturan
	// buka hasil diuji di paket exam (TestStudentReview*).
	{Method: "GET", Pattern: "/u/hasil/{attempt}", Module: "exam", Kind: KindPublic},
	// Cek perangkat (T136): publik, tanpa data sekolah.
	{Method: "GET", Pattern: "/u/cek", Module: "exam", Kind: KindPublic},
	{Method: "GET", Pattern: "/u/cek/ping", Module: "exam", Kind: KindPublic},
	{Method: "GET", Pattern: "/u/cek/contoh.jpg", Module: "exam", Kind: KindPublic},
	// Panduan cetak umum (T154): publik, tanpa data sekolah.
	{Method: "GET", Pattern: "/u/panduan/peserta.pdf", Module: "exam", Kind: KindPublic},
	{Method: "GET", Pattern: "/u/panduan/orang-tua.pdf", Module: "exam", Kind: KindPublic},

	// Pusat bantuan dan panduan cetak (T154).
	{Method: "GET", Pattern: "/ujian/bantuan", Module: "exam", Kind: KindPage, Roles: examStaff},
	{Method: "GET", Pattern: "/ujian/panduan/pengawas.pdf", Module: "exam", Kind: KindPage, Roles: examStaff},
	{Method: "GET", Pattern: "/ujian/panduan/peserta.pdf", Module: "exam", Kind: KindPage, Roles: examStaff},
	{Method: "GET", Pattern: "/ujian/panduan/orang-tua.pdf", Module: "exam", Kind: KindPage, Roles: examStaff},

	// Ujian latihan bawaan (T136).
	{Method: "GET", Pattern: "/ujian/latihan", Module: "exam", Kind: KindPage, Roles: examStaff},
	{Method: "POST", Pattern: "/ujian/latihan", Module: "exam", Kind: KindFormRef, Roles: examWriters,
		Requests: post(func(*School) string { return "/ujian/latihan" }, func(o *School) url.Values { return url.Values{"class_id": {o.ClassID}} })},
	{Method: "POST", Pattern: "/ujian/latihan/{sesi}/kartu", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/latihan/" + o.ExamPracticeSessionID + "/kartu" }, nil)},

	// Bank soal dan TP (T127). Kepala sekolah hanya baca.
	{Method: "GET", Pattern: "/ujian/bank/", Module: "exam", Kind: KindPage, Roles: examStaff},
	{Method: "GET", Pattern: "/ujian/bank/baru", Module: "exam", Kind: KindPage, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/baru", "/ujian/bank/baru?mapel=" + o.SubjectID} })},
	{Method: "POST", Pattern: "/ujian/bank/", Module: "exam", Kind: KindFormRef, Roles: examWriters,
		Requests: post(func(*School) string { return "/ujian/bank/" }, func(o *School) url.Values {
			return url.Values{"name": {"Bank lintas"}, "subject_id": {o.SubjectID}}
		})},
	{Method: "GET", Pattern: "/ujian/bank/{id}", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/" + o.ExamBankID} })},
	{Method: "GET", Pattern: "/ujian/bank/{id}/ubah", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/" + o.ExamBankID + "/ubah"} })},
	{Method: "POST", Pattern: "/ujian/bank/{id}", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/bank/" + o.ExamBankID }, func(*School) url.Values { return url.Values{"name": {"Diubah"}} })},
	{Method: "POST", Pattern: "/ujian/bank/{id}/massal", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/bank/" + o.ExamBankID + "/massal" }, func(o *School) url.Values {
			return url.Values{"action": {"kesulitan"}, "difficulty": {"sulit"}, "ids": {o.ExamQuestionID}}
		})},
	// Editor soal dan stimulus (T128).
	{Method: "GET", Pattern: "/ujian/soal/{id}", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/soal/" + o.ExamQuestionID} })},
	{Method: "GET", Pattern: "/ujian/stimulus/{id}", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/stimulus/" + o.ExamStimulusID} })},
	{Method: "GET", Pattern: "/ujian/aset/{id}", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/aset/" + o.ExamAssetID} })},
	{Method: "GET", Pattern: "/ujian/bank/{id}/soal/baru", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/" + o.ExamBankID + "/soal/baru"} })},
	{Method: "POST", Pattern: "/ujian/bank/{id}/soal", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/bank/" + o.ExamBankID + "/soal" }, func(o *School) url.Values {
			return url.Values{"kind": {"esai"}, "body": {"Soal lintas"}, "objective_id": {o.ExamObjectiveID}}
		})},
	{Method: "GET", Pattern: "/ujian/soal/{id}/ubah", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/soal/" + o.ExamQuestionID + "/ubah"} })},
	{Method: "POST", Pattern: "/ujian/soal/{id}", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/soal/" + o.ExamQuestionID }, func(*School) url.Values {
			return url.Values{"kind": {"esai"}, "body": {"Diubah"}}
		})},
	{Method: "POST", Pattern: "/ujian/bank/{id}/gambar", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/bank/" + o.ExamBankID + "/gambar" }, nil)},
	{Method: "POST", Pattern: "/ujian/draf", Module: "exam", Kind: KindFormRef, Roles: examWriters,
		Requests: post(func(*School) string { return "/ujian/draf" }, func(o *School) url.Values {
			return url.Values{"bank_id": {o.ExamBankID}, "kind": {"pg"}, "body": {"draf"}}
		})},
	{Method: "POST", Pattern: "/ujian/draf/buang", Module: "exam", Kind: KindFormRef, Roles: examWriters,
		Requests: post(func(*School) string { return "/ujian/draf/buang" }, func(o *School) url.Values { return url.Values{"bank_id": {o.ExamBankID}} })},
	{Method: "POST", Pattern: "/ujian/pratinjau", Module: "exam", Kind: KindOwn, Roles: examWriters,
		Requests: post(func(*School) string { return "/ujian/pratinjau" }, func(o *School) url.Values {
			return url.Values{"kind": {"pg"}, "body": {"Pratinjau"}, "stimulus_id": {o.ExamStimulusID}}
		})},
	{Method: "GET", Pattern: "/ujian/bank/{id}/stimulus/baru", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/" + o.ExamBankID + "/stimulus/baru"} })},
	{Method: "POST", Pattern: "/ujian/bank/{id}/stimulus", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/bank/" + o.ExamBankID + "/stimulus" }, func(*School) url.Values {
			return url.Values{"title": {"Lintas"}, "body": {"Bacaan lintas"}}
		})},
	{Method: "GET", Pattern: "/ujian/stimulus/{id}/ubah", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/stimulus/" + o.ExamStimulusID + "/ubah"} })},
	{Method: "POST", Pattern: "/ujian/stimulus/{id}", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/stimulus/" + o.ExamStimulusID }, func(*School) url.Values {
			return url.Values{"title": {"Diubah"}, "body": {"Bacaan"}}
		})},
	{Method: "POST", Pattern: "/ujian/stimulus/{id}/urutan", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/stimulus/" + o.ExamStimulusID + "/urutan" }, nil)},
	{Method: "POST", Pattern: "/ujian/stimulus/{id}/arsip", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/stimulus/" + o.ExamStimulusID + "/arsip" }, nil)},

	{Method: "POST", Pattern: "/ujian/soal/{id}/duplikat", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/soal/" + o.ExamQuestionID + "/duplikat" }, nil)},
	{Method: "POST", Pattern: "/ujian/soal/{id}/arsip", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/soal/" + o.ExamQuestionID + "/arsip" }, nil)},
	{Method: "GET", Pattern: "/ujian/tp/", Module: "exam", Kind: KindPage, Roles: examStaff,
		Requests: paths(func(o *School) []string {
			return []string{"/ujian/tp/", "/ujian/tp/?mapel=" + o.SubjectID + "&tingkat=1"}
		})},
	{Method: "POST", Pattern: "/ujian/tp/", Module: "exam", Kind: KindFormRef, Roles: examWriters,
		Requests: post(func(*School) string { return "/ujian/tp/" }, func(o *School) url.Values {
			return url.Values{"subject_id": {o.SubjectID}, "grade_level": {"1"}, "code": {"TP 9"}, "description": {"Lintas"}}
		})},
	{Method: "POST", Pattern: "/ujian/tp/tempel", Module: "exam", Kind: KindFormRef, Roles: examWriters,
		Requests: post(func(*School) string { return "/ujian/tp/tempel" }, func(o *School) url.Values {
			return url.Values{"subject_id": {o.SubjectID}, "grade_level": {"1"}, "paste": {"TP 8\tLintas"}}
		})},
	{Method: "POST", Pattern: "/ujian/tp/{id}", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/tp/" + o.ExamObjectiveID }, func(*School) url.Values {
			return url.Values{"code": {"TP 1.1"}, "description": {"Diubah"}}
		})},
	{Method: "POST", Pattern: "/ujian/tp/{id}/arsip", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/tp/" + o.ExamObjectiveID + "/arsip" }, nil)},

	// Impor soal (T129–T131). Simpan impor: kontrol positif oleh guru
	// pembuatnya, karena impor yang sudah disimpan tidak bisa diubah lagi.
	{Method: "GET", Pattern: "/ujian/bank/{id}/impor", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/" + o.ExamBankID + "/impor"} })},
	{Method: "POST", Pattern: "/ujian/bank/{id}/impor", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/bank/" + o.ExamBankID + "/impor" }, nil)},
	{Method: "GET", Pattern: "/ujian/impor/template-soal.docx", Module: "exam", Kind: KindPage, Roles: examWriters},
	{Method: "GET", Pattern: "/ujian/bank/{id}/ekspor/moodle.xml", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/" + o.ExamBankID + "/ekspor/moodle.xml"} })},
	{Method: "GET", Pattern: "/ujian/bank/{id}/ekspor/soal.xlsx", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/" + o.ExamBankID + "/ekspor/soal.xlsx"} })},
	{Method: "GET", Pattern: "/ujian/impor/template-soal.xlsx", Module: "exam", Kind: KindPage, Roles: examWriters},
	{Method: "GET", Pattern: "/ujian/impor/{id}", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/impor/" + o.ExamImportID} })},
	{Method: "GET", Pattern: "/ujian/impor/{id}/butir/{seq}", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/impor/" + o.ExamImportID + "/butir/0"} })},
	{Method: "POST", Pattern: "/ujian/impor/{id}/butir/{seq}", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/impor/" + o.ExamImportID + "/butir/0" }, func(*School) url.Values {
			return url.Values{"kind": {"esai"}, "body": {"Diperbaiki"}}
		})},
	{Method: "POST", Pattern: "/ujian/impor/{id}/simpan", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/impor/" + o.ExamImportID + "/simpan" }, func(*School) url.Values {
			return url.Values{"include": {"0"}}
		})},

	// Kisi-kisi, kartu soal, cek mutu, telaah, pengaturan ujian (T132).
	{Method: "GET", Pattern: "/ujian/bank/{id}/kisi-kisi", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/" + o.ExamBankID + "/kisi-kisi"} })},
	{Method: "GET", Pattern: "/ujian/bank/{id}/kisi-kisi.xlsx", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/" + o.ExamBankID + "/kisi-kisi.xlsx"} })},
	{Method: "GET", Pattern: "/ujian/bank/{id}/kartu-soal", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/" + o.ExamBankID + "/kartu-soal"} })},
	{Method: "GET", Pattern: "/ujian/bank/{id}/mutu", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/bank/" + o.ExamBankID + "/mutu"} })},
	{Method: "GET", Pattern: "/ujian/telaah", Module: "exam", Kind: KindPage, Roles: examWriters},
	{Method: "POST", Pattern: "/ujian/soal/{id}/ajukan", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/soal/" + o.ExamQuestionID + "/ajukan" }, nil)},
	{Method: "POST", Pattern: "/ujian/soal/{id}/telaah", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/soal/" + o.ExamQuestionID + "/telaah" }, func(*School) url.Values {
			return url.Values{"decision": {"setuju"}}
		})},
	{Method: "GET", Pattern: "/ujian/pengaturan", Module: "exam", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/ujian/pengaturan", Module: "exam", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/ujian/pengaturan" }, func(*School) url.Values {
			return url.Values{"cognitive": {"30/50/20"}, "difficulty": {"30/50/20"}}
		})},

	// Ujian (T133): guru pemilik, admin_tu, dan kepala sekolah (baca).
	{Method: "GET", Pattern: "/ujian/paket/", Module: "exam", Kind: KindPage, Roles: examStaff},
	{Method: "GET", Pattern: "/ujian/paket/baru", Module: "exam", Kind: KindPage, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/baru", "/ujian/paket/baru?tujuan=latihan"} })},
	{Method: "POST", Pattern: "/ujian/paket/", Module: "exam", Kind: KindFormRef, Roles: examWriters,
		Requests: post(func(*School) string { return "/ujian/paket/" }, func(o *School) url.Values {
			return url.Values{"name": {"Ulangan baru"}, "purpose": {"ulangan"}, "subject_id": {o.SubjectID}, "duration": {"60"}}
		})},
	{Method: "GET", Pattern: "/ujian/paket/{id}", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string {
			return []string{"/ujian/paket/" + o.ExamID, "/ujian/paket/" + o.ExamID + "?tab=soal", "/ujian/paket/" + o.ExamID + "?tab=sesi&sesi=" + o.ExamSessionID,
				"/ujian/paket/" + o.ExamID + "?tab=dokumen", "/ujian/paket/" + o.ExamID + "?tab=lanjut"}
		})},
	{Method: "GET", Pattern: "/ujian/paket/{id}/atur/{step}", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string {
			var out []string
			for _, st := range []string{"dasar", "soal", "aturan", "jadwal", "cek"} {
				out = append(out, "/ujian/paket/"+o.ExamID+"/atur/"+st)
			}
			return out
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/atur/dasar", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/atur/dasar" }, func(o *School) url.Values {
			return url.Values{"name": {"Ulangan diubah"}, "subject_id": {o.SubjectID}, "duration": {"60"}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/atur/aturan", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/atur/aturan" }, func(*School) url.Values {
			return url.Values{"results_mode": {"nilai"}, "max_attempts": {"1"}, "integrity_mode": {"catat"}}
		})},
	{Method: "GET", Pattern: "/ujian/paket/{id}/pilih", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/pilih?bank=" + o.ExamBankID} })},
	{Method: "POST", Pattern: "/ujian/paket/{id}/soal", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/soal" }, func(o *School) url.Values {
			return url.Values{"ids": {o.ExamQuestionID}, "section_id": {o.ExamSectionID}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/acak", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/acak" }, func(o *School) url.Values {
			return url.Values{"bank_id": {o.ExamBankID}, "count": {"50"}, "section_id": {o.ExamSectionID}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/isi/{item}/geser", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/isi/" + o.ExamItemID + "/geser" }, func(*School) url.Values {
			return url.Values{"arah": {"naik"}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/urutan", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/urutan" }, func(o *School) url.Values {
			return url.Values{"section_id": {o.ExamSectionID}, "ids": {o.ExamItemID}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/bagian", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/bagian" }, func(*School) url.Values {
			return url.Values{"title": {"Bagian baru"}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/bagian/{sid}", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/bagian/" + o.ExamSectionID }, func(*School) url.Values {
			return url.Values{"title": {"Pilihan ganda"}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/bagian/{sid}/hapus", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/bagian/" + o.ExamSectionID + "/hapus" }, nil)},
	{Method: "POST", Pattern: "/ujian/paket/{id}/varian", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/varian" }, func(*School) url.Values {
			return url.Values{"variants": {"2"}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/sesi", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/sesi" }, func(o *School) url.Values {
			return url.Values{"class_id": {o.Class2ID}, "date_" + o.Class2ID: {"2030-02-04"}, "start_" + o.Class2ID: {"07:30"}, "end_" + o.Class2ID: {"09:00"}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/jadwalkan", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/jadwalkan" }, nil)},
	{Method: "POST", Pattern: "/ujian/paket/{id}/salin", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/salin" }, func(*School) url.Values {
			return url.Values{"name": {"Salinan"}}
		})},
	// Dokumen ujian (T135).
	{Method: "GET", Pattern: "/ujian/paket/{id}/daftar-hadir.pdf", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string {
			return []string{"/ujian/paket/" + o.ExamID + "/daftar-hadir.pdf?sesi=" + o.ExamSessionID}
		})},
	{Method: "GET", Pattern: "/ujian/paket/{id}/berita-acara.pdf", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/berita-acara.pdf"} })},
	{Method: "POST", Pattern: "/ujian/paket/{id}/kartu", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/kartu" }, func(o *School) url.Values {
			return url.Values{"session_id": {o.ExamSessionID}}
		})},
	{Method: "GET", Pattern: "/ujian/paket/{id}/naskah", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/naskah?paket=A"} })},
	{Method: "GET", Pattern: "/ujian/paket/{id}/kunci", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/kunci"} })},
	// Susulan, remedial, pengayaan (T137).
	{Method: "POST", Pattern: "/ujian/paket/{id}/susulan", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/susulan" }, func(*School) url.Values {
			return url.Values{"date": {"2030-02-04"}, "start": {"07:30"}, "end_date": {"2030-02-04"}, "end": {"09:00"}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/tindak-lanjut", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/tindak-lanjut" }, func(*School) url.Values {
			return url.Values{"kind": {"pengayaan"}, "date": {"2030-02-04"}, "start": {"07:30"}, "end_date": {"2030-02-04"}, "end": {"09:00"}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/isi/{item}/hapus", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/isi/" + o.ExamItemID + "/hapus" }, nil)},
	// Layar pengawas (T143): pengawas sesi (guru uji, di luar jendela waktu:
	// hanya baca), admin_tu (tulis), kepala (baca; diuji TestProctorAccess,
	// karena sesi uji sudah dibatalkan guru saat giliran kepala). Sebelum
	// batal: sesi yang dibatalkan tidak ditemukan.
	{Method: "GET", Pattern: "/ujian/sesi-saya", Module: "exam", Kind: KindPage, Roles: examStaff},
	{Method: "GET", Pattern: "/ujian/sesi/{sid}/awasi", Module: "exam", Kind: KindByID, Roles: examStaff, ControlRoles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/sesi/" + o.ExamSessionID + "/awasi"} })},
	{Method: "GET", Pattern: "/ujian/sesi/{sid}/awasi/data", Module: "exam", Kind: KindByID, Roles: examStaff, ControlRoles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/sesi/" + o.ExamSessionID + "/awasi/data"} })},
	{Method: "GET", Pattern: "/ujian/sesi/{sid}/token", Module: "exam", Kind: KindByID, Roles: examStaff, ControlRoles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/ujian/sesi/" + o.ExamSessionID + "/token"} })},
	{Method: "GET", Pattern: "/ujian/sesi/{sid}/token.json", Module: "exam", Kind: KindByID, Roles: examStaff, ControlRoles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/ujian/sesi/" + o.ExamSessionID + "/token.json"} })},
	{Method: "GET", Pattern: "/ujian/sesi/{sid}/berita-acara.pdf", Module: "exam", Kind: KindByID, Roles: examStaff, ControlRoles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/sesi/" + o.ExamSessionID + "/berita-acara.pdf"} })},
	{Method: "POST", Pattern: "/ujian/sesi/{sid}/awasi/aksi", Module: "exam", Kind: KindByID, Roles: examStaff, ControlRoles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/sesi/" + o.ExamSessionID + "/awasi/aksi" },
			func(*School) url.Values { return url.Values{"aksi": {"catat"}, "catatan": {"Uji lintas sekolah"}} })},
	{Method: "POST", Pattern: "/ujian/sesi/{sid}/awasi/peserta/{pid}", Module: "exam", Kind: KindByID, Roles: examStaff, ControlRoles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/sesi/" + o.ExamSessionID + "/awasi/peserta/" + o.ExamSessionID },
			func(*School) url.Values { return url.Values{"aksi": {"catat"}, "catatan": {"Uji lintas sekolah"}} })},
	{Method: "POST", Pattern: "/ujian/sesi/{sid}/berita-acara", Module: "exam", Kind: KindByID, Roles: examStaff, ControlRoles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/sesi/" + o.ExamSessionID + "/berita-acara" },
			func(*School) url.Values { return url.Values{"catatan": {"x"}, "sandi": {"salah"}} })},
	{Method: "POST", Pattern: "/ujian/paket/{id}/soal/{qid}/batal", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/soal/" + o.ExamQuestionID + "/batal" },
			func(*School) url.Values { return url.Values{"alasan": {"Uji lintas sekolah"}} })},
	// Koreksi esai (T147): fixture tidak punya soal esai atau attempt yang
	// dikumpulkan, jadi route per soal/attempt tanpa kontrol positif (diuji
	// di paket exam: TestEssayAssignmentScope).
	{Method: "GET", Pattern: "/ujian/paket/{id}/esai", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/esai"} })},
	{Method: "GET", Pattern: "/ujian/paket/{id}/esai/{qid}", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{},
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/esai/" + o.ExamQuestionID} })},
	{Method: "POST", Pattern: "/ujian/paket/{id}/esai/{qid}/{attempt}", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{},
		Requests: post(func(o *School) string {
			return "/ujian/paket/" + o.ExamID + "/esai/" + o.ExamQuestionID + "/" + o.ExamSessionID
		},
			func(*School) url.Values { return url.Values{"skor": {"1"}} })},
	{Method: "POST", Pattern: "/ujian/paket/{id}/esai/tugas", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/esai/tugas" },
			func(o *School) url.Values { return url.Values{"user_id": {o.Teacher.ID}} })},
	{Method: "POST", Pattern: "/ujian/paket/{id}/esai/tugas/{aid}/hapus", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/esai/tugas/0/hapus" }, nil)},
	// Kirim nilai ke rapor (T150): pemegang ujian; fixture tanpa komponen
	// tujuan sehingga pratinjau menampilkan pemilih komponen.
	{Method: "GET", Pattern: "/ujian/paket/{id}/rapor", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/rapor"} })},
	{Method: "POST", Pattern: "/ujian/paket/{id}/rapor", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/rapor" }, func(*School) url.Values {
			return url.Values{"aksi": {"kirim"}}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/hasil/buka", Module: "exam", Kind: KindByID, Roles: examWriters,
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/hasil/buka" }, func(*School) url.Values {
			return url.Values{"aksi": {"buka"}}
		})},
	// Hasil dan analisis (T148): staf ujian membaca; nilai manual hanya
	// penulis. Fixture tanpa attempt, jadi route per attempt tanpa kontrol
	// positif (diuji di paket exam: TestResultDetailAndManualScore).
	{Method: "GET", Pattern: "/ujian/paket/{id}/hasil", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/hasil"} })},
	{Method: "GET", Pattern: "/ujian/paket/{id}/hasil/butir", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/hasil/butir"} })},
	{Method: "GET", Pattern: "/ujian/paket/{id}/hasil/tp", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/hasil/tp"} })},
	{Method: "GET", Pattern: "/ujian/paket/{id}/hasil/analisis.xlsx", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/hasil/analisis.xlsx"} })},
	{Method: "GET", Pattern: "/ujian/paket/{id}/hasil/cetak", Module: "exam", Kind: KindByID, Roles: examStaff,
		Requests: paths(func(o *School) []string { return []string{"/ujian/paket/" + o.ExamID + "/hasil/cetak"} })},
	{Method: "GET", Pattern: "/ujian/paket/{id}/hasil/peserta/{attempt}", Module: "exam", Kind: KindByID, Roles: examStaff, ControlRoles: []string{},
		Requests: paths(func(o *School) []string {
			return []string{"/ujian/paket/" + o.ExamID + "/hasil/peserta/" + o.ExamSessionID}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/hasil/peserta/{attempt}/nilai", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{},
		Requests: post(func(o *School) string {
			return "/ujian/paket/" + o.ExamID + "/hasil/peserta/" + o.ExamSessionID + "/nilai"
		},
			func(o *School) url.Values {
				return url.Values{"qid": {o.ExamQuestionID}, "skor": {"1"}, "alasan": {"Uji lintas sekolah"}}
			})},
	{Method: "GET", Pattern: "/ujian/paket/{id}/soal/{qid}/kunci", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: paths(func(o *School) []string {
			return []string{"/ujian/paket/" + o.ExamID + "/soal/" + o.ExamQuestionID + "/kunci"}
		})},
	{Method: "POST", Pattern: "/ujian/paket/{id}/soal/{qid}/kunci", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/soal/" + o.ExamQuestionID + "/kunci" },
			func(*School) url.Values { return url.Values{"aksi": {"pratinjau"}} })},
	{Method: "POST", Pattern: "/ujian/paket/{id}/ralat", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/ralat" },
			func(*School) url.Values { return url.Values{"pesan": {"Uji lintas sekolah"}} })},
	// Sebelum batal: sesi yang dibatalkan tidak bisa diubah lagi.
	{Method: "POST", Pattern: "/ujian/paket/{id}/sesi/{sid}/terlambat", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/sesi/" + o.ExamSessionID + "/terlambat" },
			func(*School) url.Values { return url.Values{"late_policy": {"potong"}} })},
	{Method: "POST", Pattern: "/ujian/paket/{id}/sesi/{sid}/batal", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/sesi/" + o.ExamSessionID + "/batal" }, nil)},
	{Method: "POST", Pattern: "/ujian/paket/{id}/arsip", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/paket/" + o.ExamID + "/arsip" }, nil)},

	// Agenda PTS/PAS (T134): admin_tu dan koordinator menulis; kepala
	// sekolah membaca. Guru biasa 403.
	{Method: "GET", Pattern: "/ujian/agenda", Module: "exam", Kind: KindPage, Roles: []string{Admin, Principal}},
	{Method: "GET", Pattern: "/ujian/ruang", Module: "exam", Kind: KindPage, Roles: []string{Admin, Principal}},
	{Method: "GET", Pattern: "/ujian/agenda/baru", Module: "exam", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/ujian/agenda/", Module: "exam", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/ujian/agenda/" }, func(*School) url.Values {
			return url.Values{"name": {"PTS"}, "purpose": {"pts"}, "starts_on": {"2030-03-04"}, "ends_on": {"2030-03-08"}, "grades": {"1"}}
		})},
	{Method: "POST", Pattern: "/ujian/ruang", Module: "exam", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/ujian/ruang" }, func(*School) url.Values {
			return url.Values{"name": {"Ruang Baru"}, "kind": {"kelas"}, "capacity": {"30"}}
		})},
	{Method: "POST", Pattern: "/ujian/ruang/{id}", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/ruang/" + o.ExamRoomID }, func(*School) url.Values {
			return url.Values{"name": {"Lab Satu"}, "kind": {"lab"}, "capacity": {"32"}}
		})},
	{Method: "GET", Pattern: "/ujian/agenda/{id}", Module: "exam", Kind: KindByID, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/ujian/agenda/" + o.ExamEventID} })},
	{Method: "GET", Pattern: "/ujian/agenda/{id}/jadwal", Module: "exam", Kind: KindByID, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/ujian/agenda/" + o.ExamEventID + "/jadwal"} })},
	{Method: "GET", Pattern: "/ujian/agenda/{id}/ruang", Module: "exam", Kind: KindByID, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/ujian/agenda/" + o.ExamEventID + "/ruang"} })},
	{Method: "GET", Pattern: "/ujian/agenda/{id}/pengawas", Module: "exam", Kind: KindByID, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/ujian/agenda/" + o.ExamEventID + "/pengawas"} })},
	{Method: "GET", Pattern: "/ujian/agenda/{id}/kendali", Module: "exam", Kind: KindByID, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/ujian/agenda/" + o.ExamEventID + "/kendali"} })},
	{Method: "GET", Pattern: "/ujian/agenda/{id}/kendali/data", Module: "exam", Kind: KindByID, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/ujian/agenda/" + o.ExamEventID + "/kendali/data"} })},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/kendali/aksi", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/kendali/aksi" },
			func(o *School) url.Values {
				return url.Values{"aksi": {"pesan"}, "pesan": {"Uji lintas sekolah"}, "sid": {o.ExamSessionID}}
			})},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/kendali/pindah", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/kendali/pindah" },
			func(o *School) url.Values {
				return url.Values{"peserta": {o.ExamSessionID}, "tujuan": {o.ExamSessionID}}
			})},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/kendali/pengawas", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/kendali/pengawas" },
			func(o *School) url.Values { return url.Values{"sid": {o.ExamSessionID}, "user_id": {o.Teacher.ID}} })},
	{Method: "POST", Pattern: "/ujian/agenda/{id}", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID }, func(*School) url.Values {
			return url.Values{"name": {"PAS diubah"}, "starts_on": {"2030-01-07"}, "ends_on": {"2030-01-11"}, "grades": {"1"}}
		})},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/jam", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/jam" }, func(*School) url.Values {
			return url.Values{"day": {"2030-01-08"}, "start": {"07:30"}, "end": {"09:00"}}
		})},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/sel", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/sel" }, func(o *School) url.Values {
			return url.Values{"slot_id": {o.ExamSlotID}, "grade": {"1"}, "subject_id": {o.SubjectID}}
		})},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/ruang", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/ruang" }, func(o *School) url.Values {
			return url.Values{"room_id": {o.ExamRoomID}}
		})},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/bagi", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/bagi" }, nil)},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/pindah", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/pindah" }, func(o *School) url.Values {
			return url.Values{"from": {o.ClassID + "|" + o.ExamRoomID + "|1"}, "to": {o.ExamRoomID + "|1"}}
		})},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/sel/{cell}/ujian", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/sel/" + o.ExamCellID + "/ujian" }, nil)},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/pengawas", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/pengawas" }, nil)},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/pengawas/tukar", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/pengawas/tukar" }, func(o *School) url.Values {
			return url.Values{"unit": {o.ExamRoomID + "|" + o.ExamSlotID + "|1"}, "user_id": {o.Admin.ID}}
		})},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/terapkan", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/terapkan" }, nil)},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/umumkan", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/umumkan" }, nil)},
	{Method: "GET", Pattern: "/ujian/agenda/{id}/dokumen", Module: "exam", Kind: KindByID, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/ujian/agenda/" + o.ExamEventID + "/dokumen"} })},
	{Method: "GET", Pattern: "/ujian/agenda/{id}/daftar-hadir.pdf", Module: "exam", Kind: KindByID, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/ujian/agenda/" + o.ExamEventID + "/daftar-hadir.pdf"} })},
	{Method: "GET", Pattern: "/ujian/agenda/{id}/berita-acara.pdf", Module: "exam", Kind: KindByID, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/ujian/agenda/" + o.ExamEventID + "/berita-acara.pdf"} })},
	{Method: "GET", Pattern: "/ujian/agenda/{id}/denah.pdf", Module: "exam", Kind: KindByID, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/ujian/agenda/" + o.ExamEventID + "/denah.pdf"} })},
	{Method: "GET", Pattern: "/ujian/agenda/{id}/pintu.pdf", Module: "exam", Kind: KindByID, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string {
			return []string{"/ujian/agenda/" + o.ExamEventID + "/pintu.pdf?ruang=" + o.ExamRoomID}
		})},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/kartu", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/kartu" }, nil)},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/duduk", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/duduk" }, func(*School) url.Values {
			return url.Values{"selang": {"1"}}
		})},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/jam/{slot}/hapus", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/jam/" + o.ExamSlotID + "/hapus" }, nil)},
	{Method: "POST", Pattern: "/ujian/agenda/{id}/arsip", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/agenda/" + o.ExamEventID + "/arsip" }, nil)},
	{Method: "POST", Pattern: "/ujian/ruang/{id}/arsip", Module: "exam", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/ujian/ruang/" + o.ExamRoomID + "/arsip" }, nil)},

	// Arsip (bank, soal, stimulus, TP): kontrol positif oleh guru pemilik,
	// penulis terakhir di urutan uji, dan arsip bank paling akhir, sehingga
	// kontrol positif lain selalu memakai data yang masih aktif.
	{Method: "POST", Pattern: "/ujian/bank/{id}/arsip", Module: "exam", Kind: KindByID, Roles: examWriters, ControlRoles: []string{Teacher},
		Requests: post(func(o *School) string { return "/ujian/bank/" + o.ExamBankID + "/arsip" }, nil)},

	// Profil sekolah dan zona waktu (T126).
	// Pusat Pengaturan (T156): semua staf; isinya per peran dan modul.
	{Method: "GET", Pattern: "/pengaturan", Kind: KindPage, Roles: staff},
	{Method: "GET", Pattern: "/pengaturan/sekolah/", Kind: KindPage, Roles: schoolData},
	{Method: "POST", Pattern: "/pengaturan/sekolah/", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/pengaturan/sekolah/" }, func(*School) url.Values {
			return url.Values{"timezone": {"Asia/Makassar"}}
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

	// Pengaturan absensi per mapel (T68): hanya admin/TU. Halaman sendiri
	// sejak T156 (sebelumnya kartu di halaman mata pelajaran).
	{Method: "GET", Pattern: "/pengaturan/absensi", Module: "attendance", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/pengaturan/absensi", Module: "attendance", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/pengaturan/absensi" }, func(*School) url.Values {
			return url.Values{"per_mapel": {"aktif"}}
		})},

	// Pengaturan rapor (T73): hanya admin/TU.
	{Method: "GET", Pattern: "/pengaturan/rapor/", Module: "grades", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/pengaturan/rapor/", Module: "grades", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/pengaturan/rapor/" }, func(*School) url.Values {
			return url.Values{"format": {"angka"}, "kkm_default": {"75"}, "principal_name": {"Uji"}, "signer_title": {"Kepala Sekolah"},
				"letter_0": {"A"}, "min_0": {"80"}, "max_0": {"100"}, "letter_1": {"B"}, "min_1": {"0"}, "max_1": {"79"}}
		})},

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
	{Method: "GET", Pattern: "/jadwal/", Module: "schedule", Kind: KindPage, Roles: []string{Admin, Principal},
		Requests: paths(func(o *School) []string { return []string{"/jadwal/", "/jadwal/?kelas=" + o.ClassID} })},
	{Method: "GET", Pattern: "/jadwal/saya", Module: "schedule", Kind: KindPage, Roles: []string{Teacher}},
	{Method: "POST", Pattern: "/jadwal/kelas/{classID}", Module: "schedule", Kind: KindByID, Roles: adminTU,
		Requests: func(own, o *School) []Request {
			return []Request{
				// Kelas sekolah lain.
				{Path: "/jadwal/kelas/" + o.ClassID, Form: url.Values{"subject_id": {o.SubjectID}, "day": {"2"}, "start": {"09:00"}, "end": {"09:40"}}},
				// Kelas sendiri dengan mapel sekolah lain.
				{Path: "/jadwal/kelas/" + own.ClassID, Form: url.Values{"subject_id": {o.SubjectID}, "day": {"2"}, "start": {"09:00"}, "end": {"09:40"}}},
			}
		}},
	{Method: "GET", Pattern: "/jadwal/slot/{id}/ubah", Module: "schedule", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/jadwal/slot/" + o.SlotID + "/ubah"} })},
	{Method: "POST", Pattern: "/jadwal/slot/{id}", Module: "schedule", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/jadwal/slot/" + o.SlotID }, func(o *School) url.Values {
			return url.Values{"subject_id": {o.SubjectID}, "day": {"3"}, "start": {"10:00"}, "end": {"10:40"}}
		})},
	{Method: "POST", Pattern: "/jadwal/slot/{id}/hapus", Module: "schedule", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/jadwal/slot/" + o.SlotID + "/hapus" }, nil)},

	// Komponen nilai (T69): admin/TU semua mapel, guru mapel yang ditugaskan.
	{Method: "GET", Pattern: "/nilai/komponen/", Module: "grades", Kind: KindPage, Roles: []string{Admin, Teacher},
		Requests: paths(func(o *School) []string { return []string{"/nilai/komponen/", "/nilai/komponen/?mapel=" + o.SubjectID} })},
	{Method: "POST", Pattern: "/nilai/komponen/{subjectID}", Module: "grades", Kind: KindByID, Roles: []string{Admin, Teacher},
		Requests: post(func(o *School) string { return "/nilai/komponen/" + o.SubjectID }, func(*School) url.Values {
			return url.Values{"rows": {"1"}, "id_0": {""}, "name_0": {"Uji lintas"}, "weight_0": {"100"}, "active_0": {"on"}}
		})},

	// Input nilai (T70): hanya guru pengampu kelas–mapel.
	{Method: "GET", Pattern: "/nilai/input/", Module: "grades", Kind: KindPage, Roles: []string{Teacher},
		Requests: paths(func(o *School) []string {
			return []string{"/nilai/input/", "/nilai/input/?kelas=" + o.ClassID + "&mapel=" + o.SubjectID,
				"/nilai/input/?kelas=" + o.ClassID + "&mapel=" + o.SubjectID + "&komponen=" + o.GradeComponentID}
		})},
	{Method: "POST", Pattern: "/nilai/input/{classID}/{componentID}", Module: "grades", Kind: KindByID, Roles: []string{Teacher},
		Requests: func(own, o *School) []Request {
			form := func(s *School) url.Values { return url.Values{"g_" + s.EnrollmentIDs[0]: {"80"}} }
			return []Request{
				{Path: "/nilai/input/" + o.ClassID + "/" + o.GradeComponentID, Form: form(o)},
				// Kelas sendiri dengan komponen sekolah lain, dan sebaliknya.
				{Path: "/nilai/input/" + own.ClassID + "/" + o.GradeComponentID, Form: form(own)},
				{Path: "/nilai/input/" + o.ClassID + "/" + own.GradeComponentID, Form: form(o)},
			}
		}},

	// Rekap nilai (T72): admin/TU dan kepala sekolah semua kelas, guru mapel
	// yang diajar dan kelas perwaliannya.
	{Method: "GET", Pattern: "/nilai/rekap/", Module: "grades", Kind: KindPage, Roles: []string{Admin, Principal, Teacher},
		Requests: paths(func(o *School) []string {
			return []string{"/nilai/rekap/", "/nilai/rekap/?kelas=" + o.ClassID, "/nilai/rekap/?kelas=" + o.ClassID + "&mapel=" + o.SubjectID}
		})},

	// Rapor PDF (T74): admin/TU dan wali kelas.
	{Method: "GET", Pattern: "/nilai/rapor/{enrollmentID}.pdf", Module: "grades", Kind: KindByID, Roles: []string{Admin, Teacher},
		Requests: paths(func(o *School) []string { return []string{"/nilai/rapor/" + o.EnrollmentIDs[0] + ".pdf?semester=1"} })},

	// Konfigurasi laporan (T77): peran yang punya sumber laporan; ubah dan
	// hapus hanya pemilik (Admin di data uji).
	{Method: "GET", Pattern: "/laporan/konfigurasi/", Kind: KindPage, Roles: financeRead,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/konfigurasi/", "/laporan/konfigurasi/?ubah=" + o.ReportConfigID}
		})},
	{Method: "POST", Pattern: "/laporan/konfigurasi/", Kind: KindOwn, Roles: financeRead,
		Requests: post(func(*School) string { return "/laporan/konfigurasi/" }, func(*School) url.Values {
			return url.Values{"name": {"Uji lintas"}, "source": {"payments"}, "columns_payments": {"nis", "amount"}}
		})},
	{Method: "POST", Pattern: "/laporan/konfigurasi/{id}", Kind: KindByID, Roles: financeRead, ControlRoles: adminTU,
		Requests: post(func(o *School) string { return "/laporan/konfigurasi/" + o.ReportConfigID }, func(*School) url.Values {
			return url.Values{"name": {"Diubah"}, "source": {"attendance"}, "columns_attendance": {"nis"}, "shared": {"on"}}
		})},
	{Method: "GET", Pattern: "/laporan/konfigurasi/{id}.xlsx", Kind: KindByID, Roles: financeRead, ControlRoles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/laporan/konfigurasi/" + o.ReportConfigID + ".xlsx"} })},
	{Method: "GET", Pattern: "/laporan/konfigurasi/{id}.pdf", Kind: KindByID, Roles: financeRead, ControlRoles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/laporan/konfigurasi/" + o.ReportConfigID + ".pdf"} })},
	{Method: "POST", Pattern: "/laporan/konfigurasi/{id}/hapus", Kind: KindByID, Roles: financeRead, ControlRoles: adminTU,
		Requests: post(func(o *School) string { return "/laporan/konfigurasi/" + o.ReportConfigID + "/hapus" }, nil)},

	// Pembangun laporan (T78): peran yang punya sumber laporan.
	{Method: "GET", Pattern: "/laporan/bangun/", Kind: KindPage, Roles: financeRead,
		Requests: paths(func(o *School) []string {
			return []string{"/laporan/bangun/", "/laporan/bangun/?konfigurasi=" + o.ReportConfigID}
		})},
	{Method: "POST", Pattern: "/laporan/bangun/pratinjau", Kind: KindOwn, Roles: financeRead,
		Requests: post(func(*School) string { return "/laporan/bangun/pratinjau" }, func(*School) url.Values {
			return url.Values{"source": {"payments"}, "columns_payments": {"nis", "student_name", "amount"}}
		})},
	{Method: "POST", Pattern: "/laporan/bangun/simpan", Kind: KindFormRef, Roles: financeRead,
		Requests: post(func(*School) string { return "/laporan/bangun/simpan" }, func(o *School) url.Values {
			return url.Values{"config_id": {o.ReportConfigID}, "name": {"Diambil"}, "source": {"payments"}, "columns_payments": {"nis"}}
		})},

	// Rekap pembayaran (T80): bendahara dan kepala sekolah.
	{Method: "GET", Pattern: "/keuangan/rekap-pembayaran", Module: "billing", Kind: KindPage, Roles: []string{Treasurer, Principal},
		Requests: paths(func(*School) []string {
			return []string{"/keuangan/rekap-pembayaran", "/keuangan/rekap-pembayaran?mode=harian&dari=2026-09-01&sampai=2026-09-30",
				"/keuangan/rekap-pembayaran?mode=bulanan&tahun=2026"}
		})},
	{Method: "GET", Pattern: "/keuangan/rekap-pembayaran.xlsx", Module: "billing", Kind: KindPage, Roles: []string{Treasurer, Principal},
		Requests: paths(func(*School) []string { return []string{"/keuangan/rekap-pembayaran.xlsx?mode=bulanan&tahun=2026"} })},

	// Impor nilai lewat mapping (T82): hanya admin/TU.
	{Method: "GET", Pattern: "/impor/nilai", Module: "grades", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/impor/nilai", Module: "grades", Kind: KindOwn, Roles: adminTU, Requests: post(func(*School) string { return "/impor/nilai" }, nil)},

	// Mapping impor (T81): hanya admin/TU.
	{Method: "GET", Pattern: "/impor/mapping", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/impor/mapping", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/impor/mapping" }, func(*School) url.Values {
			return url.Values{"step": {"simpan"}, "name": {"Uji lintas"}, "target": {"students"}, "header": {"NIS", "Nama", "Kelas"},
				"map_0": {"NIS"}, "map_1": {"Nama"}, "map_4": {"Kelas"}}
		})},

	// Pengumuman.
	{Method: "GET", Pattern: "/pengumuman", Module: "announcements", Kind: KindPage, Roles: staff},
	{Method: "POST", Pattern: "/pengumuman", Module: "announcements", Kind: KindFormRef, Roles: schoolData,
		Requests: post(func(*School) string { return "/pengumuman" }, func(o *School) url.Values {
			return url.Values{"title": {"Uji lintas"}, "body": {"Isi"}, "class_id": {o.ClassID}}
		})},
	{Method: "GET", Pattern: "/pengumuman/baru", Module: "announcements", Kind: KindPage, Roles: schoolData},

	// Portal orang tua.
	{Method: "GET", Pattern: "/portal/", Module: "portal", Kind: KindPage, Roles: parent},
	{Method: "GET", Pattern: "/portal/pengumuman", Module: "announcements", Kind: KindPage, Roles: parent},
	{Method: "GET", Pattern: "/portal/siswa/{id}", Module: "portal", Kind: KindByID, Roles: parent,
		Requests: paths(func(o *School) []string { return []string{"/portal/siswa/" + o.StudentIDs[0]} })},
	{Method: "GET", Pattern: "/portal/siswa/{id}/tagihan", Module: "billing", Kind: KindByID, Roles: parent,
		Requests: paths(func(o *School) []string { return []string{"/portal/siswa/" + o.StudentIDs[0] + "/tagihan"} })},
	{Method: "GET", Pattern: "/portal/siswa/{id}/jadwal", Module: "schedule", Kind: KindByID, Roles: parent,
		Requests: paths(func(o *School) []string { return []string{"/portal/siswa/" + o.StudentIDs[0] + "/jadwal"} })},
	{Method: "GET", Pattern: "/portal/siswa/{id}/nilai", Module: "grades", Kind: KindByID, Roles: parent,
		Requests: paths(func(o *School) []string { return []string{"/portal/siswa/" + o.StudentIDs[0] + "/nilai"} })},
	// Ujian anak (T151): fixture sekolah tanpa modul ujian aktif, jadi
	// tanpa kontrol positif (diuji di paket portal: TestChildExams).
	{Method: "GET", Pattern: "/portal/siswa/{id}/ujian", Module: "exam", Kind: KindByID, Roles: parent, ControlRoles: []string{},
		Requests: paths(func(o *School) []string { return []string{"/portal/siswa/" + o.StudentIDs[0] + "/ujian"} })},
	// Hasil untuk siswa dan orang tua (T151).
	{Method: "GET", Pattern: "/portal/siswa/{id}/rapor.pdf", Module: "grades", Kind: KindByID, Roles: parent,
		Requests: paths(func(o *School) []string {
			return []string{"/portal/siswa/" + o.StudentIDs[0] + "/rapor.pdf?semester=1"}
		})},
	{Method: "GET", Pattern: "/portal/pembayaran/{id}/kuitansi", Module: "billing", Kind: KindByID, Roles: parent,
		Requests: paths(func(o *School) []string { return []string{"/portal/pembayaran/" + o.PaymentID + "/kuitansi"} })},

	// Bantu Dapodik (T98).
	{Method: "GET", Pattern: "/dapodik/", Module: "dapodik", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/dapodik/", "/dapodik/?kelas=" + o.ClassID + "&status=belum"}
		})},
	{Method: "GET", Pattern: "/dapodik/siswa/{id}/fpd.pdf", Module: "dapodik", Kind: KindByID, Roles: schoolData,
		Requests: paths(func(o *School) []string { return []string{"/dapodik/siswa/" + o.StudentIDs[0] + "/fpd.pdf"} })},
	{Method: "GET", Pattern: "/dapodik/kelas/{classID}/fpd.pdf", Module: "dapodik", Kind: KindByID, Roles: schoolData,
		Requests: paths(func(o *School) []string { return []string{"/dapodik/kelas/" + o.ClassID + "/fpd.pdf"} })},
	{Method: "GET", Pattern: "/dapodik/fpd-kosong.pdf", Module: "dapodik", Kind: KindPage, Roles: schoolData},
	{Method: "GET", Pattern: "/dapodik/cek", Module: "dapodik", Kind: KindPage, Roles: schoolData},
	{Method: "GET", Pattern: "/dapodik/ekspor-pd.xlsx", Module: "dapodik", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/dapodik/ekspor-pd.xlsx", "/dapodik/ekspor-pd.xlsx?kelas=" + o.ClassID}
		})},
	{Method: "GET", Pattern: "/dapodik/siswa/{id}", Module: "dapodik", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/dapodik/siswa/" + o.StudentIDs[0]} })},
	// Isi format import nilai (T102): kelas/mapel sekolah lain ditolak.
	{Method: "GET", Pattern: "/dapodik/nilai", Module: "dapodik", Kind: KindPage, Roles: []string{Admin, Teacher}},
	{Method: "POST", Pattern: "/dapodik/nilai", Module: "dapodik", Kind: KindFormRef, Roles: []string{Admin, Teacher},
		Requests: post(func(*School) string { return "/dapodik/nilai" }, func(o *School) url.Values {
			return url.Values{"class_id": {o.ClassID}, "subject_id": {o.SubjectID}}
		})},
	{Method: "GET", Pattern: "/dapodik/nilai/unduh", Module: "dapodik", Kind: KindPage, Roles: []string{Admin, Teacher}},
	// Impor ekspor Dapodik (T101): hanya data sekolah sendiri (file + sesi).
	{Method: "GET", Pattern: "/dapodik/impor", Module: "dapodik", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/dapodik/impor", Module: "dapodik", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/dapodik/impor" }, func(*School) url.Values { return url.Values{} })},
	{Method: "POST", Pattern: "/dapodik/impor/terapkan", Module: "dapodik", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/dapodik/impor/terapkan" }, func(*School) url.Values { return url.Values{} })},
	{Method: "POST", Pattern: "/dapodik/siswa/{id}/tandai", Module: "dapodik", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/dapodik/siswa/" + o.StudentIDs[0] + "/tandai" }, func(*School) url.Values {
			return url.Values{"entered": {"1"}}
		})},

	// Pendaftar (T93).
	{Method: "GET", Pattern: "/pendaftaran/", Module: "admission", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/pendaftaran/", "/pendaftaran/?periode=" + o.AdmissionPeriodID + "&jalur=" + o.AdmissionTrackID, "/pendaftaran/?q=SPMB"}
		})},
	{Method: "GET", Pattern: "/pendaftaran/{id}/bukti.pdf", Module: "admission", Kind: KindByID, Roles: schoolData,
		Requests: paths(func(o *School) []string { return []string{"/pendaftaran/" + o.ApplicantID + "/bukti.pdf"} })},
	{Method: "GET", Pattern: "/pendaftaran/{id}", Module: "admission", Kind: KindByID, Roles: schoolData,
		Requests: paths(func(o *School) []string { return []string{"/pendaftaran/" + o.ApplicantID} })},
	// Kontrol positif dilewati: fixture tidak punya objek terenkripsi di
	// storage. Jalur sukses diuji di paket admission (TestDocuments).
	{Method: "GET", Pattern: "/pendaftaran/{id}/dokumen/{docID}", Module: "admission", Kind: KindByID, Roles: schoolData, ControlRoles: []string{},
		Requests: func(own, o *School) []Request {
			return []Request{
				{Path: "/pendaftaran/" + o.ApplicantID + "/dokumen/" + o.ApplicantDocID},
				{Path: "/pendaftaran/" + own.ApplicantID + "/dokumen/" + o.ApplicantDocID},
			}
		}},
	// Biaya formulir (T94). Urutan penting untuk kontrol positif: kuitansi
	// dan catat (409, sudah lunas) sebelum batal.
	{Method: "GET", Pattern: "/pendaftaran/biaya", Module: "admission", Kind: KindPage, Roles: []string{Treasurer, Admin, Principal}},
	{Method: "GET", Pattern: "/pendaftaran/{id}/biaya/{paymentID}/kuitansi", Module: "admission", Kind: KindByID, Roles: []string{Treasurer, Admin, Principal},
		Requests: func(own, o *School) []Request {
			reqs := []Request{{Path: "/pendaftaran/" + o.ApplicantID + "/biaya/" + o.ApplicantFeePaymentID + "/kuitansi"}}
			if own.ID != o.ID {
				reqs = append(reqs, Request{Path: "/pendaftaran/" + own.ApplicantID + "/biaya/" + o.ApplicantFeePaymentID + "/kuitansi"})
			}
			return reqs
		}},
	{Method: "POST", Pattern: "/pendaftaran/{id}/biaya", Module: "admission", Kind: KindByID, Roles: []string{Treasurer},
		Requests: post(func(o *School) string { return "/pendaftaran/" + o.ApplicantID + "/biaya" }, func(*School) url.Values {
			return url.Values{"paid_on": {"2026-11-01"}, "method": {"tunai"}}
		})},
	{Method: "POST", Pattern: "/pendaftaran/{id}/biaya/batal", Module: "admission", Kind: KindByID, Roles: []string{Treasurer},
		Requests: post(func(o *School) string { return "/pendaftaran/" + o.ApplicantID + "/biaya/batal" }, func(*School) url.Values {
			return url.Values{"void_reason": {"Uji lintas"}}
		})},

	// Rekap pendaftaran (T97).
	{Method: "GET", Pattern: "/pendaftaran/rekap", Module: "admission", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/pendaftaran/rekap", "/pendaftaran/rekap?periode=" + o.AdmissionPeriodID}
		})},
	{Method: "GET", Pattern: "/pendaftaran/rekap/pendaftar.xlsx", Module: "admission", Kind: KindPage, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/pendaftaran/rekap/pendaftar.xlsx?periode=" + o.AdmissionPeriodID}
		})},

	// Penerimaan menjadi siswa (T96).
	{Method: "GET", Pattern: "/pendaftaran/terima", Module: "admission", Kind: KindPage, Roles: schoolData},
	{Method: "POST", Pattern: "/pendaftaran/terima", Module: "admission", Kind: KindFormRef, Roles: adminTU,
		Requests: post(func(*School) string { return "/pendaftaran/terima" }, func(o *School) url.Values {
			return url.Values{"pick": {o.ApplicantID}}
		})},
	// Pengaturan pendaftaran dan Jadikan siswa per pendaftar (T120). Pendaftar
	// fixture berstatus dikirim, jadi kontrol positif mendapat 409.
	{Method: "GET", Pattern: "/pendaftaran/pengaturan", Module: "admission", Kind: KindPage, Roles: schoolData},
	{Method: "POST", Pattern: "/pendaftaran/pengaturan", Module: "admission", Kind: KindOwn, Roles: adminTU,
		Requests: post(func(*School) string { return "/pendaftaran/pengaturan" }, func(*School) url.Values {
			return url.Values{"reregistration": {"true"}, "mode": {"tahun"}, "year_digits": {"4"}, "seq_digits": {"4"}}
		})},
	{Method: "POST", Pattern: "/pendaftaran/{id}/jadikan-siswa", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/" + o.ApplicantID + "/jadikan-siswa" }, nil)},
	// Metode seleksi (T118): koreksi nilai jalur dan verifikasi massal.
	{Method: "POST", Pattern: "/pendaftaran/{id}/nilai-jalur", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/" + o.ApplicantID + "/nilai-jalur" }, func(*School) url.Values {
			return url.Values{"track_score": {"80"}}
		})},
	{Method: "POST", Pattern: "/pendaftaran/verifikasi", Module: "admission", Kind: KindFormRef, Roles: adminTU,
		Requests: post(func(*School) string { return "/pendaftaran/verifikasi" }, func(o *School) url.Values {
			return url.Values{"pick": {o.ApplicantID}}
		})},
	{Method: "POST", Pattern: "/pendaftaran/{id}/daftar-ulang", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/" + o.ApplicantID + "/daftar-ulang" }, func(*School) url.Values {
			return url.Values{"done": {"1"}}
		})},

	// Input pendaftar oleh TU (T92). Pendaftar fixture berstatus dikirim,
	// jadi kontrol positif diarahkan (303) ke halaman detail.
	{Method: "GET", Pattern: "/pendaftaran/baru", Module: "admission", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/pendaftaran/baru", Module: "admission", Kind: KindFormRef, Roles: adminTU,
		Requests: post(func(*School) string { return "/pendaftaran/baru" }, func(o *School) url.Values {
			return url.Values{"choice": {o.AdmissionPeriodID + "|" + o.AdmissionTrackID}, "full_name": {"Lintas"}, "gender": {"L"}, "birth_date": {"2019-01-01"}, "phone": {"081200000000"}, "consent": {"on"}}
		})},
	{Method: "GET", Pattern: "/pendaftaran/{id}/isi/{step}", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/pendaftaran/" + o.ApplicantID + "/isi/calon"} })},
	{Method: "POST", Pattern: "/pendaftaran/{id}/isi/{step}", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/" + o.ApplicantID + "/isi/alamat" }, func(*School) url.Values {
			return url.Values{"street": {"Lintas"}}
		})},
	{Method: "POST", Pattern: "/pendaftaran/{id}/dokumen/{type}", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/" + o.ApplicantID + "/dokumen/kk" }, nil)},
	{Method: "POST", Pattern: "/pendaftaran/{id}/kirim", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/" + o.ApplicantID + "/kirim" }, nil)},
	{Method: "POST", Pattern: "/pendaftaran/{id}/status", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/" + o.ApplicantID + "/status" }, func(*School) url.Values {
			return url.Values{"status": {"diverifikasi"}}
		})},
	{Method: "POST", Pattern: "/pendaftaran/{id}/hapus-data", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/" + o.ApplicantID + "/hapus-data" }, func(*School) url.Values {
			return url.Values{"confirm": {"on"}}
		})},

	// Pendaftaran (T89).
	{Method: "GET", Pattern: "/pendaftaran/periode/", Module: "admission", Kind: KindPage, Roles: schoolData},
	{Method: "GET", Pattern: "/pendaftaran/periode/baru", Module: "admission", Kind: KindPage, Roles: adminTU},
	{Method: "POST", Pattern: "/pendaftaran/periode/logo", Module: "admission", Kind: KindOwn, Roles: adminTU, Requests: post(func(*School) string { return "/pendaftaran/periode/logo" }, nil)},
	{Method: "POST", Pattern: "/pendaftaran/periode/logo/hapus", Module: "admission", Kind: KindOwn, Roles: adminTU, Requests: post(func(*School) string { return "/pendaftaran/periode/logo/hapus" }, nil)},
	{Method: "POST", Pattern: "/pendaftaran/periode/", Module: "admission", Kind: KindFormRef, Roles: adminTU,
		Requests: post(func(*School) string { return "/pendaftaran/periode/" }, func(o *School) url.Values {
			return url.Values{"name": {"Lintas"}, "academic_year_id": {o.YearID}, "target_grade": {"1"}, "opens_on": {"2026-12-01"}, "closes_on": {"2027-01-31"}}
		})},
	{Method: "GET", Pattern: "/pendaftaran/periode/{id}", Module: "admission", Kind: KindByID, Roles: schoolData,
		Requests: paths(func(o *School) []string { return []string{"/pendaftaran/periode/" + o.AdmissionPeriodID} })},
	{Method: "GET", Pattern: "/pendaftaran/periode/{id}/ubah", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/pendaftaran/periode/" + o.AdmissionPeriodID + "/ubah"} })},
	// Wizard gelombang dan salin gelombang (T117).
	{Method: "GET", Pattern: "/pendaftaran/periode/{id}/atur/{step}", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string {
			base := "/pendaftaran/periode/" + o.AdmissionPeriodID + "/atur/"
			return []string{base + "dasar", base + "jadwal", base + "jalur", base + "dokumen"}
		})},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/atur/{step}", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/periode/" + o.AdmissionPeriodID + "/atur/dasar" }, func(o *School) url.Values {
			return url.Values{"name": {"Diubah"}, "academic_year_id": {o.YearID}, "target_grade": {"1"}}
		})},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/salin", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/periode/" + o.AdmissionPeriodID + "/salin" }, nil)},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/status", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/periode/" + o.AdmissionPeriodID + "/status" }, func(*School) url.Values {
			return url.Values{"status": {"buka"}}
		})},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/hapus", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/periode/" + o.AdmissionPeriodID + "/hapus" }, nil)},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/jalur", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/periode/" + o.AdmissionPeriodID + "/jalur" }, func(*School) url.Values {
			return url.Values{"track_name": {"Lintas"}}
		})},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/jalur/{trackID}", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: admissionTrack("")},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/jalur/{trackID}/hapus", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: admissionTrack("/hapus")},

	// Tes seleksi (T93a).
	{Method: "GET", Pattern: "/pendaftaran/periode/{id}/tes", Module: "admission", Kind: KindByID, Roles: schoolData,
		Requests: paths(func(o *School) []string { return []string{"/pendaftaran/periode/" + o.AdmissionPeriodID + "/tes"} })},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/tes", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/periode/" + o.AdmissionPeriodID + "/tes" }, func(*School) url.Values {
			return url.Values{"test_name": {"Lintas"}, "weight": {"10"}}
		})},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/tes/{testID}", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: admissionTest("", url.Values{"test_name": {"Diubah"}, "weight": {"100"}})},
	{Method: "GET", Pattern: "/pendaftaran/periode/{id}/tes/{testID}/nilai", Module: "admission", Kind: KindByID, Roles: schoolData,
		Requests: func(own, o *School) []Request {
			return []Request{{Path: "/pendaftaran/periode/" + o.AdmissionPeriodID + "/tes/" + o.AdmissionTestID + "/nilai"}}
		}},
	// Lembar penilaian PDF (T119).
	{Method: "GET", Pattern: "/pendaftaran/periode/{id}/tes/{testID}/lembar.pdf", Module: "admission", Kind: KindByID, Roles: schoolData,
		Requests: func(own, o *School) []Request {
			return []Request{{Path: "/pendaftaran/periode/" + o.AdmissionPeriodID + "/tes/" + o.AdmissionTestID + "/lembar.pdf"}}
		}},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/tes/{testID}/nilai", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: admissionTest("/nilai", url.Values{})},
	// Tes seleksi online (T149): fixture memakai tes kertas, jadi tanpa
	// kontrol positif (diuji di paket admission: TestOnlineAdmissionTest).
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/tes/{testID}/peserta", Module: "admission", Kind: KindByID, Roles: adminTU, ControlRoles: []string{},
		Requests: admissionTest("/peserta", url.Values{"session_id": {""}})},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/latihan", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: post(func(o *School) string { return "/pendaftaran/periode/" + o.AdmissionPeriodID + "/latihan" }, nil)},
	// Peringkat dan hasil (T93b).
	{Method: "GET", Pattern: "/pendaftaran/periode/{id}/hasil", Module: "admission", Kind: KindByID, Roles: schoolData,
		Requests: paths(func(o *School) []string { return []string{"/pendaftaran/periode/" + o.AdmissionPeriodID + "/hasil"} })},
	{Method: "GET", Pattern: "/pendaftaran/periode/{id}/hasil.xlsx", Module: "admission", Kind: KindByID, Roles: schoolData,
		Requests: paths(func(o *School) []string {
			return []string{"/pendaftaran/periode/" + o.AdmissionPeriodID + "/hasil.xlsx"}
		})},
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/hasil", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: func(own, o *School) []Request {
			reqs := []Request{{Path: "/pendaftaran/periode/" + o.AdmissionPeriodID + "/hasil", Form: url.Values{}}}
			if own.ID != o.ID { // pendaftar sekolah lain lewat periode sendiri
				reqs = append(reqs, Request{Path: "/pendaftaran/periode/" + own.AdmissionPeriodID + "/hasil", Form: url.Values{"result_" + o.ApplicantID: {"diterima"}}})
			}
			return reqs
		}},
	// Tes fixture punya nilai, jadi kontrol positif mendapat 409.
	{Method: "POST", Pattern: "/pendaftaran/periode/{id}/tes/{testID}/hapus", Module: "admission", Kind: KindByID, Roles: adminTU,
		Requests: admissionTest("/hapus", url.Values{})},

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
	{Method: "GET", Pattern: "/siswa/{id}/data-rinci", Kind: KindByID, Roles: schoolData,
		Requests: paths(func(o *School) []string { return []string{"/siswa/" + o.StudentIDs[0] + "/data-rinci"} })},
	// Ekspor data pribadi siswa (UU PDP, T153).
	{Method: "GET", Pattern: "/siswa/{id}/data-pribadi.json", Kind: KindByID, Roles: adminTU,
		Requests: paths(func(o *School) []string { return []string{"/siswa/" + o.StudentIDs[0] + "/data-pribadi.json"} })},
	{Method: "POST", Pattern: "/siswa/{id}/data-rinci", Kind: KindByID, Roles: adminTU,
		Requests: func(_, o *School) []Request {
			id := o.StudentIDs[0]
			return []Request{
				{Path: "/siswa/" + id + "/data-rinci", Form: url.Values{"section": {"identitas"}, "nik": {"3201010101019999"}, "religion": {"01"}}},
				{Path: "/siswa/" + id + "/data-rinci", Form: url.Values{"section": {"ibu"}, "full_name": {"Ibu Lintas"}, "nik": {"3201010101018888"}}},
			}
		}},
	{Method: "POST", Pattern: "/siswa/{id}/wali/{guardianID}/akun", Module: "portal", Kind: KindByID, Roles: adminTU,
		Requests: guardianAccount("/akun")},
	{Method: "POST", Pattern: "/siswa/{id}/wali/{guardianID}/akun/kirim-ulang", Module: "portal", Kind: KindByID, Roles: adminTU,
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

// admissionTrack menguji jalur sekolah lain, termasuk jalur sekolah lain di
// bawah periode sekolah sendiri.
func admissionTrack(suffix string) func(own, other *School) []Request {
	return func(own, o *School) []Request {
		form := url.Values{"track_name": {"Diubah"}}
		reqs := []Request{{Path: "/pendaftaran/periode/" + o.AdmissionPeriodID + "/jalur/" + o.AdmissionTrackID + suffix, Form: form}}
		if own.ID != o.ID { // kontrol positif memanggil dengan own == other
			reqs = append(reqs, Request{Path: "/pendaftaran/periode/" + own.AdmissionPeriodID + "/jalur/" + o.AdmissionTrackID + suffix, Form: form})
		}
		return reqs
	}
}

// admissionTest menguji tes sekolah lain, termasuk tes sekolah lain di bawah
// periode sekolah sendiri.
func admissionTest(suffix string, form url.Values) func(own, other *School) []Request {
	return func(own, o *School) []Request {
		reqs := []Request{{Path: "/pendaftaran/periode/" + o.AdmissionPeriodID + "/tes/" + o.AdmissionTestID + suffix, Form: form}}
		if own.ID != o.ID {
			reqs = append(reqs, Request{Path: "/pendaftaran/periode/" + own.AdmissionPeriodID + "/tes/" + o.AdmissionTestID + suffix, Form: form})
		}
		return reqs
	}
}
