# T123 — Model data bank soal

**Fase:** 14 Bank Soal
**Dependensi:** T65 (penugasan guru), T119
**Estimasi:** 2 hari

## Tujuan
Sekolah punya bank soal per mapel yang bisa dipakai ulang di banyak paket ujian, baik ulangan siswa maupun tes seleksi pendaftaran.

## Langkah
1. Migrasi `exam_banks`:
   - `id`, `school_id`, `name`;
   - `subject_id NULL` (NULL untuk tes seleksi umum, mis. "Tes Potensi");
   - `grade_level smallint NULL`, `created_by`, `created_at`, `archived_at NULL`.
2. Migrasi `exam_questions`:
   - `id`, `school_id`, `bank_id`;
   - `kind` (`pg`, `pg_kompleks`, `benar_salah`, `isian`, `menjodohkan`, `esai`);
   - `body` (teks soal, HTML terbatas hasil sanitasi);
   - `options jsonb`: daftar opsi `{id, text, image}` tanpa penanda benar;
   - `answer_key jsonb`: kunci, terpisah dari `options` agar tidak ikut terkirim ke peserta;
   - `points numeric` (bawaan 1), `difficulty` (`mudah`, `sedang`, `sulit`, NULL), `tags text[]`;
   - `explanation` (pembahasan), `version int`, `created_by`, `updated_at`, `archived_at NULL`.
3. Soal yang sudah dipakai di ujian yang berjalan atau selesai **tidak diubah di tempat**. Saat disunting, `version` naik dan paket yang sudah terkunci tetap memakai salinan soal (T127).
4. Akses (keputusan: bank soal dipakai bersama per mapel):
   - **guru pengampu mapel** (T65, semua kelas dan tingkat): melihat semua soal di bank mapel itu, memakainya di paket, menambah soal, dan menduplikat soal guru lain untuk disunting sebagai soal miliknya;
   - **mengubah dan mengarsipkan soal hanya pembuat soal dan admin_tu.** Tombol ubah tidak tampil untuk guru lain, dan route ubah menolak (403);
   - admin_tu: semua bank, termasuk bank tes seleksi;
   - kepala_sekolah: baca.
5. Halaman `GET /ujian/bank`: daftar bank dengan filter mapel/tingkat dan jumlah soal per jenis.
6. Halaman `GET /ujian/bank/{id}`: daftar soal dengan pencarian, filter jenis/tag/kesulitan, dan arsip soal.

## File yang disentuh
- `db/migrations/0000XX_exam_banks.up.sql` dan `.down.sql`
- `internal/exam/bank.go`, `bank_test.go`, `bank_handler.go`
- `web/templates/exam/banks.html`, `bank_detail.html`
- `internal/platform/nav/menu.go` (menu "Ujian Online")

## Kriteria selesai
- [ ] Guru tidak bisa membuka bank soal mapel yang tidak ia ampu (403/404)
- [ ] Guru pengampu mapel yang sama bisa memakai dan menduplikat soal guru lain, tetapi tidak bisa mengubah atau mengarsipkannya
- [ ] `answer_key` tidak pernah ikut ter-render di halaman yang dibuka peserta (uji unit pada fungsi serialisasi soal untuk peserta)
- [ ] Soal yang dipakai ujian terkunci tidak berubah saat disunting (versi baru)
- [ ] Route baru masuk daftar uji T58 dan T59
