# T127 — Bank soal

**Fase:** 15 Bank Soal
**Dependensi:** T125
**Estimasi:** 3 hari

## Tujuan
Soal sekolah terkumpul rapi per mapel, bisa dicari, dipakai ulang bertahun-tahun, dan bisa dianalisis per tujuan pembelajaran.

## Model data
1. `exam_banks`:
   - `id`, `school_id`, `name`;
   - `subject_id NULL` (NULL untuk bank tes seleksi umum, mis. "Tes Potensi Akademik");
   - `grade_level smallint NULL`, `created_by`, `archived_at NULL`.
2. `learning_objectives` (TP): `id`, `school_id`, `subject_id`, `grade_level`, `code` (mis. "TP 3.2"), `description`, `sort_order`.
   - Diisi guru di halaman bank atau diimpor dari Excel.
   - Dipakai ulang untuk deskripsi capaian rapor (T150, T74b).
3. `exam_questions`:
   - `id`, `school_id`, `bank_id`, `kind`, `body_src`, `body_html`, `options jsonb`;
   - `answer_key jsonb`, terpisah dari `options` dan tidak pernah ikut serialisasi untuk peserta;
   - `points`, `difficulty`, `cognitive_level` (C1–C6, dikelompokkan LOTS/MOTS/HOTS), `objective_id NULL`, `tags text[]`;
   - `explanation_src`, `explanation_html`;
   - `status` (`draf`, `siap`);
   - `version`, `created_by`, `updated_by`, `archived_at`.
   - Untuk teks soal dan pembahasan, `*_src` berisi sumber yang ditulis guru, dan `*_html` hasil render server dengan rumus (T123).
4. `exam_question_stats`: riwayat statistik per pemakaian (tingkat kesukaran, daya beda, jumlah peserta, ujian), diisi T148. Tampil di daftar soal agar guru tahu soal mana yang bagus.
5. **Versi:** soal yang sudah dipakai di ujian terkunci tidak diubah di tempat. Menyunting menaikkan `version`, dan ujian lama memakai snapshot (T133).

## UI
1. **Daftar bank** `/ujian/bank`:
   - kartu per mapel berisi jumlah soal siap/draf, sebaran jenis, sebaran kesulitan, dan TP yang belum punya soal;
   - filter tingkat;
   - tombol utama **Tambah soal** dan **Impor** (Word / Moodle / Excel).
2. **Detail bank:**
   - daftar soal ringkas: potongan teks dengan rumus dan gambar mini, jenis, kesulitan, TP, statistik terakhir, dan "dipakai di N ujian";
   - pencarian teks; filter jenis/TP/kesulitan/status/pembuat;
   - **pilih banyak** untuk ubah TP/tag/kesulitan sekaligus, arsip, atau pindah bank.
3. Soal milik guru lain hanya bisa dipakai dan diduplikat. Tombol ubah tidak tampil (keputusan bank bersama).
4. Kosong: panduan tiga langkah (buat bank, tambah atau impor soal, buat ujian) dan tombol **Pakai bank contoh** (T154).

## File yang disentuh
- `db/migrations/0000XX_exam_banks.up.sql` dan `.down.sql`
- `internal/exam/bank.go`, `question_store.go`, `objective.go`, tes, `bank_handler.go`
- `web/templates/exam/banks.html`, `bank_detail.html`, `objectives.html`

## Kriteria selesai
- [ ] Fungsi serialisasi soal untuk peserta tidak pernah memuat `answer_key` atau `explanation_*` (uji unit untuk setiap jenis)
- [ ] Guru pengampu mapel bisa memakai dan menduplikat soal guru lain, tetapi tidak bisa mengubah atau mengarsipkannya
- [ ] Menyunting soal yang dipakai ujian terkunci membuat versi baru, dan snapshot ujian tidak berubah
- [ ] Pencarian dan filter di bank 5.000 soal < 300 ms
- [ ] Route baru masuk registry uji T58/T59
