# T125 — Impor soal format Moodle XML dan GIFT

**Fase:** 14 Bank Soal
**Dependensi:** T124
**Estimasi:** 2,5 hari

## Tujuan
Bank soal yang sudah dibuat sekolah di Moodle bisa dipindahkan ke AKRA tanpa diketik ulang.

## Langkah
1. Unggah file `.xml` (Moodle XML) atau `.txt`/`.gift` (GIFT) ke sebuah bank soal.
2. Pemetaan jenis soal:

   | Moodle | AKRA |
   |---|---|
   | `multichoice` (single) | PG |
   | `multichoice` (multiple) | PG kompleks |
   | `truefalse` | Benar/salah (satu pernyataan) |
   | `shortanswer`, `numerical` | Isian singkat |
   | `matching` | Menjodohkan |
   | `essay` | Esai |

   Jenis lain (`cloze`, `ddwtos`, `calculated`, dan sebagainya) dilewati dan dilaporkan per soal.
3. Gambar yang tertanam di Moodle XML (`<file encoding="base64">`) diunggah ke storage; `@@PLUGINFILE@@` diganti dengan URL AKRA.
4. Kategori Moodle (`$course$/...`) menjadi tag. `generalfeedback` menjadi pembahasan.
5. Alur pratinjau seperti impor dengan mapping (T82):
   - tabel soal yang akan dibuat, beserta yang dilewati dan alasannya;
   - tombol **Impor**;
   - semua soal masuk dalam satu transaksi.
6. Batas ukuran file 20 MB. Parser XML aman: tanpa entitas eksternal; batas jumlah soal 2.000 per file.
7. Ekspor balik ke Moodle XML: opsional (kandidat), tidak wajib di MVP 4.

## File yang disentuh
- `internal/exam/import_moodle.go`, `import_gift.go`, `import_test.go`, `testdata/*.xml`, `testdata/*.gift`
- `internal/exam/import_handler.go`
- `web/templates/exam/import.html`

## Kriteria selesai
- [ ] File contoh ekspor Moodle 4.x (semua jenis yang didukung, dengan gambar) terimpor benar (uji dengan `testdata`)
- [ ] Jenis yang tidak didukung dilaporkan, bukan membuat soal rusak
- [ ] File XML dengan entitas eksternal (XXE) ditolak
- [ ] Gagal di tengah: tidak ada soal yang tersimpan sebagian
