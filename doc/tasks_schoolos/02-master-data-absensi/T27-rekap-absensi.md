# T27 — Rekap absensi

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T26
**Estimasi:** 1 hari

## Tujuan
Admin, kepala sekolah, dan wali kelas bisa melihat rekap absensi per siswa dan per kelas untuk rentang tanggal tertentu.

## Langkah
1. Buat `internal/attendance/report.go`:
   - `SummaryByStudent(ctx, schoolID, classID, from, to)`: jumlah hadir, sakit, izin, alpa per siswa.
   - `SummaryByClass(ctx, schoolID, classID, from, to)`: total per status untuk kelas, plus persentase kehadiran.
   - Query memakai `GROUP BY` pada `attendance_records` dengan join ke `attendance_sessions` dan `enrollments`.
   - Setiap query memfilter `school_id` dari sesi.
2. Pastikan query hanya menghitung sesi yang tanggalnya berada di dalam enrollment siswa (`session_date BETWEEN enrollments.start_date AND COALESCE(enrollments.end_date, 'infinity')`).
3. Handler:
   - `GET /absensi/rekap?kelas={id}&dari={tanggal}&sampai={tanggal}`
   - Akses: `admin_tu`, `kepala_sekolah`, dan `guru` untuk kelas yang diampu.
4. Tampilkan tabel per siswa dan ringkasan kelas.
5. Tombol unduh Excel disiapkan sebagai placeholder dan dikerjakan di T47.
6. Batas rentang maksimal 1 tahun ajaran, untuk menjaga performa.

## File yang disentuh
- `internal/attendance/report.go`, `report_test.go`
- `internal/attendance/handler.go` (route rekap)
- `web/templates/attendance/rekap.html`

## Kriteria selesai
- [x] Jumlah hadir + sakit + izin + alpa per siswa sama dengan jumlah sesi yang tercatat untuk siswa tersebut
- [x] Uji dengan data contoh (misalnya 3 siswa, 10 sesi, satu siswa pindah kelas di tengah) menghasilkan angka yang sudah dihitung manual
- [x] Rentang lebih dari satu tahun ajaran ditolak dengan pesan jelas
- [x] Rekap untuk 30 siswa dan 60 sesi selesai di bawah 1 detik
- [x] Guru yang membuka rekap kelas lain mendapat `403`

## Catatan
- Persentase kehadiran dihitung dari (hadir + sakit + izin + alpa) sebagai pembagi, bukan hanya sesi yang "hadir". Definisi ini perlu dikonfirmasi ke sekolah pada wawancara.
