package pii

// registered berisi semua kolom terenkripsi di database. Setiap migrasi
// yang menambah kolom ciphertext wajib menambahkan entri di sini agar ikut
// dirotasi oleh cmd/rotate-pii-key.
var registered []Column

// Registered mengembalikan salinan daftar kolom terenkripsi.
func Registered() []Column {
	return append([]Column(nil), registered...)
}
