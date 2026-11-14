# T106 — Tombol "Ambil lokasi saya" untuk lintang dan bujur

**Fase:** 13 Perbaikan
**Dependensi:** T86, T88, T90
**Estimasi:** 0,5–1 hari

## Latar belakang
Field lintang dan bujur (F-PD nomor 19–20, koordinat rumah siswa) membingungkan pengguna: orang tua dan TU tidak terbiasa mengisi koordinat secara manual, dan form tidak memberi petunjuk. Field ini opsional (tidak termasuk isian wajib Dapodik di AKRA), tetapi diminta Dapodik.

## Keputusan
Solusinya tombol **"Ambil lokasi saya"** yang mengisi lintang dan bujur dari GPS perangkat (2026-11-14).

## Langkah
1. Di formulir pendaftaran publik (T90, langkah Alamat) dan form data rinci siswa (T88, bagian Alamat), tampilkan lintang dan bujur sebagai satu kelompok "Titik lokasi rumah (opsional)".
2. Tambah tombol "Ambil lokasi saya" yang memakai `navigator.geolocation.getCurrentPosition` (Alpine.js), lalu mengisi kedua input dengan 6 angka di belakang koma.
3. Tampilkan status: sedang mengambil lokasi, berhasil (dengan akurasi dalam meter), atau gagal (izin ditolak, GPS mati, browser tidak mendukung) dengan pesan dalam bahasa Indonesia.
4. Petunjuk singkat: "Tekan tombol saat berada di rumah. Bisa dikosongkan."
5. Input manual tetap bisa dipakai dan validasi server tidak berubah (lintang −11..6, bujur 95..141). Koordinat di luar Indonesia dari GPS tetap ditolak server dengan pesan yang sudah ada.
6. Geolocation hanya jalan di HTTPS (dan localhost); di HTTP tombol disembunyikan atau diberi pesan.
7. Koordinat tidak dikirim ke layanan pihak ketiga (tanpa peta eksternal).

## File yang disentuh
- `web/templates/partials/profile_fields.html` (atau partial baru untuk kelompok lokasi)
- `internal/students/profile_handler.go` (penanda field lokasi di `FieldView`, jika perlu)

## Kriteria selesai
- [x] Tombol mengisi lintang dan bujur di formulir pendaftaran dan form data rinci
- [x] Izin lokasi ditolak menampilkan pesan yang jelas; form tetap bisa disimpan tanpa koordinat
- [x] Label menunjukkan field ini opsional
- [x] Uji handler: halaman memuat tombol; penyimpanan koordinat tetap tervalidasi seperti sebelumnya

## Catatan implementasi
- Lintang dan bujur ditampilkan sebagai satu kelompok "Titik lokasi rumah (opsional)" (`FieldView` Input `lokasi`, partial `lokasi_picker`) di formulir pendaftaran dan form data rinci staf, di bawah isian wilayah T110.
- Input lintang dan bujur **disembunyikan** (permintaan 2026-11-15): koordinat hanya diisi lewat tombol. Yang tampil: petunjuk, ringkasan "Lokasi tersimpan: lintang, bujur", tombol "Ambil ulang lokasi" dan "Hapus lokasi". Error validasi server tetap tampil di bawah kelompok ini. Di form staf yang hanya-baca, koordinat tampil sebagai teks.
- Tombol "Ambil lokasi saya" memakai `navigator.geolocation.getCurrentPosition` (akurasi tinggi, batas 15 detik) dan mengisi kedua input dengan 6 angka di belakang koma; status sedang mengambil, berhasil (akurasi dalam meter), atau gagal (izin ditolak, lokasi tidak tersedia, waktu habis) dalam bahasa Indonesia. Diuji di Chrome dengan izin diberikan dan ditolak.
- Di koneksi tidak aman (bukan HTTPS/localhost) atau browser tanpa geolocation, tombol disembunyikan dan diganti pesan; bagian ini boleh dikosongkan.
- Validasi server tidak berubah (lintang −11..6, bujur 95..141); koordinat tidak dikirim ke layanan lain dan tidak ada peta eksternal.

