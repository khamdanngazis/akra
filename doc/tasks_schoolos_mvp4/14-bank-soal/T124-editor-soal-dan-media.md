# T124 — Editor soal dan media

**Fase:** 14 Bank Soal
**Dependensi:** T123
**Estimasi:** 3 hari

## Tujuan
Guru dan TU bisa menulis soal semua jenis dengan gambar, langsung di AKRA, termasuk dari HP atau tablet.

## Langkah
1. Form soal per jenis:
   - **PG:** 2–6 opsi, satu benar.
   - **PG kompleks:** lebih dari satu opsi benar. Penilaian dipilih: semua benar = penuh, atau proporsional.
   - **Benar/salah:** beberapa pernyataan, masing-masing dijawab B/S.
   - **Isian singkat:** daftar jawaban yang diterima. Pilihan: abaikan huruf besar/kecil dan spasi, atau rentang angka.
   - **Menjodohkan:** pasangan kiri–kanan; opsi kanan diacak.
   - **Esai:** rubrik/acuan penilaian untuk guru (seperti T119); dinilai manual di T135.
2. Gambar di soal dan opsi diunggah ke storage (R2) lewat paket storage yang ada. Gambar dikecilkan seperti T108 (maks. lebar 1600 px, WebP/JPEG). Audio/video di luar cakupan.
3. Teks soal memakai editor sederhana (tebal, miring, daftar, tabel kecil, gambar). HTML disanitasi di server dengan allowlist tag.
4. **Rumus matematika (wajib):**
   - ditulis dengan KaTeX di antara `\(...\)` (dalam baris) atau `\[...\]` (baris sendiri), di teks soal, opsi, dan pembahasan;
   - **tombol bantu rumus** di editor: pecahan, pangkat, akar, indeks, simbol (≤ ≥ ≠ π ∞ ∑ ∫), matriks sederhana, satuan. Guru memilih tombol, lalu rumus muncul dengan pratinjau langsung, sehingga tidak perlu hafal LaTeX;
   - KaTeX disajikan sebagai aset statis dari server AKRA (bukan CDN), supaya ujian lab tetap jalan saat akses internet terbatas. Font KaTeX di-subset agar ringan di HP;
   - rumus yang salah tulis ditampilkan sebagai teks merah di pratinjau dan ditolak saat simpan, agar tidak muncul rusak di halaman ujian;
   - isian singkat berupa angka tetap diketik biasa. Peserta tidak menulis rumus.
5. **Pratinjau sebagai peserta**: soal tampil persis seperti di halaman ujian (T130), di ukuran HP dan desktop.
6. Duplikat soal, pindah soal antar-bank, dan arsip soal.

## File yang disentuh
- `internal/exam/question.go`, `question_test.go`, `question_handler.go`, `sanitize.go`
- `web/templates/exam/question_form.html`, `partials/exam_question.html` (dipakai bersama T130)
- `web/static/js/exam-editor.js`

## Kriteria selesai
- [ ] Setiap jenis soal bisa dibuat, disunting, dan dipratinjau
- [ ] HTML berbahaya (`<script>`, `onerror=`, `javascript:`) dibuang oleh sanitizer (uji unit)
- [ ] Gambar dari sekolah lain tidak bisa dibuka lewat URL soal (uji lintas sekolah)
- [ ] Validasi per jenis: PG tanpa kunci, isian tanpa jawaban diterima, dsb. ditolak dengan pesan jelas
- [ ] Rumus di soal, opsi, dan pembahasan tampil benar di pratinjau, halaman ujian, dan naskah PDF cadangan (T127)
- [ ] Rumus yang tidak valid ditolak saat simpan
- [ ] Form bisa dipakai di layar 390 px
