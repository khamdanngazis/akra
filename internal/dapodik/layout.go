// Package dapodik membantu sekolah memindahkan data ke aplikasi Dapodik:
// lembar salin (T98), formulir F-PD (T99), ekspor format daftar PD (T100),
// dan cek kesiapan (T103).
package dapodik

// LayoutVersion adalah versi aplikasi Dapodik yang menjadi acuan urutan
// field. Diambil dari form Peserta Didik di classic/app.js installer
// Dapodik 2027 (Admin.view._components.form.PesertaDidik,
// RegistrasiPesertaDidik, dan PesertaDidikLongitudinal).
const LayoutVersion = "2027"

// FieldKind menentukan cara nilai ditampilkan dan disalin.
type FieldKind int

const (
	KindText   FieldKind = iota // teks apa adanya
	KindDate                    // dd/mm/yyyy, seperti input tanggal Dapodik
	KindChoice                  // label pilihan persis seperti combobox Dapodik
	KindNumber                  // angka
)

// LayoutField adalah satu field di form Dapodik.
type LayoutField struct {
	// Label persis seperti di form Dapodik.
	Label string
	// Key adalah sumber data di AKRA (lihat values di sheet.go). Kosong
	// berarti AKRA tidak menyimpan data ini; field tetap ditampilkan agar
	// urutannya sama dengan Dapodik.
	Key  string
	Kind FieldKind
	// Note adalah keterangan untuk operator, mis. "diisi oleh pusat".
	Note string
}

// LayoutSection adalah satu bagian form Dapodik.
type LayoutSection struct {
	Title  string
	Fields []LayoutField
}

