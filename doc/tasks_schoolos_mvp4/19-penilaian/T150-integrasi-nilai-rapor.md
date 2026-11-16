# T150 — Integrasi dengan nilai rapor dan deskripsi capaian

**Fase:** 19 Penilaian
**Dependensi:** T146, T148, T69–T71, T73
**Estimasi:** 2,5 hari

## Tujuan
Nilai ulangan, PTS, dan PAS online masuk ke komponen nilai rapor dengan sekali konfirmasi guru. Ketuntasan TP membantu menulis deskripsi capaian.

## Langkah
1. Ujian `ulangan`/`pts`/`pas` dihubungkan ke **komponen nilai** (T69) mapel yang sama di tahun ajaran aktif, dipilih di wizard.
   - Migrasi: `exams.grade_component_id NULL`.
   - Untuk ulangan harian yang banyak, komponen bisa menerima **rata-rata beberapa ujian** (mis. komponen "Ulangan Harian" = rata-rata UH1–UH4).
2. **Kirim ke rapor** (tidak otomatis):
   - pratinjau per siswa: nilai ujian, nilai komponen saat ini, nilai baru;
   - aturan bila sudah ada nilai: timpa, atau ambil tertinggi (remedial dengan batas KKTP sesuai T73);
   - siswa tidak hadir tidak diubah;
   - ditulis ke `grades` dalam satu transaksi dengan validasi T70; tercatat di audit dan riwayat pengiriman.
3. Bila nilai berubah setelah dikirim (nilai ulang T146 atau koreksi esai), guru mendapat tanda "nilai berubah, kirim ulang?" di detail ujian.
4. **Deskripsi capaian** (bila T74b sudah ada): saran "capaian menonjol" dan "perlu ditingkatkan" per siswa dari analisis TP (T148). Contoh: "Menguasai TP 3.1 (operasi pecahan); perlu ditingkatkan TP 3.4 (perbandingan)". Guru menyunting sebelum disimpan; tidak pernah terisi otomatis tanpa konfirmasi.

## File yang disentuh
- `db/migrations/0000XX_exam_grade_component.up.sql` dan `.down.sql`
- `internal/exam/sync_grades.go`, tes, `sync_handler.go`
- `internal/grades` (pakai ulang validasi), (opsional) modul deskripsi T74b
- `web/templates/exam/sync_grades.html`

## Kriteria selesai
- [ ] Nilai terkirim tampil di input nilai (T70), rekap (T72), dan rapor PDF (T74)
- [ ] "Ambil tertinggi" tidak pernah menurunkan nilai; batas KKTP untuk remedial dipatuhi
- [ ] Guru tidak bisa mengirim ke komponen mapel/kelas yang tidak ia ampu
- [ ] Perubahan nilai setelah dikirim memunculkan tanda kirim ulang
