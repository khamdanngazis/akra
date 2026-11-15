# T87 — Data orang tua rinci

**Fase:** 09 Data Induk
**Dependensi:** T85, T85b, T86
**Estimasi:** 1,5 hari

## Tujuan
Data ayah kandung, ibu kandung, dan wali tersedia sesuai F-PD, termasuk orang tua yang tidak punya akun portal.

## Langkah
1. Migrasi: tambah kolom di `guardians`: `nik_enc bytea`, `nik_idx bytea` (T85b), `birth_year smallint`, `education`, `occupation`, `income`, `special_needs text[]`.
2. Relasi `student_guardians.relationship` tetap `ayah`/`ibu`/`wali`/`lainnya`. Tambah `is_biological boolean NOT NULL DEFAULT true` untuk membedakan ayah/ibu kandung.
3. Satu siswa paling banyak satu ayah kandung dan satu ibu kandung (unique index parsial).
4. Validasi: NIK 16 digit, tahun lahir 1930..tahun ini − 10, kode referensi lewat `ref.Valid`.
5. Pekerjaan "Meninggal Dunia" boleh tanpa NIK dan penghasilan.
6. Nama ibu kandung ditandai wajib untuk kelengkapan Dapodik (dipakai T88 dan T103), tetapi tidak wajib di database.
7. NIK orang tua mengikuti aturan enkripsi, mask, dan redaksi yang sama dengan T86. Validasi 16 digit di service sebelum enkripsi.

## File yang disentuh
- `db/migrations/0000XX_guardian_details.up.sql` dan `.down.sql`
- `internal/students/guardian_service.go`, `guardian_repo.go`, test

## Kriteria selesai
- [x] Siswa tidak bisa punya dua ibu kandung
- [x] Wali tanpa akun portal bisa disimpan dengan data rinci
- [x] Akun portal orang tua (T24) tetap berjalan tanpa perubahan
- [x] NIK orang tua tidak tampil utuh untuk peran `guru` dan `orang_tua`
- [x] Kolom `guardians.nik_enc` tidak berisi NIK dalam bentuk teks
