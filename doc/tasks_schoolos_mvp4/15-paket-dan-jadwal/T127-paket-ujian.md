# T127 — Paket ujian

**Fase:** 15 Paket dan Jadwal
**Dependensi:** T123, T124
**Estimasi:** 2,5 hari

## Tujuan
Guru atau TU menyusun paket ujian dari bank soal, lengkap dengan aturan pengerjaannya.

## Langkah
1. Migrasi `exams`:
   - `id`, `school_id`, `name`;
   - `purpose` (`seleksi`, `ulangan`, `pts`, `pas`, `latihan`), `subject_id NULL`;
   - `duration_minutes`;
   - `shuffle_questions bool`, `shuffle_options bool`;
   - `show_result` (`tidak`, `nilai`, `nilai_dan_pembahasan`);
   - `max_attempts` (bawaan 1), `pass_score NULL`;
   - `status` (`draf`, `siap`, `arsip`), `locked_at NULL`, `created_by`.
2. Migrasi `exam_items`: isi paket. Dua cara, bisa digabung:
   - **Soal tetap:** `question_id` + urutan.
   - **Acak dari bank:** aturan `{bank_id, kind, difficulty, tags, count}`. Tiap peserta mendapat soal acak yang berbeda dengan jumlah dan proporsi yang sama.
3. Bagian (section), mis. "TPA" 30 soal dan "Bahasa Inggris" 20 soal, dengan urutan bagian tetap.
4. Ringkasan paket: jumlah soal, total poin, perkiraan waktu, jumlah soal esai (perlu dinilai manual).
5. **Kunci paket** (`locked_at`): terjadi saat jadwal pertama dimulai. Setelah terkunci, soal di dalamnya disalin sebagai snapshot (`exam_item_snapshots`). Perubahan bank soal tidak mengubah ujian yang sedang atau sudah berjalan.
6. Pratinjau paket seperti peserta (T130) dan cetak naskah PDF cadangan, untuk jaga-jaga listrik/internet mati.
7. Salin paket (seperti salin gelombang T117).

## File yang disentuh
- `db/migrations/0000XX_exams.up.sql` dan `.down.sql`
- `internal/exam/exam.go`, `exam_test.go`, `exam_handler.go`, `paper_pdf.go`
- `web/templates/exam/exams.html`, `exam_form.html`, `exam_detail.html`

## Kriteria selesai
- [ ] Aturan acak yang meminta soal lebih banyak dari isi bank ditolak dengan pesan jelas
- [ ] Dua peserta dengan aturan acak mendapat set soal berbeda, tetapi jumlah per jenis dan kesulitan sama (uji unit dengan seed)
- [ ] Menyunting soal di bank setelah paket terkunci tidak mengubah snapshot
- [ ] Naskah PDF cadangan tidak memuat kunci jawaban
