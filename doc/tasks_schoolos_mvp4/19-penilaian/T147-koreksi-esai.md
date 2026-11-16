# T147 — Koreksi esai

**Fase:** 19 Penilaian
**Dependensi:** T146
**Estimasi:** 3 hari

## Tujuan
Koreksi esai cepat, adil, dan bisa dibagi ke beberapa guru. Esai sering menjadi alasan guru kembali ke kertas.

## Langkah
1. **Antrian koreksi per soal** (bukan per siswa): semua jawaban untuk soal yang sama berurutan, sehingga guru menilai dengan standar yang konsisten.
2. **Layar koreksi:**
   - soal dan **rubrik** di samping;
   - jawaban siswa dalam huruf besar dan mudah dibaca;
   - skor diisi dengan tombol cepat (0, 1, 2, … maks.) atau ketik;
   - komentar opsional untuk siswa;
   - **Simpan & berikutnya**, dengan pintasan keyboard;
   - progres "18/32".
3. **Anonim** (bawaan aktif): nama disembunyikan saat koreksi.
4. **Bagi koreksi:** pengelola menugaskan soal atau rentang peserta ke guru lain (mis. tim mapel PAS). Penugasan memberi akses koreksi saja.
5. **Kemiripan jawaban:** jawaban esai yang sangat mirip antar-peserta di ujian yang sama ditandai (kemiripan teks n-gram), sebagai bahan pertimbangan guru, bukan vonis.
6. **Koreksi dari HP** dimungkinkan untuk esai pendek, dengan tata letak yang ditumpuk.
7. Setelah semua esai dikoreksi, nilai menjadi `final` dan guru diberi tahu bahwa nilai siap dikirim ke rapor.

## File yang disentuh
- `internal/exam/essay.go`, `similarity.go`, tes, `essay_handler.go`
- `web/templates/exam/essay_queue.html`, `essay_grade.html`

## Kriteria selesai
- [ ] Koreksi 30 esai pendek ≤ 20 menit (uji dengan guru pilot)
- [ ] Guru yang ditugaskan hanya melihat soal/peserta yang ditugaskan
- [ ] Kemiripan menandai pasangan jawaban salinan dalam data uji
- [ ] Nilai baru final setelah semua esai dikoreksi
