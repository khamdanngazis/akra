# T112 — Pilih titik lokasi rumah di peta (Google Maps)

**Fase:** 13 Perbaikan
**Dependensi:** T106, T110
**Estimasi:** 1–1,5 hari

## Latar belakang
Di T106 titik lokasi rumah hanya diisi dengan tombol "Ambil lokasi saya" (GPS perangkat), dan input lintang/bujur disembunyikan. Masalahnya, orang tua bisa saja mengisi formulir saat tidak berada di rumah (di kantor, di sekolah, atau dibantu TU), sehingga koordinat yang terambil salah dan tidak bisa diperbaiki.

## Keputusan
- Orang tua (dan TU) bisa **memilih titik rumah di peta**. Penyedia peta: **Google Maps** (2026-11-15).
- Ini mengubah keputusan T106 "tanpa peta eksternal". Batasnya:
  - peta hanya dimuat saat pengguna menekan tombol, bukan saat halaman dibuka;
  - Google hanya menerima area peta yang sedang dilihat dan titik yang digeser;
  - nama, NIK, dan isian lain tidak pernah dikirim ke Google.

## Langkah
1. **Akun dan kunci:**
   - Buat project di Google Cloud dan aktifkan **Maps JavaScript API**.
   - Buat API key khusus browser, dibatasi:
     - HTTP referrer: domain staging/production AKRA dan `localhost`;
     - API: hanya Maps JavaScript API (Geocoding hanya jika langkah 5 dipakai).
   - Pasang batas kuota harian dan peringatan anggaran di Google Cloud.
   - Simpan sebagai env `GOOGLE_MAPS_API_KEY` di Railway. Kunci ini tetap terlihat di browser, karena itu wajib dibatasi.
   - Tanpa kunci, fitur peta tidak tampil dan T106 tetap berfungsi seperti sekarang.
2. **Tampilan di kelompok "Titik lokasi rumah (opsional)":**
   - Tombol **"Pilih di peta"** di samping "Ambil lokasi saya".
   - Peta terbuka sebagai panel/modal layar penuh di HP, dengan satu penanda yang bisa digeser atau dipindah dengan mengetuk peta.
   - Tombol **"Pakai titik ini"** mengisi lintang dan bujur (6 angka di belakang koma); **"Batal"** tidak mengubah apa pun.
   - Ringkasan "Lokasi tersimpan" tetap seperti T106, ditambah peta kecil statis atau tautan "Lihat di peta" untuk memeriksa ulang.
3. **Posisi awal peta**, berurutan:
   1. titik yang sudah tersimpan;
   2. hasil "Ambil lokasi saya" jika baru diambil;
   3. wilayah yang dipilih di isian alamat (T110), lihat langkah 5;
   4. tengah Indonesia dengan zoom provinsi.
4. **Bantuan memilih:**
   - Pilihan tampilan satelit untuk mengenali atap rumah.
   - Zoom minimal saat menyimpan (mis. ≥ 16), agar titik tidak asal.
   - Petunjuk singkat: "Geser penanda ke atap rumah Anda."
5. **Pusat peta dari desa/kecamatan** (opsional, putuskan saat implementasi):
   - Data wilayah T110 tidak memuat koordinat.
   - Pilihan A: pakai Geocoding API dengan nama desa + kecamatan + kabupaten (alamat jalan **tidak** dikirim). Ada biaya per permintaan dan Google menerima nama desa.
   - Pilihan B: tambahkan koordinat kabupaten/kota dari dataset `cahyadsn/wilayah` (`wilayah_level_1_2.sql`, MIT) ke data wilayah, tanpa panggilan ke Google.
   - Rekomendasi: B dulu.
6. **Validasi server tidak berubah:** lintang −11..6, bujur 95..141. Titik di luar Indonesia tetap ditolak dengan pesan yang ada.
7. **Pemuatan skrip Google:**
   - Lazy load saat tombol ditekan, lewat `https://maps.googleapis.com/maps/api/js?key=…&loading=async`, dengan callback dan bahasa `id`.
   - Gagal muat (jaringan, kunci salah, kuota habis): tampilkan pesan, dan GPS tetap bisa dipakai.
8. **Berlaku di dua tempat:**
   - formulir pendaftaran publik (langkah Alamat);
   - form data rinci staf, termasuk tampilan hanya-baca dengan tautan "Lihat di peta".
9. **Privasi dan persetujuan:**
   - Tambah kalimat di petunjuk: "Peta disediakan Google."
   - Tinjau teks persetujuan pengolahan data di halaman awal pendaftaran: perlu menyebut bahwa peta disediakan pihak ketiga jika fitur ini dipakai.
   - Catat di `doc/` (kebijakan data) bahwa koordinat rumah disimpan di AKRA. Google hanya melihat tampilan peta saat orang tua memilih.
10. **Biaya:**
    - Maps JavaScript API dihitung per pemuatan peta.
    - Cek harga dan kuota gratis terbaru di halaman harga Google Maps Platform saat implementasi, lalu catat perkiraan biaya bulanan untuk satu sekolah, mis. ratusan pendaftar × 1–2 kali buka peta.

## File yang disentuh
- `internal/platform/config/config.go` (`GOOGLE_MAPS_API_KEY`, opsional), `.env.example`
- `web/templates/partials/profile_fields.html` (`lokasi_picker`), `web/static/js/` (skrip peta, lazy load)
- `web/tailwind/app.css` (panel peta)
- Halaman awal pendaftaran (teks persetujuan), dokumentasi deploy (`doc/restore.md` atau README: cara membuat dan membatasi API key)
- Jika langkah 5 pilihan B: `internal/wilayah` (kolom koordinat kabupaten/kota) dan `cmd/import-wilayah`

## Kriteria selesai
- [ ] Orang tua bisa memilih titik rumah di peta walau tidak sedang di rumah; titik tersimpan dan tampil lagi saat halaman dibuka
- [ ] Peta tidak dimuat (tidak ada permintaan ke Google) sampai tombol "Pilih di peta" ditekan
- [ ] Tanpa `GOOGLE_MAPS_API_KEY` atau saat skrip gagal dimuat, tombol peta tidak tampil/berpesan jelas dan "Ambil lokasi saya" tetap jalan
- [ ] Nyaman di HP 360 px: peta layar penuh, penanda mudah digeser, tombol "Pakai titik ini" terlihat
- [ ] API key dibatasi referrer dan API; langkah pembuatannya terdokumentasi
- [ ] Teks persetujuan/petunjuk menyebut peta dari Google
- [ ] Uji handler: tombol peta hanya muncul jika kunci diset; validasi koordinat tetap
