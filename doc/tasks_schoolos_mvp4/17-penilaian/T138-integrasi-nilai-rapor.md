# T138 — Integrasi dengan komponen nilai rapor

**Fase:** 17 Penilaian
**Dependensi:** T135, T69, T70
**Estimasi:** 1,5 hari

## Tujuan
Nilai ulangan, PTS, dan PAS online langsung masuk ke komponen nilai rapor, tanpa diketik ulang guru.

## Langkah
1. Paket `ulangan`/`pts`/`pas` bisa dihubungkan ke **komponen nilai** (T69) mapel yang sama di tahun ajaran aktif, mis. komponen "PTS" mapel Matematika.
   - Migrasi: `exams.grade_component_id NULL`.
2. Setelah semua nilai final, guru menekan **"Kirim ke nilai rapor"** (bukan otomatis). Guru masih bisa memeriksa dulu dan memberi remedial.
   - Tampil pratinjau: siswa, nilai ujian, nilai saat ini di komponen.
   - Pilihan untuk nilai yang sudah ada: timpa, atau ambil yang tertinggi (remedial).
3. Nilai ditulis ke tabel `grades` (`enrollment_id`, `component_id`) dalam satu transaksi, dengan validasi yang sama seperti input nilai T70.
   - Siswa yang tidak ikut ujian tidak diubah: tetap NULL atau nilai lama.
4. **Remedial:** paket ulang untuk siswa di bawah KKM. Saat dikirim, aturan "ambil tertinggi" atau "maksimal KKM" sesuai konfigurasi rapor (T73) dan aturan nilai akhir (T71).
5. Riwayat pengiriman tersimpan (siapa, kapan, berapa siswa) dan tercatat di audit.

## File yang disentuh
- `db/migrations/0000XX_exam_grade_component.up.sql` dan `.down.sql`
- `internal/exam/sync_grades.go`, `sync_grades_test.go`, `sync_handler.go`
- `internal/grades` (pakai ulang validasi simpan nilai)
- `web/templates/exam/sync_grades.html`

## Kriteria selesai
- [ ] Nilai terkirim tampil di input nilai (T70) dan rekap nilai
- [ ] Guru tidak bisa mengirim ke komponen mapel/kelas yang tidak ia ampu
- [ ] Pilihan "ambil tertinggi" tidak menurunkan nilai yang sudah ada
- [ ] Siswa tidak hadir tidak berubah nilainya
