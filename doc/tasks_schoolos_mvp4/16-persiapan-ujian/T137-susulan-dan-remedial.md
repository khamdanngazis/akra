# T137 — Ujian susulan, remedial, dan pengayaan

**Fase:** 16 Persiapan Ujian
**Dependensi:** T133, T148
**Estimasi:** 2 hari

## Tujuan
Siswa yang sakit atau izin bisa ujian susulan, dan siswa yang belum tuntas bisa remedial, tanpa membuat ujian dari nol.

## Langkah
1. **Susulan:** dari detail ujian, tombol **Buat sesi susulan**.
   - Peserta terisi otomatis dari yang **tidak hadir** atau belum mengumpulkan; bisa ditambah atau dikurangi.
   - Memakai **varian lain** bila ada, agar soal tidak bocor dari teman.
   - Nilai susulan masuk ke ujian yang sama (satu kolom nilai).
2. **Remedial:** dari hasil (T148), tombol **Buat remedial untuk yang belum tuntas**.
   - Peserta: siswa di bawah KKTP.
   - Soal: bisa difokuskan pada **TP yang belum tuntas** per siswa; aturan acak dibuat otomatis dari analisis TP.
   - Aturan nilai saat dikirim ke rapor (T150): ambil tertinggi, atau maksimal sama dengan KKTP, sesuai pengaturan rapor sekolah (T73).
3. **Pengayaan:** paket opsional untuk siswa yang sudah tuntas. Nilainya tidak menurunkan nilai rapor.
4. Riwayat percobaan per siswa terlihat di hasil: utama, susulan, remedial.

## File yang disentuh
- `internal/exam/followup.go`, `followup_test.go`, `exam_handler.go`
- `web/templates/exam/followup.html`

## Kriteria selesai
- [ ] Sesi susulan berisi tepat peserta tidak hadir, memakai varian berbeda bila tersedia
- [ ] Remedial terfokus TP membuat aturan acak yang benar dari analisis TP siswa
- [ ] Nilai remedial dikirim sesuai aturan (tidak menurunkan, batas KKTP)
