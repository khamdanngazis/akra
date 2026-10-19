# T72 — Rekap nilai per mapel dan per kelas

**Fase:** 07 Nilai
**Dependensi:** T71
**Estimasi:** 1–2 hari

## Tujuan
Guru, admin, dan kepala sekolah bisa melihat rekap nilai per mapel dan per kelas, dengan nilai akhir dan status lengkap atau belum.

## Langkah
1. Buat `internal/grades/report.go`:
   - `SubjectSummary(ctx, schoolID, classID, subjectID)`: satu baris per siswa dengan nilai per komponen, nilai akhir, dan huruf.
   - `ClassSummary(ctx, schoolID, classID)`: ringkasan semua mapel untuk kelas, dengan jumlah belum lengkap.
2. Gunakan `FinalScore` dari T71. Jangan menghitung ulang di SQL.
3. Akses:
   - Guru: mapel yang diajarnya.
   - Wali kelas: ringkasan kelas (baca).
   - Admin dan kepala sekolah: semua kelas (baca).
4. Route `GET /nilai/rekap?kelas=&mapel=`.
5. Tampilkan jumlah nilai kosong per mapel sebagai peringatan.

## File yang disentuh
- `internal/grades/report.go`, `report_test.go`
- `internal/grades/handler.go`
- `web/templates/grades/summary.html`

## Kriteria selesai
- [ ] Nilai akhir di rekap sama dengan hasil `FinalScore` untuk data contoh
- [ ] Siswa dengan komponen kosong ditandai "belum lengkap"
- [ ] Guru mapel lain mendapat `403`
- [ ] Rekap 30 siswa dan 6 mapel selesai di bawah 1 detik

## Catatan
- Angka rekap ini adalah sumber untuk ekspor T79 dan nilai di portal T75.
