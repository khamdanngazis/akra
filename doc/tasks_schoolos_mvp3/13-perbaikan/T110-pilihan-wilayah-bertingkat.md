# T110 — Alamat dengan pilihan wilayah bertingkat (provinsi sampai desa)

**Fase:** 13 Perbaikan
**Dependensi:** T86, T88, T90, T101
**Estimasi:** 2–3 hari

## Latar belakang
Kecamatan dan desa/kelurahan saat ini diketik bebas, sehingga ejaan tidak seragam (mis. "Sukajadi", "Ds. Sukajadi", "sukajadi") dan orang tua tidak tahu harus menulis apa. Data wilayah Indonesia resmi tersedia dan bisa dipakai untuk pilihan bertingkat.

## Keputusan
- Data wilayah disimpan di database AKRA dan dilayani AKRA sendiri, **bukan** memanggil API pihak ketiga saat formulir dipakai: alamat anak tidak dikirim ke server lain, formulir tidak bergantung pada layanan luar, dan kode mengikuti keputusan resmi.
- Sumber yang diusulkan: dataset [cahyadsn/wilayah](https://github.com/cahyadsn/wilayah) (kode wilayah sesuai Kepmendagri No 300.2.2-2430 Tahun 2025) dan [cahyadsn/wilayah_kodepos](https://github.com/cahyadsn/wilayah_kodepos) untuk kode pos.

## Langkah
1. **Periksa lisensi** dataset (file LICENSE di repo) sebelum dipakai. Jika tidak cocok, pakai sumber lain atau data Kemendagri langsung. Catat sumber, versi keputusan, dan lisensi di README.
2. Migrasi tabel `wilayah` (global, bukan per sekolah): `kode text PK` (format `11.01.01.2001`), `nama text`, `tingkat smallint` (1 provinsi, 2 kab/kota, 3 kecamatan, 4 desa/kelurahan), `induk text` (kode tingkat di atasnya), `kode_pos text NULL` (desa). Indeks `(induk)` dan pencarian nama (`lower(nama)`).
3. Perintah impor data (`cmd/` atau langkah migrasi data) yang idempoten, agar pembaruan Kepmendagri berikutnya cukup menjalankan ulang impor.
4. Endpoint baca untuk formulir: daftar anak satu kode (`?induk=`) dan pencarian nama per tingkat; dipakai formulir publik (dengan rate limit ringan) dan form staf. Tidak memerlukan data pribadi.
5. Kolom baru di data rinci (`student_profiles`) dan pendaftar: `wilayah_kode` (kode desa). Nama kecamatan dan desa tetap disimpan di kolom yang ada (`district`, `village`) agar lembar salin, F-PD, ekspor, dan impor Dapodik tidak berubah. Tambah nama kabupaten/kota dan provinsi bila dibutuhkan untuk tampilan.
6. Komponen input (Alpine.js): Provinsi → Kabupaten/Kota → Kecamatan → Desa/Kelurahan, masing-masing bisa dicari dengan mengetik; memilih desa mengisi kode pos (tetap bisa diubah).
7. Pilihan "Desa tidak ada di daftar" untuk isian manual (pemekaran wilayah lebih cepat dari pembaruan data); ditandai agar TU bisa memeriksa.
8. Data lama tanpa kode tetap tampil apa adanya; form menyarankan pencocokan otomatis berdasarkan nama bila ada kecocokan unik.
9. Impor ekspor Dapodik (T101): cocokkan nama kecamatan/desa ke kode bila unik di kabupaten yang sama; jika tidak, simpan sebagai isian manual.
10. Validasi server: kode desa harus ada di tabel `wilayah` dan nama yang disimpan diambil dari tabel (bukan dari isian pengguna).

## File yang disentuh
- `db/migrations/0000XX_wilayah.up.sql`, `cmd/import-wilayah/`
- `internal/wilayah/` (repo dan handler pencarian)
- `internal/students/profile*.go`, `internal/admission/applicant.go`, `internal/dapodik/import_pd.go`
- `web/templates/partials/` (komponen alamat), `web/templates/admission/public_step.html`, `web/templates/students/profile.html`

## Kriteria selesai
- [ ] Lisensi dan versi data tercatat; impor data bisa diulang tanpa duplikasi
- [ ] Pilihan bertingkat bekerja di formulir pendaftaran dan form data rinci, termasuk pencarian dengan mengetik
- [ ] Kode pos terisi otomatis dari desa
- [ ] Isian manual tetap mungkin dan ditandai
- [ ] Lembar salin, F-PD, dan ekspor Dapodik tetap menampilkan nama kecamatan/desa yang sama
- [ ] Formulir tidak memanggil layanan pihak ketiga
- [ ] Endpoint wilayah masuk daftar route (publik/terautentikasi) dan diuji
