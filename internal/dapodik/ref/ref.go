// Package ref berisi daftar pilihan (kode dan label) yang dipakai Dapodik
// untuk data peserta didik. Semua form, PDF, lembar salin, dan ekspor yang
// berkaitan dengan Dapodik wajib memakai daftar dari paket ini, bukan
// menyalin daftarnya sendiri (T85).
//
// Sumber: Formulir Peserta Didik (F-PD) di installer Dapodik 2027
// (files/formulir/F-PESERTA_DIDIK.pdf). Label combobox di aplikasi Dapodik
// diambil dari server saat aplikasi berjalan, sehingga tidak tersedia di
// installer; label di sini mengikuti F-PD dan perlu dicocokkan ulang dengan
// aplikasi Dapodik asli saat uji T105.
package ref

import (
	"errors"
	"fmt"
)

// Version adalah versi Dapodik yang menjadi acuan daftar di paket ini.
const Version = "2027"

// ErrUnknownCode dikembalikan jika kode tidak ada di daftar.
var ErrUnknownCode = errors.New("dapodik ref: unknown code")

// Option adalah satu pilihan. Code disimpan di database sebagai teks dua
// digit ("01"), bukan angka, agar sama dengan penulisan di F-PD.
type Option struct {
	Code  string
	Label string
}

// List adalah satu daftar referensi.
type List struct {
	// Key adalah nama teknis daftar, dipakai di pesan error dan uji.
	Key string
	// Multi bernilai true jika Dapodik mengizinkan lebih dari satu pilihan
	// (mis. kebutuhan khusus).
	Multi   bool
	Options []Option
}

// Valid melaporkan apakah code ada di daftar.
func (l List) Valid(code string) bool {
	_, ok := l.find(code)
	return ok
}

// Label mengembalikan label untuk code. Kode yang tidak dikenal
// mengembalikan ErrUnknownCode, bukan string kosong.
func (l List) Label(code string) (string, error) {
	if o, ok := l.find(code); ok {
		return o.Label, nil
	}
	return "", fmt.Errorf("%w: %s %q", ErrUnknownCode, l.Key, code)
}

// Code mengembalikan kode untuk label yang sama persis. Dipakai saat
// membaca file ekspor Dapodik yang menulis label, bukan kode (T101).
func (l List) Code(label string) (string, error) {
	for _, o := range l.Options {
		if o.Label == label {
			return o.Code, nil
		}
	}
	return "", fmt.Errorf("%w: %s label %q", ErrUnknownCode, l.Key, label)
}

func (l List) find(code string) (Option, bool) {
	for _, o := range l.Options {
		if o.Code == code {
			return o, true
		}
	}
	return Option{}, false
}

// Agama: F-PD nomor 9.
var Agama = List{Key: "agama", Options: []Option{
	{"01", "Islam"},
	{"02", "Kristen/ Protestan"},
	{"03", "Katholik"},
	{"04", "Hindu"},
	{"05", "Budha"},
	{"06", "Khonghucu"},
	{"07", "Kepercayaan Kepada Tuhan YME"},
}}

// Kewarganegaraan: F-PD nomor 10. Kode memakai singkatan, bukan angka.
var Kewarganegaraan = List{Key: "kewarganegaraan", Options: []Option{
	{"WNI", "Indonesia (WNI)"},
	{"WNA", "Asing (WNA)"},
}}

// KebutuhanKhusus: F-PD nomor 11, 34, dan 41. Boleh lebih dari satu.
// Label 08 tidak terbaca di teks F-PD; urutan huruf A–Q menunjukkan
// Laras (E). Label 14 di F-PD tertulis "Kesulitan Belajra" (salah ketik)
// dan di sini ditulis "Kesulitan Belajar".
var KebutuhanKhusus = List{Key: "kebutuhan_khusus", Multi: true, Options: []Option{
	{"01", "Tidak"},
	{"02", "Netra (A)"},
	{"03", "Rungu (B)"},
	{"04", "Grahita ringan (C)"},
	{"05", "Grahita Sedang (C1)"},
	{"06", "Daksa Ringan (D)"},
	{"07", "Daksa Sedang (D1)"},
	{"08", "Laras (E)"},
	{"09", "Wicara (F)"},
	{"10", "Tuna ganda (G)"},
	{"11", "Hiper aktif (H)"},
	{"12", "Cerdas Istimewa (i)"},
	{"13", "Bakat Istimewa (J)"},
	{"14", "Kesulitan Belajar (K)"},
	{"15", "Narkoba (N)"},
	{"16", "Indigo (O)"},
	{"17", "Down Sindrome (P)"},
	{"18", "Autis (Q)"},
}}

// TempatTinggal: F-PD nomor 21.
var TempatTinggal = List{Key: "tempat_tinggal", Options: []Option{
	{"01", "Bersama orang tua"},
	{"02", "Wali"},
	{"03", "Kos"},
	{"04", "Asrama"},
	{"05", "Panti Asuhan"},
}}

// ModaTransportasi: F-PD nomor 22.
var ModaTransportasi = List{Key: "moda_transportasi", Options: []Option{
	{"01", "Jalan kaki"},
	{"02", "Kendaraan pribadi"},
	{"03", "Kendaraan Umum/angkot/Pete-pete"},
	{"04", "Jemputan Sekolah"},
	{"05", "Kereta Api"},
	{"06", "Ojek"},
	{"07", "Andong/Bendi/Sado/ Dokar/Delman/Beca"},
	{"08", "Perahu penyebrangan/Rakit/Getek"},
	{"99", "Lainnya"},
}}

