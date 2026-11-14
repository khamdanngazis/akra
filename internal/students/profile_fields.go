package students

import (
	"akra/internal/dapodik/ref"
)

// Jenis isian field profil.
type fieldKind int

const (
	kindText     fieldKind = iota // teks bebas, maks Max karakter
	kindNISN                      // 10 digit
	kindIdentity                  // NIK (WNI, 16 digit) atau KITAS (WNA)
	kindKK                        // No KK, 16 digit
	kindRef                       // satu kode dari Ref
	kindRefMulti                  // beberapa kode dari Ref, dipisah koma
	kindDigits                    // angka sebagai teks, panjang Min–Max digit
	kindInt                       // bilangan bulat Min–Max
	kindDecimal                   // desimal Min–Max
	kindBool                      // "true"/"false"
	kindDate                      // YYYY-MM-DD
	kindPhone                     // nomor telepon tanpa tanda baca
	kindEmail
	kindWilayah // kode desa/kelurahan (T110), mis. 32.73.04.1001
)

// ProfileField menjelaskan satu field data rinci siswa.
type ProfileField struct {
	Key   string // nama field di form dan audit
	Label string // label untuk pesan error dan form
	Kind  fieldKind
	Ref   *ref.List // untuk kindRef dan kindRefMulti
	Min   float64
	Max   float64 // panjang maksimal (teks) atau nilai maksimal (angka)
	// Sensitive berarti nilai disimpan terenkripsi di kolom <Column>_enc
	// dan hanya boleh ditampilkan utuh untuk admin_tu dan kepala_sekolah.
	Sensitive bool
	// IndexKind diisi jika field butuh blind index (kolom <Column>_idx).
	IndexKind string
	Column    string // nama kolom database (tanpa akhiran _enc)
	// Section mengelompokkan field sesuai bagian F-PD.
	Section string
}

// Bagian data rinci, berurutan seperti F-PD.
const (
	SectionIdentity     = "identitas"
	SectionAddress      = "alamat"
	SectionFamily       = "keluarga"
	SectionAssistance   = "bantuan" // bantuan pemerintah: KIP, PIP, KKS (T109)
	SectionContact      = "kontak"
	SectionPeriodic     = "periodik"
	SectionRegistration = "registrasi"
)

