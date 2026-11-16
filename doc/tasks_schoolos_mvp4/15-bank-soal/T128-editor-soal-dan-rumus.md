# T128 — Editor soal dan rumus

**Fase:** 15 Bank Soal
**Dependensi:** T123, T127
**Estimasi:** 6 hari

## Tujuan
Guru menulis semua jenis soal, termasuk soal bergambar dan berumus, dengan cepat dan tanpa harus menguasai LaTeX.

## Jenis soal
| Jenis | Isi | Penilaian |
|---|---|---|
| Pilihan ganda | 2–6 opsi, satu benar | Penuh / 0 |
| PG kompleks | Beberapa opsi benar | Pilih: semua benar = penuh, atau proporsional (tanpa negatif) |
| Benar/salah | Beberapa pernyataan, masing-masing B/S | Proporsional |
| Isian singkat | Daftar jawaban diterima; teks (abaikan besar/kecil, spasi, tanda baca) atau angka (rentang toleransi, koma/titik desimal) | Penuh / 0 |
| Menjodohkan | Pasangan kiri–kanan, pengecoh tambahan di kanan | Proporsional |
| Esai | Rubrik/acuan dan skor maksimal | Manual (T147) |

## Langkah
1. **Editor teks:**
   - tebal, miring, garis bawah, sub/superskrip, daftar, tabel kecil, gambar, rumus;
   - tempel dari Word mempertahankan format dasar dan gambar;
   - sumber disanitasi di server dengan allowlist.
2. **Rumus:**
   - tombol **Σ Rumus** membuka editor rumus (pilihan dari T123) dengan palet pecahan, pangkat, akar, indeks, matriks, dan simbol;
   - pratinjau langsung;
   - guru mahir bisa mengetik LaTeX langsung;
   - rumus dirender di server saat simpan; rumus tidak valid ditandai merah dan menolak simpan.
3. **Gambar:**
   - unggah, tempel dari clipboard, atau seret;
   - dikecilkan otomatis (T108): lebar maks. 1600 px, WebP;
   - teks alternatif wajib untuk aksesibilitas, berisi saran dari nama file.
4. **Opsi jawaban:** tambah, hapus, dan urutkan dengan seret; tandai benar dengan satu ketuk; opsi bisa berupa gambar atau rumus. Peringatan bila ada opsi kembar atau opsi kosong.
5. **Metadata di panel samping:** TP, level kognitif, kesulitan, poin, tag, pembahasan.
6. **Pratinjau sebagai peserta**, beralih tampilan HP/desktop, memakai komponen yang sama dengan halaman ujian (T138).
7. **Draf tersimpan otomatis** tiap 5 detik di browser dan server (status `draf`). Menutup tab tidak menghilangkan soal.
8. **Alur cepat:**
   - **Simpan & tambah berikutnya**, dengan jenis, TP, dan kesulitan dari soal sebelumnya;
   - pintasan keyboard: Ctrl+Enter simpan, Alt+1..6 tandai opsi benar.
9. Validasi per jenis dengan pesan yang menyebut bagian yang salah, misalnya "Tandai satu opsi yang benar" atau "Isian butuh minimal satu jawaban diterima".
10. Bisa dipakai di tablet. Di HP hanya untuk koreksi kecil, dan ini dijelaskan di UI.

## Stimulus bersama
1. **Editor stimulus:** teks panjang dengan paragraf bernomor baris (opsional, untuk soal "pada baris ke-3"), tabel, gambar, grafik, rumus.
2. Dari editor stimulus, guru langsung menambah soal-soal turunannya. Urutan soal di dalam stimulus bisa diseret.
3. Pratinjau sebagai peserta menampilkan stimulus dan soal dalam tata letak halaman ujian (T138).

## Tulisan Arab
1. Tombol arah tulisan **← Arab / Latin →** per paragraf, plus deteksi otomatis (`dir="auto"`) saat mengetik atau menempel teks Arab.
2. Font Arab dari ADR T123 dimuat di editor, halaman ujian, dan cetak. Ukuran teks Arab sedikit lebih besar dari Latin agar harakat terbaca.
3. Opsi jawaban bisa berupa teks Arab. Label A–E tetap di posisi yang konsisten; tata letak opsi Arab dibalik kanan-ke-kiri.
4. Isian singkat dalam bahasa Arab menormalkan harakat dan variasi huruf (ا/أ/إ, ة/ه, ى/ي) saat menilai, bila diaktifkan per soal.

## File yang disentuh
- `internal/exam/question.go`, `question_validate.go`, `sanitize.go`, `mathrender/` (render server), tes
- `internal/exam/question_handler.go`
- `web/templates/exam/question_form.html`, `partials/exam_question.html`
- `web/static/js/exam-editor.js` (dimuat hanya di halaman editor)

## Kriteria selesai
- [ ] Keenam jenis bisa dibuat, disunting, dipratinjau, dan divalidasi
- [ ] Sanitizer menolak `<script>`, atribut `on*`, `javascript:`, dan `style` berbahaya (uji unit)
- [ ] Rumus contoh (pecahan bersarang, matriks 3×3, integral, kimia sederhana) tampil sama di pratinjau, halaman ujian, dan cetak
- [ ] Draf bertahan setelah tab ditutup lalu dibuka lagi
- [ ] Guru baru bisa membuat 5 soal PG berumus dalam ≤ 10 menit (uji pengguna dengan 3 guru)
- [ ] Stimulus dengan 5 soal turunan bisa dibuat, diurutkan, dan dipratinjau
- [ ] Soal PAI campuran Latin–Arab berharakat tampil benar di editor, pratinjau, dan cetak
- [ ] Isian Arab dengan opsi normalisasi menerima jawaban berharakat dan tanpa harakat