// Pekerjaan: F-PD nomor 24, 32, 39, dan 46. F-PD meminta memilih
// "Meninggal Dunia" untuk orang tua yang sudah wafat tetapi tidak mencetak
// kodenya; kode 98 perlu dicocokkan dengan Dapodik asli (T105).
var Pekerjaan = List{Key: "pekerjaan", Options: []Option{
	{"01", "Tidak bekerja"},
	{"02", "Nelayan"},
	{"03", "Petani"},
	{"04", "Peternak"},
	{"05", "PNS/TNI/POLRI"},
	{"06", "Karyawan Swasta"},
	{"07", "Pedagang Kecil"},
	{"08", "Pedagang Besar"},
	{"09", "Wiraswasta"},
	{"10", "Wirausaha"},
	{"11", "Buruh"},
	{"12", "Pensiunan"},
	{"98", "Meninggal Dunia"},
}}

// KodeMeninggal adalah kode Pekerjaan untuk orang tua yang sudah wafat.
// Orang tua dengan kode ini boleh tanpa NIK dan penghasilan (T87).
const KodeMeninggal = "98"

// Pendidikan: pendidikan terakhir orang tua/wali, F-PD nomor 31, 38, 45.
var Pendidikan = List{Key: "pendidikan", Options: []Option{
	{"01", "Tidak sekolah"},
	{"02", "Putus SD"},
	{"03", "SD Sederajat"},
	{"04", "SMP Sederajat"},
	{"05", "SMA Sederajat"},
	{"06", "D1"},
	{"07", "D2"},
	{"08", "D3"},
	{"09", "D4/S1"},
	{"10", "S2"},
	{"11", "S3"},
}}

// Penghasilan: penghasilan bulanan orang tua/wali, F-PD nomor 33, 40, 47.
var Penghasilan = List{Key: "penghasilan", Options: []Option{
	{"01", "< Rp. 500.000"},
	{"02", "Rp. 500.000-Rp.999.999"},
	{"03", "Rp. 1.000.000-Rp.1.999.999"},
	{"04", "Rp.2.000.000-Rp.4.999.999"},
	{"05", "Rp.5.000.000-Rp.20.000.000"},
	{"06", "> Rp.20.000.000"},
	{"07", "Tidak Berpenghasilan"},
}}

// AlasanPIP: alasan menolak PIP, F-PD nomor 27.
var AlasanPIP = List{Key: "alasan_pip", Options: []Option{
	{"01", "Dilarang pemda karena menerima bantuan serupa"},
	{"02", "Menolak"},
	{"03", "Sudah mampu"},
}}

// JenisKesejahteraan: F-PD Data Periodik nomor 8.
var JenisKesejahteraan = List{Key: "jenis_kesejahteraan", Options: []Option{
	{"01", "PKH"},
	{"02", "PIP"},
	{"03", "Kartu Perlindungan Sosial"},
	{"04", "Kartu Keluarga Sejahtera"},
	{"05", "Kartu Kesehatan"},
}}

// JenisPrestasi: F-PD bagian Prestasi.
var JenisPrestasi = List{Key: "jenis_prestasi", Options: []Option{
	{"01", "Sains"},
	{"02", "Seni"},
	{"03", "Olahraga"},
	{"04", "Lain-lain"},
}}

// TingkatPrestasi: F-PD bagian Prestasi.
var TingkatPrestasi = List{Key: "tingkat_prestasi", Options: []Option{
	{"01", "Sekolah"},
	{"02", "Kecamatan"},
	{"03", "Kabupaten"},
	{"04", "Provinsi"},
	{"05", "Nasional"},
	{"06", "Internasional"},
}}

// JenisBeasiswa: F-PD bagian Beasiswa.
var JenisBeasiswa = List{Key: "jenis_beasiswa", Options: []Option{
	{"01", "Anak berprestasi"},
	{"02", "Anak Miskin"},
	{"03", "Pendidikan"},
	{"04", "Unggulan"},
}}

// JenisPendaftaran: F-PD Registrasi nomor 2.
var JenisPendaftaran = List{Key: "jenis_pendaftaran", Options: []Option{
	{"01", "Siswa Baru"},
	{"02", "Pindahan"},
	{"03", "Kembali Bersekolah"},
}}

// AlasanKeluar: F-PD Pendaftaran Keluar nomor 1.
var AlasanKeluar = List{Key: "alasan_keluar", Options: []Option{
	{"01", "Mutasi"},
	{"02", "Dikeluarkan"},
	{"03", "Mengundurkan Diri"},
	{"04", "Putus Sekolah"},
	{"05", "Wafat"},
	{"06", "Hilang"},
}}

// All mengembalikan semua daftar, berurutan sesuai kemunculan di F-PD.
func All() []List {
	return []List{
		Agama, Kewarganegaraan, KebutuhanKhusus, TempatTinggal, ModaTransportasi,
		Pekerjaan, Pendidikan, Penghasilan, AlasanPIP, JenisKesejahteraan,
		JenisPrestasi, TingkatPrestasi, JenisBeasiswa, JenisPendaftaran, AlasanKeluar,
	}
}
