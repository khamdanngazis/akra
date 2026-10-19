# T79 — Ekspor laporan kustom

**Fase:** 08 Laporan dan Impor
**Dependensi:** T78, T49
**Estimasi:** 1 hari

## Tujuan
Hasil laporan kustom bisa diunduh sebagai Excel dan PDF. Ekspor besar memakai job yang sudah ada di T49.

## Langkah
1. Gunakan `Compile` dari T76 dan `excelize` dari T47. Jangan menulis jalur ekspor kedua.
2. Ekspor PDF memakai tata letak tabel sederhana dari T47. Judul laporan dari nama konfigurasi.
3. Batas sinkron 5.000 baris. Di atasnya, buat job seperti T49 dengan `params` berisi konfigurasi.
4. Route: `GET /laporan/konfigurasi/{id}.xlsx` dan `.pdf`.
5. Hanya pemilik atau konfigurasi bersama yang bisa diunduh.

## File yang disentuh
- `internal/reports/builder/export.go`
- `internal/reports/handler.go` (route)
- `internal/platform/jobs/report_export_job.go` (menerima konfigurasi)

## Kriteria selesai
- [ ] Angka di Excel sama dengan pratinjau T78 untuk konfigurasi yang sama
- [ ] Konfigurasi dengan lebih dari 5.000 baris dibuat sebagai job
- [ ] Pengguna lain mendapat `404` untuk konfigurasi pribadi
- [ ] Hasil file menampilkan nama sekolah dan tanggal cetak

## Catatan
- Konfigurasi dengan sumber yang sudah tidak diizinkan untuk pemilik (misalnya peran berubah) harus ditolak saat diunduh.
