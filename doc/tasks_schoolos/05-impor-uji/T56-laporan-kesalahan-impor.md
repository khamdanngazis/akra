# T56 — Laporan kesalahan impor

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T55
**Estimasi:** 3 jam

## Tujuan
Admin bisa mengunduh daftar baris yang gagal, lengkap dengan nomor baris dan pesan kesalahan, untuk memperbaiki file dan mengimpor ulang.

## Langkah
1. Buat `internal/importer/error_report.go`:
   - Ambil semua `import_rows` dengan status `error` untuk batch tertentu.
   - Hasilkan Excel dengan kolom: nomor baris asli, NIS, nama, kelas, pesan kesalahan, data asli.
2. Pesan kesalahan harus dalam bahasa Indonesia yang mudah dipahami. Contoh: "Kelas `1X` tidak ditemukan di tahun ajaran aktif."
3. Tampilkan ringkasan kesalahan di halaman batch: jumlah per jenis kesalahan (contoh: 2 NIS kosong, 1 kelas tidak ditemukan).
4. Handler `GET /impor/batch/{id}/kesalahan.xlsx`.
5. Akses: `admin_tu` saja.

## File yang disentuh
- `internal/importer/error_report.go`, `error_report_test.go`
- `internal/importer/handler.go`
- `web/templates/importer/batch.html` (ringkasan dan tautan)

## Kriteria selesai
- [ ] Setiap baris error ada di file dengan nomor baris asli
- [ ] Pesan kesalahan tidak berisi nama kolom teknis (misalnya `nis_text is null`)
- [ ] Ringkasan jenis kesalahan sama dengan jumlah baris error per jenis
- [ ] File bisa dipakai ulang sebagai input setelah diperbaiki (kolom sama dengan T52)
- [ ] Guru mendapat `403`

## Catatan
- Kolom data asli penting agar admin tidak perlu membuka file sumber lagi.
