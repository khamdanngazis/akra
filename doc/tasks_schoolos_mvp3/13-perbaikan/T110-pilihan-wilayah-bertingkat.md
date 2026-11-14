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
- [x] Lisensi dan versi data tercatat; impor data bisa diulang tanpa duplikasi
- [x] Pilihan bertingkat bekerja di formulir pendaftaran dan form data rinci, termasuk pencarian dengan mengetik
- [x] Kode pos terisi otomatis dari desa
- [x] Isian manual tetap mungkin dan ditandai
- [x] Lembar salin, F-PD, dan ekspor Dapodik tetap menampilkan nama kecamatan/desa yang sama
- [x] Formulir tidak memanggil layanan pihak ketiga
- [x] Endpoint wilayah masuk daftar route (publik/terautentikasi) dan diuji

## Catatan implementasi
- **Lisensi dan versi:** kedua dataset berlisensi MIT; sumber, tanggal pengambilan, versi Kepmendagri (README repo menyebut 300.2.2-2430/2025, header berkas masih 300.2.2-2138/2025), dan teks lisensi dicatat di `internal/wilayah/data/LICENSE.md`. 91.599 wilayah: 38 provinsi, 514 kabupaten/kota, 7.285 kecamatan, 83.762 desa/kelurahan, semua desa punya kode pos.
- **Data disertakan** sebagai `internal/wilayah/data/wilayah.tsv.gz` (±690 KB, di-embed). `cmd/import-wilayah -convert` membuatnya dari berkas SQL sumber; tanpa flag, mengimpor ulang ke `DATABASE_URL`.
- **Impor otomatis:** server mengimpor di latar belakang saat start jika sidik data berbeda dengan `wilayah_meta.versi` (advisory lock, upsert, aman diulang). Kode yang hilang di data baru ditandai `aktif = false`, tidak dihapus. Selama data belum ada, isian alamat otomatis jatuh ke mode manual. Tidak perlu langkah deploy tambahan.
- **Tabel** `wilayah(kode, nama, tingkat, induk, kode_pos, aktif)` tanpa FK ke induk; kolom baru `student_profiles.wilayah_kode` (pendaftar menyimpannya di jsonb profil).
- **Endpoint** `GET /wilayah?induk=` (publik, JSON, cache 1 hari) mengembalikan wilayah di bawah satu kode. Pencarian dengan mengetik dilakukan di browser pada daftar desa (daftar per kecamatan kecil): desa/kelurahan memakai **satu isian** (combobox, permintaan 2026-11-15) — ketik sebagian nama, daftar yang cocok muncul di bawahnya (yang diawali kata itu lebih dulu, dengan kode pos), pilih dengan klik atau panah + Enter; Escape/klik di luar mengembalikan desa terpilih sebelumnya; tidak ada endpoint pencarian global dan belum ada rate limit khusus karena datanya publik dan bisa di-cache.
- **Komponen** `wilayah_picker` (`web/static/js/wilayah.js`) menggantikan isian bebas desa, kecamatan, dan kode pos di formulir pendaftaran dan form data rinci staf: Provinsi → Kab/Kota → Kecamatan → Desa, kode pos terisi otomatis (bisa diubah), kotak "Desa tidak ada di daftar, isi manual". Tanpa JavaScript yang tampil isian manual.
- **Validasi server:** kode harus format desa dan ada di tabel; nama desa/kecamatan yang disimpan diambil dari tabel (`wilayah.Apply`), kode pos diisi bila kosong. Lembar salin, F-PD, dan ekspor tetap memakai kolom `village`/`district`.
- **Data lama dan impor:** alamat tanpa kode dicocokkan dengan nama desa + kecamatan bila hasilnya unik, sebagai saran di form (perlu disimpan ulang). Impor ekspor Dapodik (T101) mengisi `wilayah_kode` dengan cara yang sama; jika tidak unik, tetap manual. Detail pendaftar menandai "Desa/kecamatan diisi manual, periksa ejaannya".

