# T103 — Cek kesiapan Dapodik

**Fase:** 11 Bantu Dapodik
**Dependensi:** T88
**Estimasi:** 1 hari

## Tujuan
TU tahu lebih awal data mana yang akan ditolak atau menjadi peringatan di validasi Dapodik, sebelum mulai memasukkan data.

## Langkah
1. Fungsi `Check(profile, guardians) []Issue{Field, Level (error/peringatan), Message}` di `internal/dapodik`. Aturan:
   - NIK kosong, bukan 16 digit, atau sama dengan No KK
   - NISN bukan 10 digit
   - Nama ibu kandung kosong, atau mengandung gelar (Almh., Hj., Dr., S.Pd)
   - Nama siswa atau orang tua mengandung gelar atau karakter aneh
   - Usia tidak wajar untuk jenjang sekolah (mis. SD < 5 atau > 13 tahun)
   - RT/RW bukan angka, kode pos bukan 5 digit
   - Lintang/bujur di luar wilayah Indonesia
   - Tahun lahir orang tua terlalu dekat dengan tahun lahir siswa (< 12 tahun)
2. Halaman `GET /dapodik/cek`: daftar siswa yang punya masalah, dikelompokkan per kelas, dengan tautan ke form data rinci (T88).
3. Ringkasan di beranda TU: jumlah siswa dengan error.

## File yang disentuh
- `internal/dapodik/check.go`, `check_test.go`
- `web/templates/dapodik/check.html`

## Kriteria selesai
- [x] Setiap aturan punya uji unit dengan contoh lolos dan gagal
- [x] Siswa tanpa masalah tidak muncul di daftar
- [x] Pesan memakai bahasa operator Dapodik, bukan istilah teknis

## Catatan implementasi
Kartu beranda TU memakai jumlah siswa dengan field wajib Dapodik belum lengkap (SQL kelengkapan T88), bukan menjalankan semua aturan per siswa.
