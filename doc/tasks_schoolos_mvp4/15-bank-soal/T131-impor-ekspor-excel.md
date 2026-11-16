# T131 — Impor dan ekspor soal Excel

**Fase:** 15 Bank Soal
**Dependensi:** T128
**Estimasi:** 1,5 hari

## Tujuan
Soal yang disusun bersama di spreadsheet (mis. oleh MGMP) bisa masuk sekaligus, dan bank soal bisa dicadangkan atau diserahkan dalam format yang dikenal.

## Langkah
1. Template Excel:
   - kolom `jenis`, `soal`, `opsi_a`–`opsi_f`, `kunci`, `poin`, `kesulitan`, `level_kognitif`, `tp`, `tag`, `pembahasan`;
   - lembar petunjuk dan contoh per jenis;
   - validasi data (dropdown) di sel `jenis`, `kesulitan`, dan `level_kognitif`.
2. Rumus boleh ditulis sebagai LaTeX di antara `\( \)`. Gambar tidak didukung lewat Excel (dijelaskan di petunjuk).
3. Pratinjau dan perbaikan memakai komponen T129, dengan laporan nomor baris.
4. Ekspor bank ke Excel dengan format yang sama, termasuk kunci jawaban. Hanya untuk pengelola bank; tercatat di audit.

## Stimulus
- Lembar kedua `stimulus` (kolom `kode`, `judul`, `teks`) dan kolom `stimulus` di lembar soal yang merujuk `kode`. Soal dengan kode stimulus sama dikelompokkan sesuai urutan baris.

## Kriteria selesai
- [ ] Round-trip ekspor lalu impor menghasilkan soal identik
- [ ] Baris salah dilaporkan dengan nomor baris dan alasan
- [ ] Ekspor tercatat di audit log
- [ ] Stimulus dari lembar `stimulus` terhubung ke soal yang merujuk kodenya, termasuk saat ekspor/impor ulang
