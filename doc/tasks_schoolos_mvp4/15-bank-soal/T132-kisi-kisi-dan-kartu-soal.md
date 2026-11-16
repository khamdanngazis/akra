# T132 — Kisi-kisi, kartu soal, dan telaah soal

**Fase:** 15 Bank Soal
**Dependensi:** T127, T133
**Estimasi:** 2,5 hari

## Tujuan
Dokumen administrasi yang wajib diserahkan guru ke wakasek kurikulum (kisi-kisi dan kartu soal) dibuat otomatis dari soal yang sudah ditandai. Guru tidak lagi menyusunnya manual di Word.

## Langkah
1. **Kisi-kisi per ujian:** tabel nomor soal, TP, materi/tag, indikator, level kognitif, bentuk soal, kesulitan, poin.
   - Ringkasan proporsi LOTS/MOTS/HOTS dan mudah/sedang/sulit, dengan peringatan bila tidak sesuai target yang diatur koordinator (mis. 30/50/20).
   - Kolom "indikator" diisi guru per soal (opsional, disimpan di soal).
2. **Kartu soal per butir:** identitas, TP, indikator, level, soal lengkap dengan rumus/gambar, kunci, pembahasan, dan statistik pemakaian bila ada.
3. Keduanya dalam format halaman cetak (HTML print, karena memuat rumus; lihat T123) dan **Excel** untuk kisi-kisi. Kop sekolah diambil dari pengaturan rapor.
4. **Telaah soal** (opsional per sekolah, diatur koordinator):
   - status soal `draf → ditelaah → siap`;
   - penelaah (guru mapel sama atau koordinator) memberi catatan per soal;
   - pembuat memperbaiki;
   - penelaah menyetujui.
   - Bila telaah aktif, ujian PTS/PAS hanya boleh memakai soal berstatus `siap`.
5. **Cek mutu otomatis** di daftar soal: kunci tunggal yang selalu di posisi sama, opsi "semua benar" berlebihan, soal tanpa TP, dan soal mirip/kembar di bank (kemiripan teks).

## File yang disentuh
- `internal/exam/blueprint.go`, `question_card.go`, `review.go`, `quality.go`, tes
- `web/templates/exam/blueprint_print.html`, `question_card_print.html`, `review.html`

## Kriteria selesai
- [ ] Kisi-kisi dan kartu soal cocok dengan isi ujian, termasuk rumus dan gambar saat dicetak
- [ ] Peringatan proporsi muncul bila menyimpang dari target
- [ ] Alur telaah: soal belum disetujui tidak bisa masuk ujian PTS/PAS bila telaah aktif
- [ ] Deteksi soal kembar menemukan duplikat hasil impor ganda
