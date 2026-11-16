# T130 — Impor soal Moodle XML dan GIFT

**Fase:** 15 Bank Soal
**Dependensi:** T128
**Estimasi:** 2,5 hari

## Tujuan
Sekolah yang sebelumnya memakai Moodle memindahkan bank soalnya ke AKRA.

## Langkah
1. Unggah `.xml` (Moodle XML) atau `.txt`/`.gift` (GIFT).
2. Pemetaan jenis:
   - `multichoice` single → PG; multiple → PG kompleks;
   - `truefalse` → benar/salah;
   - `shortanswer`/`numerical` → isian;
   - `matching` → menjodohkan;
   - `essay` → esai.
   - `cloze`, `ddwtos`, `calculated`, dan jenis lain dilewati dengan alasan.
3. Gambar base64 (`@@PLUGINFILE@@`) diunggah ke storage.
4. Rumus TeX Moodle (`$$...$$`, `\(...\)`) dirender ulang dengan renderer AKRA; yang gagal ditandai.
5. Kategori Moodle menjadi tag; `generalfeedback` menjadi pembahasan.
6. Layar pratinjau dan perbaikan sama dengan impor Word (T129), memakai komponen yang sama.
7. Parser XML aman: tanpa entitas eksternal; ≤ 20 MB; ≤ 2.000 soal.
8. **Ekspor ke Moodle XML** untuk soal milik sekolah, supaya sekolah tidak terkunci di AKRA. Ini nilai jual untuk sekolah yang ragu.

## File yang disentuh
- `internal/exam/import_moodle.go`, `import_gift.go`, `export_moodle.go`, tes, `testdata/`
- `web/templates/exam/import_moodle.html`

## Kriteria selesai
- [ ] File ekspor Moodle 4.x berisi semua jenis yang didukung (dengan gambar dan rumus) terimpor benar
- [ ] Ekspor AKRA lalu impor ke Moodle menghasilkan soal yang sama (dicek manual di Moodle uji)
- [ ] XXE ditolak; gagal di tengah tidak menyimpan sebagian
