# T126 — Impor dan ekspor soal Excel

**Fase:** 14 Bank Soal
**Dependensi:** T124
**Estimasi:** 1,5 hari

## Tujuan
Guru yang tidak memakai Moodle bisa menyiapkan soal di Excel, yang sudah biasa dipakai untuk kisi-kisi dan soal di sekolah.

## Langkah
1. Template Excel yang bisa diunduh dari halaman bank soal, dengan kolom:
   - `jenis`, `soal`, `opsi_a`–`opsi_f`, `kunci` (mis. `B` atau `A,C`), `poin`, `kesulitan`, `tag`, `pembahasan`;
   - satu lembar petunjuk dan contoh per jenis.
2. Jenis yang didukung lewat Excel: PG, PG kompleks, benar/salah, isian singkat, esai. Menjodohkan dan gambar disunting di AKRA setelah impor.
3. Validasi per baris dengan laporan baris yang salah, memakai pola laporan error impor yang sudah ada (`error_report.go`). Alur pratinjau lalu impor dalam satu transaksi.
4. Ekspor bank soal ke Excel dengan format yang sama, termasuk kunci jawaban. Hanya untuk pengelola bank; tercatat di audit log.

## File yang disentuh
- `internal/exam/import_excel.go`, `export_excel.go`, tes
- `web/templates/exam/import.html`

## Kriteria selesai
- [ ] Template yang diunduh, diisi contoh, lalu diimpor menghasilkan soal yang benar
- [ ] Ekspor lalu impor ulang menghasilkan soal yang sama (round-trip)
- [ ] Baris salah dilaporkan dengan nomor baris dan alasan
- [ ] Ekspor tercatat di audit log
