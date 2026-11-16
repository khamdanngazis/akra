# T135 — Penilaian otomatis dan esai

**Fase:** 17 Penilaian
**Dependensi:** T132
**Estimasi:** 2 hari

## Tujuan
Soal objektif langsung dinilai saat ujian dikumpulkan. Soal esai dinilai guru dengan cepat, dan nilai akhir peserta selalu jelas statusnya.

## Langkah
1. Saat attempt dikumpulkan (manual, otomatis, atau oleh job), server menilai semua soal objektif dari snapshot paket (T127) dan menyimpan `exam_answers.points_awarded`. Aturan per jenis:
   - PG: penuh atau 0.
   - PG kompleks: sesuai aturan soal (semua benar, atau proporsional tanpa nilai negatif).
   - Benar/salah: proporsional per pernyataan.
   - Isian: cocok dengan salah satu jawaban diterima, setelah normalisasi.
   - Menjodohkan: proporsional per pasangan.
2. Nilai attempt = Σ poin / total poin × 100, dibulatkan 2 desimal.
   - Selama masih ada esai yang belum dinilai, status nilainya **"menunggu esai"** dan belum dikirim ke modul lain.
   - Perhitungan memakai integer (poin × 100) seperti `track_score` T118.
3. Halaman penilaian esai `GET /ujian/{id}/esai`:
   - per soal, semua jawaban peserta berurutan;
   - nama peserta bisa disembunyikan agar adil;
   - rubrik di samping;
   - isi poin, lalu simpan dan lanjut. Navigasi keyboard.
4. **Nilai ulang:** bila kunci soal ternyata salah, admin/guru pengelola bisa membetulkan kunci di snapshot lalu menilai ulang semua attempt paket itu. Tercatat di audit. Nilai yang sudah dikirim ke modul lain ikut diperbarui (T137, T138).
5. Penyesuaian manual per peserta (mis. soal dibatalkan untuk semua): soal dibatalkan, poinnya dikeluarkan dari total.

## File yang disentuh
- `internal/exam/grading.go`, `grading_test.go`, `essay_handler.go`
- `web/templates/exam/essay.html`

## Kriteria selesai
- [ ] Tabel uji penilaian per jenis soal, termasuk jawaban kosong dan jawaban berlebih
- [ ] Nilai tidak dikirim ke modul lain selama esai belum dinilai
- [ ] Nilai ulang setelah kunci diperbaiki mengubah nilai semua peserta dan tercatat di audit
- [ ] Guru hanya bisa menilai esai paket yang ia kelola
