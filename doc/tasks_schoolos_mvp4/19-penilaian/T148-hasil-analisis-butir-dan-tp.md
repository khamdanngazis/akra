# T148 — Hasil, analisis butir, dan analisis TP

**Fase:** 19 Penilaian
**Dependensi:** T146, T147
**Estimasi:** 3,5 hari

## Tujuan
Guru langsung mendapat hasil dan **dokumen analisis yang biasa diminta kurikulum** (analisis butir, ketuntasan, remedial/pengayaan), serta tahu TP mana yang belum dikuasai kelasnya.

## Langkah
1. **Hasil per ujian:**
   - ringkasan: rata-rata, tertinggi, terendah, sebaran nilai (histogram), jumlah tuntas/belum tuntas KKTP, kehadiran;
   - tabel peserta: nilai, status, durasi, ringkasan integritas (T141), susulan/remedial;
   - filter kelas, sesi, varian.
2. **Detail per peserta:**
   - setiap soal dengan jawaban, kunci, poin, dan komentar;
   - log kejadian;
   - tombol nilai manual untuk koreksi khusus (tercatat di audit).
3. **Analisis butir** (rumus klasik yang dikenal guru):
   - tingkat kesukaran p;
   - daya beda (27% atas − 27% bawah);
   - efektivitas pengecoh (opsi dipilih < 5% ditandai);
   - peringatan otomatis: daya beda negatif ("periksa kunci"), p = 0 atau p = 1, dengan tautan ke nilai ulang.
   - Hasilnya disimpan ke `exam_question_stats` untuk riwayat di bank soal (T127).
4. **Analisis TP:**
   - ketuntasan per TP untuk kelas (persentase siswa tuntas) dan per siswa (peta TP siswa);
   - menjadi dasar remedial terfokus (T137) dan deskripsi capaian (T150).
5. **Daftar remedial dan pengayaan:** siswa belum tuntas beserta TP yang belum tuntas, dan siswa tuntas. Ada tombol langsung **Buat remedial** (T137).
6. **Unduhan:**
   - Excel: hasil, analisis butir, analisis TP, daftar remedial/pengayaan;
   - halaman cetak "Analisis Hasil Ulangan" dengan kop, siap diserahkan ke wakasek kurikulum.
7. **Hasil untuk peserta** sesuai aturan ujian; dibuka setelah sesi terakhir (T151).

## File yang disentuh
- `internal/exam/result.go`, `item_analysis.go`, `objective_analysis.go`, tes, `result_handler.go`
- `web/templates/exam/results.html`, `result_detail.html`, `item_analysis.html`, `objective_analysis.html`, `analysis_print.html`

## Kriteria selesai
- [ ] Rumus p, daya beda, dan pengecoh diuji dengan data contoh yang dihitung manual
- [ ] Analisis TP benar untuk siswa dengan soal acak berbeda (varian)
- [ ] Excel cocok dengan tampilan halaman
- [ ] Hasil 600 peserta × 40 soal tampil < 2 detik
