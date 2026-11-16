# T136 — Hasil ujian dan analisis butir soal

**Fase:** 17 Penilaian
**Dependensi:** T135
**Estimasi:** 2 hari
**Keputusan:** calon siswa tidak melihat hasil; siswa diatur per paket (lihat indeks)

## Tujuan
Guru dan TU melihat hasil ujian dengan cepat, dan tahu soal mana yang terlalu mudah, terlalu sulit, atau kuncinya mungkin salah.

## Langkah
1. Halaman hasil `GET /ujian/{id}/hasil`:
   - per peserta: nilai, durasi pengerjaan, jumlah kejadian (T133), status (selesai, tidak hadir, menunggu esai);
   - filter per sesi/kelas;
   - unduh Excel lewat `reports/export`.
2. Detail per peserta: setiap soal dengan jawaban peserta, kunci, poin, dan log kejadian. Untuk guru/TU saja.
3. **Analisis butir** per soal, rumus klasik yang biasa dipakai guru:
   - **tingkat kesukaran** p = proporsi benar (mudah > 0,7; sulit < 0,3);
   - **daya beda** = p kelompok atas 27% − p kelompok bawah 27%;
   - **sebaran pilihan opsi**: opsi pengecoh yang tidak dipilih siapa pun ditandai.
4. Peringatan otomatis: daya beda negatif ("periksa kunci jawaban"), p = 0 atau p = 1.
5. Statistik ringkas: rata-rata, nilai tertinggi/terendah, jumlah di atas/bawah KKM atau nilai minimal.
6. Hasil untuk peserta, sesuai `show_result` paket (T127):
   - tidak ditampilkan;
   - nilai saja;
   - nilai dan pembahasan. Ini dibuka setelah sesi terakhir paket berakhir agar soal tidak bocor ke sesi berikutnya.
7. Calon siswa: hasil tidak ditampilkan di halaman ujian. Hasil seleksi tetap lewat halaman status pendaftaran (T95).

## File yang disentuh
- `internal/exam/result.go`, `item_analysis.go`, `item_analysis_test.go`, `result_handler.go`
- `web/templates/exam/results.html`, `result_detail.html`, `item_analysis.html`

## Kriteria selesai
- [ ] Rumus kesukaran dan daya beda diuji unit dengan data contoh yang hasilnya dihitung manual
- [ ] Pembahasan tidak terbuka sebelum sesi terakhir selesai
- [ ] Excel hasil cocok dengan tampilan halaman
- [ ] Wali murid (portal) tidak melihat detail jawaban (uji T59)