// Layout adalah urutan bagian dan field form Peserta Didik Dapodik 2027.
var Layout = []LayoutSection{
	{"Data Pribadi", []LayoutField{
		{Label: "Nama", Key: "full_name"},
		{Label: "Jenis kelamin", Key: "gender", Kind: KindChoice},
		{Label: "NISN", Key: "nisn"},
		{Label: "Kewarganegaraan", Key: "citizenship", Kind: KindChoice},
		{Label: "NIK", Key: "nik"},
		{Label: "No KK", Key: "kk_number"},
		{Label: "Tempat lahir", Key: "birth_place"},
		{Label: "Tanggal lahir", Key: "birth_date", Kind: KindDate},
		{Label: "No Registrasi Akta Lahir", Key: "birth_cert_number"},
		{Label: "Agama & Kepercayaan", Key: "religion", Kind: KindChoice},
		{Label: "Kebutuhan khusus", Key: "special_needs", Kind: KindChoice},
		{Label: "Alamat jalan", Key: "street"},
		{Label: "RT", Key: "rt", Kind: KindNumber},
		{Label: "RW", Key: "rw", Kind: KindNumber},
		{Label: "Nama dusun", Key: "hamlet"},
		{Label: "Desa/Kelurahan", Key: "village"},
		{Label: "Kecamatan", Key: "district"},
		{Label: "Kode pos", Key: "postal_code", Kind: KindNumber},
		{Label: "Lintang", Key: "latitude", Kind: KindNumber},
		{Label: "Bujur", Key: "longitude", Kind: KindNumber},
		{Label: "Tempat tinggal", Key: "residence_type", Kind: KindChoice},
		{Label: "Moda transportasi", Key: "transport_mode", Kind: KindChoice},
		{Label: "No KKS (Kartu Keluarga Sejahtera)", Key: "kks_number"},
		{Label: "Anak ke-berapa (berdasarkan KK)", Key: "child_order", Kind: KindNumber},
		{Label: "Pekerjaan (diperuntukan warga belajar)", Note: "khusus warga belajar, tidak disimpan AKRA"},
		{Label: "Penerima KPS/PKH", Note: "tidak disimpan AKRA"},
		{Label: "No KPS/PKH", Note: "tidak disimpan AKRA"},
		{Label: "Penerima KIP (Kartu Indonesia Pintar)", Key: "has_kip", Kind: KindChoice},
		{Label: "No KIP", Key: "kip_number"},
		{Label: "Usulan dari sekolah (Layak PIP)", Key: "pip_eligible", Kind: KindChoice},
		{Label: "Alasan layak PIP", Key: "pip_reason", Kind: KindChoice},
	}},
	{"Bank diperuntukan PIP (diisi oleh pusat)", []LayoutField{
		{Label: "Nama Bank", Note: "diisi oleh pusat"},
		{Label: "Nomor Rekening", Note: "diisi oleh pusat"},
		{Label: "Kantor Cabang Pembantu (KCP)", Note: "diisi oleh pusat"},
		{Label: "Rekening Atas Nama", Note: "diisi oleh pusat"},
	}},
	{"Data Ayah Kandung", []LayoutField{
		{Label: "Nama ayah", Key: "ayah.full_name"},
		{Label: "NIK Ayah", Key: "ayah.nik"},
		{Label: "Tahun lahir ayah", Key: "ayah.birth_year", Kind: KindNumber},
		{Label: "Pendidikan ayah", Key: "ayah.education", Kind: KindChoice},
		{Label: "Pekerjaan ayah", Key: "ayah.occupation", Kind: KindChoice},
		{Label: "Penghasilan ayah", Key: "ayah.income", Kind: KindChoice},
		{Label: "Berkebutuhan khusus ayah", Key: "ayah.special_needs", Kind: KindChoice},
	}},
	{"Data Ibu Kandung", []LayoutField{
		{Label: "Nama ibu", Key: "ibu.full_name"},
		{Label: "NIK Ibu", Key: "ibu.nik"},
		{Label: "Tahun lahir ibu", Key: "ibu.birth_year", Kind: KindNumber},
		{Label: "Pendidikan", Key: "ibu.education", Kind: KindChoice},
		{Label: "Pekerjaan ibu", Key: "ibu.occupation", Kind: KindChoice},
		{Label: "Penghasilan ibu", Key: "ibu.income", Kind: KindChoice},
		{Label: "Berkebutuhan khusus ibu", Key: "ibu.special_needs", Kind: KindChoice},
	}},
	{"Data Wali", []LayoutField{
		{Label: "Mempunyai wali", Key: "has_wali", Kind: KindChoice},
		{Label: "Nama wali", Key: "wali.full_name"},
		{Label: "NIK Wali", Key: "wali.nik"},
		{Label: "Tahun lahir wali", Key: "wali.birth_year", Kind: KindNumber},
		{Label: "Pendidikan wali", Key: "wali.education", Kind: KindChoice},
		{Label: "Pekerjaan wali", Key: "wali.occupation", Kind: KindChoice},
		{Label: "Penghasilan wali", Key: "wali.income", Kind: KindChoice},
	}},
	{"Kontak", []LayoutField{
		{Label: "Nomor telepon Rumah", Key: "home_phone"},
		{Label: "Nomor HP", Key: "mobile_phone"},
		{Label: "Email", Key: "email"},
	}},
	{"Pendaftaran Masuk", []LayoutField{
		{Label: "Jenis pendaftaran", Key: "registration_type", Kind: KindChoice},
		{Label: "Nomor Induk PD / NIS", Key: "nis"},
		{Label: "Tanggal masuk sekolah", Key: "entry_date", Kind: KindDate},
		{Label: "Sekolah asal", Key: "previous_school"},
		{Label: "Apakah pernah PAUD Formal (TK)", Note: "tidak disimpan AKRA"},
		{Label: "Apakah pernah PAUD Non Formal (KB/TPA/SPS)", Note: "tidak disimpan AKRA"},
		{Label: "Hobby", Note: "tidak disimpan AKRA"},
		{Label: "Cita-cita", Note: "tidak disimpan AKRA"},
	}},
	{"Pendaftaran Ujian Nasional Sekolah Menengah", []LayoutField{
		{Label: "No Peserta UN SMP/MTs", Key: "exam_number"},
		{Label: "No Seri Ijazah SMP/MTs", Key: "diploma_serial"},
		{Label: "No Seri SKHUN", Key: "skhun_number"},
	}},
	{"Data Periodik", []LayoutField{
		{Label: "Tinggi badan (cm)", Key: "height_cm", Kind: KindNumber},
		{Label: "Berat badan (kg)", Key: "weight_kg", Kind: KindNumber},
		{Label: "Lingkar kepala", Key: "head_cm", Kind: KindNumber},
		{Label: "Jarak rumah ke sekolah", Key: "distance_band", Kind: KindChoice},
		{Label: "Sebutkan (dalam kilometer)", Key: "distance_km", Kind: KindNumber},
		{Label: "Waktu tempuh ke sekolah (jam)", Key: "travel_hours", Kind: KindNumber},
		{Label: "Waktu tempuh ke sekolah (menit)", Key: "travel_rest_minutes", Kind: KindNumber},
		{Label: "Jumlah saudara kandung", Key: "siblings", Kind: KindNumber},
	}},
}
