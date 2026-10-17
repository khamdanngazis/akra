# T52 — Template impor siswa

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T22
**Estimasi:** 3 jam

## Tujuan
Sekolah bisa mengunduh satu template Excel dengan kolom baku, lalu mengisinya dari data yang sudah mereka punya.

## Langkah
1. Tetapkan kolom baku (urutan tetap):
   | Kolom | Wajib | Format |
   |---|---|---|
   | NIS | ya | teks |
   | Nama lengkap | ya | teks |
   | Tanggal lahir | tidak | `YYYY-MM-DD` |
   | Jenis kelamin | tidak | `L` atau `P` |
   | Kelas | ya | nama kelas, misalnya `1A` |
   | Nama wali | tidak | teks |
   | Telepon wali | tidak | teks |
   | Email wali | tidak | email |
2. Buat `internal/importer/template.go` dengan `excelize`:
   - Baris pertama header.
   - Baris kedua contoh pengisian (ditandai dengan warna abu-abu dan teks "contoh, hapus baris ini").
   - Kolom NIS diset format teks agar `007` tidak berubah.
   - Validasi data di Excel (dropdown untuk jenis kelamin).
3. Handler `GET /impor/siswa/template.xlsx`. Akses: `admin_tu`.
4. Halaman `GET /impor/siswa` berisi tombol unduh template dan penjelasan singkat kolom.

## File yang disentuh
- `internal/importer/template.go`, `template_test.go`
- `internal/importer/handler.go`
- `web/templates/importer/index.html`

## Kriteria selesai
- [ ] Template terbuka di Excel dengan delapan kolom sesuai tabel
- [ ] NIS `007` tetap `007` saat template diisi dan dibuka lagi
- [ ] Dropdown jenis kelamin berfungsi
- [ ] Guru mendapat `403`

## Catatan
- Kolom sama dengan yang dipakai di T23 dan T20. Jika ada perubahan, ubah di satu tempat (konstanta).
