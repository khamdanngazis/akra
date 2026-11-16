# T129 — Impor soal dari Word

**Fase:** 15 Bank Soal
**Dependensi:** T123, T128
**Estimasi:** 5 hari

## Tujuan
Guru yang sudah punya soal di Word (cara paling umum di sekolah) memindahkannya ke AKRA dalam hitungan menit.

## Langkah
1. **Template Word resmi** (`.docx`), bisa diunduh dari halaman impor, dengan format mudah:
   ```
   1. Teks soal ... (boleh rumus Equation Word dan gambar)
   A. opsi
   B. opsi
   ...
   Kunci: B
   TP: 3.2        (opsional)
   Kesulitan: sedang   (opsional)
   Pembahasan: ... (opsional)
   ```
   Penanda jenis lain:
   - `Jenis: PG kompleks` dengan `Kunci: A, C`;
   - `Jenis: isian` dengan `Jawaban: 12 | dua belas`;
   - `Jenis: benar salah`;
   - `Jenis: esai` dengan `Rubrik: ...`.
2. **Pembaca toleran** untuk dokumen guru yang tidak persis mengikuti template:
   - nomor `1.` atau `1)`;
   - opsi `A.`, `a.`, `A)`, atau daftar bernomor Word;
   - kunci di tabel terpisah di akhir dokumen ("Kunci jawaban: 1. B 2. D ...").
3. **Rumus:**
   - Equation Word (OMML) dikonversi ke LaTeX sesuai subset di ADR T123;
   - objek MathType: pakai gambar pratinjau bila ada, dan tandai "rumus diimpor sebagai gambar — ketik ulang bila perlu".
4. **Gambar** di soal dan opsi diunggah dan dikecilkan otomatis.
5. **Layar pratinjau impor** (inti UX):
   - tampil berdampingan: potongan asli dan hasil di AKRA;
   - status per soal: ✓ siap, ⚠ perlu dicek (alasan: kunci tidak ditemukan, rumus sebagai gambar, opsi kurang), ✗ dilewati;
   - soal bisa diperbaiki langsung di pratinjau (editor T128) sebelum disimpan;
   - pilihan: impor semua yang siap, atau termasuk yang perlu dicek sebagai `draf`;
   - TP/kesulitan/tag bawaan untuk semua soal di file ini.
6. Disimpan dalam satu transaksi. Laporan akhir: "38 soal masuk (siap 35, draf 3), 2 dilewati", dengan tautan ke soal draf.
7. Batasan: file ≤ 20 MB, ≤ 500 soal per file. Dokumen diproses di job River dengan indikator kemajuan bila lebih dari 50 soal.

## File yang disentuh
- `internal/exam/import_docx.go`, `omml.go` (konversi rumus), `import_preview.go`, tes, `testdata/*.docx`
- `web/templates/exam/import_word.html`
- `web/static/files/template-soal-akra.docx`

## Kriteria selesai
- [ ] Template resmi terisi contoh semua jenis terimpor 100%
- [ ] Dokumen contoh guru sungguhan (dari T123): ≥ 90% soal siap tanpa perbaikan, sisanya ditandai "perlu dicek" (tidak ada soal rusak diam-diam)
- [ ] Rumus OMML pada subset ADR terkonversi benar (uji dengan pasangan input/output)
- [ ] Kunci di tabel akhir dokumen dikenali
- [ ] Dokumen berisi makro atau objek berbahaya ditolak atau diabaikan dengan aman
