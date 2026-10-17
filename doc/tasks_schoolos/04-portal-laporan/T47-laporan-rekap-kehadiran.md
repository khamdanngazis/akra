# T47 — Laporan: rekap kehadiran

**Fase:** 04 Portal dan Laporan
**Dependensi:** T27
**Estimasi:** 1–2 hari

## Tujuan
Rekap kehadiran bisa difilter dan diunduh sebagai Excel dan PDF. Angka di file sama dengan angka di layar.

## Langkah
1. Buat `internal/reports/attendance.go`:
   - `AttendanceReport(ctx, schoolID, filter)` yang memakai fungsi rekap dari T27.
   - Filter: kelas (satu atau semua), rentang tanggal (maksimal 1 tahun ajaran).
2. Buat `internal/reports/export/excel.go` dengan `excelize`:
   - Sheet "Rekap" berisi kolom: NIS, nama, kelas, hadir, sakit, izin, alpa, persentase.
   - Baris header di-bold, lebar kolom otomatis secukupnya.
   - Baris ringkasan total di bawah.
3. Buat `internal/reports/export/pdf.go`:
   - Judul, periode, nama sekolah, tabel sederhana, tanggal cetak.
4. Handler:
   - `GET /laporan/kehadiran?kelas=&dari=&sampai=`: tampilkan tabel di layar.
   - `GET /laporan/kehadiran.xlsx?...` dan `GET /laporan/kehadiran.pdf?...`: unduhan.
5. Akses: `admin_tu`, `kepala_sekolah`. Guru hanya lewat rekap kelasnya di T27.
6. Ekspor dibuat di handler. Batas 5.000 baris untuk ekspor sinkron, sisanya ke T49.

## File yang disentuh
- `internal/reports/attendance.go`, `attendance_test.go`
- `internal/reports/export/excel.go`, `pdf.go`
- `internal/reports/handler.go`

## Kriteria selesai
- [ ] Angka di file Excel sama dengan tabel di layar untuk filter yang sama
- [ ] File Excel terbuka di Excel dan Google Sheets tanpa peringatan perbaikan
- [ ] PDF terbuka dan menampilkan judul, periode, dan tabel
- [ ] Rentang lebih dari satu tahun ajaran ditolak
- [ ] Guru mendapat `403` pada route laporan

## Catatan
- Persentase menggunakan definisi di T27 (pembagi semua sesi). Jika sekolah memakai definisi lain, ubah di satu tempat saja.