// ProfileFields adalah semua field data rinci siswa, berurutan seperti F-PD.
var ProfileFields = []ProfileField{
	{Key: "nisn", Label: "NISN", Kind: kindNISN, Column: "nisn", Section: SectionIdentity},
	{Key: "nik", Label: "NIK/No. KITAS", Kind: kindIdentity, Sensitive: true, IndexKind: "nik", Column: "nik", Section: SectionIdentity},
	{Key: "kk_number", Label: "No KK", Kind: kindKK, Sensitive: true, IndexKind: "kk", Column: "kk", Section: SectionIdentity},
	{Key: "birth_place", Label: "Tempat lahir", Kind: kindText, Max: 50, Column: "birth_place", Section: SectionIdentity},
	{Key: "birth_cert_number", Label: "No registrasi akta lahir", Kind: kindText, Max: 50, Sensitive: true, Column: "birth_cert", Section: SectionIdentity},
	{Key: "religion", Label: "Agama", Kind: kindRef, Ref: &ref.Agama, Column: "religion", Section: SectionIdentity},
	{Key: "citizenship", Label: "Kewarganegaraan", Kind: kindRef, Ref: &ref.Kewarganegaraan, Column: "citizenship", Section: SectionIdentity},
	{Key: "country", Label: "Nama negara", Kind: kindText, Max: 50, Column: "country", Section: SectionIdentity},
	{Key: "special_needs", Label: "Kebutuhan khusus", Kind: kindRefMulti, Ref: &ref.KebutuhanKhusus, Column: "special_needs", Section: SectionIdentity},

	{Key: "street", Label: "Alamat jalan", Kind: kindText, Max: 100, Column: "street", Section: SectionAddress},
	{Key: "rt", Label: "RT", Kind: kindDigits, Min: 1, Max: 3, Column: "rt", Section: SectionAddress},
	{Key: "rw", Label: "RW", Kind: kindDigits, Min: 1, Max: 3, Column: "rw", Section: SectionAddress},
	{Key: "hamlet", Label: "Nama dusun", Kind: kindText, Max: 50, Column: "hamlet", Section: SectionAddress},
	{Key: "wilayah_kode", Label: "Kode wilayah desa", Kind: kindWilayah, Column: "wilayah_kode", Section: SectionAddress},
	{Key: "village", Label: "Kelurahan/desa", Kind: kindText, Max: 50, Column: "village", Section: SectionAddress},
	{Key: "district", Label: "Kecamatan", Kind: kindText, Max: 50, Column: "district", Section: SectionAddress},
	{Key: "postal_code", Label: "Kode pos", Kind: kindDigits, Min: 5, Max: 5, Column: "postal_code", Section: SectionAddress},
	{Key: "latitude", Label: "Lintang", Kind: kindDecimal, Min: -11, Max: 6, Column: "latitude", Section: SectionAddress},
	{Key: "longitude", Label: "Bujur", Kind: kindDecimal, Min: 95, Max: 141, Column: "longitude", Section: SectionAddress},
	{Key: "residence_type", Label: "Tempat tinggal", Kind: kindRef, Ref: &ref.TempatTinggal, Column: "residence_type", Section: SectionAddress},
	{Key: "transport_mode", Label: "Moda transportasi", Kind: kindRef, Ref: &ref.ModaTransportasi, Column: "transport_mode", Section: SectionAddress},

	{Key: "child_order", Label: "Anak ke-berapa", Kind: kindInt, Min: 1, Max: 30, Column: "child_order", Section: SectionFamily},
	{Key: "siblings", Label: "Jumlah saudara kandung", Kind: kindInt, Min: 0, Max: 30, Column: "siblings", Section: SectionFamily},
	{Key: "has_kip", Label: "Punya KIP", Kind: kindBool, Column: "has_kip", Section: SectionAssistance},
	{Key: "kip_number", Label: "Nomor KIP", Kind: kindText, Max: 30, Sensitive: true, Column: "kip", Section: SectionAssistance},
	{Key: "kip_name", Label: "Nama di KIP", Kind: kindText, Max: 100, Column: "kip_name", Section: SectionAssistance},
	{Key: "pip_eligible", Label: "Layak PIP", Kind: kindBool, Column: "pip_eligible", Section: SectionAssistance},
	{Key: "pip_reason", Label: "Alasan PIP", Kind: kindRef, Ref: &ref.AlasanPIP, Column: "pip_reason", Section: SectionAssistance},
	{Key: "kks_number", Label: "Nomor KKS", Kind: kindText, Max: 30, Sensitive: true, Column: "kks", Section: SectionAssistance},

	{Key: "home_phone", Label: "Telepon rumah", Kind: kindPhone, Column: "home_phone", Section: SectionContact},
	{Key: "mobile_phone", Label: "Nomor HP", Kind: kindPhone, Column: "mobile_phone", Section: SectionContact},
	{Key: "email", Label: "Email", Kind: kindEmail, Max: 100, Column: "email", Section: SectionContact},

	{Key: "height_cm", Label: "Tinggi badan (cm)", Kind: kindInt, Min: 30, Max: 250, Column: "height_cm", Section: SectionPeriodic},
	{Key: "weight_kg", Label: "Berat badan (kg)", Kind: kindInt, Min: 2, Max: 250, Column: "weight_kg", Section: SectionPeriodic},
	{Key: "head_cm", Label: "Lingkar kepala (cm)", Kind: kindInt, Min: 20, Max: 80, Column: "head_cm", Section: SectionPeriodic},
	{Key: "distance_km", Label: "Jarak ke sekolah (km)", Kind: kindDecimal, Min: 0, Max: 9999, Column: "distance_km", Section: SectionPeriodic},
	{Key: "travel_minutes", Label: "Waktu tempuh (menit)", Kind: kindInt, Min: 0, Max: 1440, Column: "travel_minutes", Section: SectionPeriodic},

	{Key: "registration_type", Label: "Jenis pendaftaran", Kind: kindRef, Ref: &ref.JenisPendaftaran, Column: "registration_type", Section: SectionRegistration},
	{Key: "entry_date", Label: "Tanggal masuk sekolah", Kind: kindDate, Column: "entry_date", Section: SectionRegistration},
	{Key: "previous_school", Label: "Sekolah asal", Kind: kindText, Max: 100, Column: "previous_school", Section: SectionRegistration},
	{Key: "exam_number", Label: "Nomor peserta ujian", Kind: kindText, Max: 30, Column: "exam_number", Section: SectionRegistration},
	{Key: "diploma_serial", Label: "No seri ijazah", Kind: kindText, Max: 50, Column: "diploma_serial", Section: SectionRegistration},
	{Key: "skhun_number", Label: "No SKHUN", Kind: kindText, Max: 50, Column: "skhun_number", Section: SectionRegistration},
}

var profileFieldByKey = func() map[string]ProfileField {
	m := make(map[string]ProfileField, len(ProfileFields))
	for _, f := range ProfileFields {
		m[f.Key] = f
	}
	return m
}()

// LookupProfileField mengembalikan definisi field berdasarkan key.
func LookupProfileField(key string) (ProfileField, bool) {
	f, ok := profileFieldByKey[key]
	return f, ok
}

// aad adalah additional data enkripsi untuk field sensitif; mengikat
// ciphertext ke kolomnya (T85b).
func (f ProfileField) aad() string { return "student_profiles." + f.Column }
