# T130 — Halaman ujian peserta

**Fase:** 16 Pelaksanaan
**Dependensi:** T127, T129
**Estimasi:** 3 hari

## Tujuan
Peserta mengerjakan ujian di HP atau komputer dengan tampilan jelas, ringan, dan mirip ujian berbasis komputer yang sudah dikenal (ANBK).

## Langkah
1. Alur peserta:
   1. Login (T129).
   2. **Konfirmasi data:** nama, mapel, durasi, jumlah soal; untuk sesi dengan bunyi tanda, ada tes suara (T133).
   3. Masukkan token sesi (bila diwajibkan).
   4. Tombol **Mulai**.
   5. Mengerjakan.
   6. **Selesai:** ringkasan "terjawab X dari Y, ragu-ragu Z".
   7. Konfirmasi kumpulkan.
2. Saat **Mulai**, server membuat `exam_attempts`:
   - `id`, `participant_id`, `started_at`, `deadline_at`, `submitted_at NULL`;
   - urutan soal dan opsi hasil acak, disimpan agar sama saat halaman dimuat ulang;
   - `seed`, `device_id`.
3. Soal dikirim **sekali** saat halaman dibuka, dalam satu payload JSON tanpa kunci jawaban. Navigasi antar-soal di sisi klien (Alpine.js), tanpa request ke server.
4. Tampilan:
   - satu soal per layar;
   - nomor soal di panel/grid: hijau = terjawab, kuning = ragu-ragu, abu = belum;
   - tombol Sebelumnya/Berikutnya besar (min 44 px);
   - ukuran huruf bisa diperbesar;
   - hitung mundur selalu terlihat.
5. Komponen tampilan soal sama dengan pratinjau (T124, `partials/exam_question.html`).
6. Ringan untuk HP murah: tanpa framework berat, gambar lazy-load, total JS halaman ujian < 50 KB gzip.

## File yang disentuh
- `db/migrations/0000XX_exam_attempts.up.sql` dan `.down.sql`
- `internal/exam/attempt.go`, `attempt_test.go`, `take_handler.go`
- `web/templates/exam/take.html`, `take_confirm.html`, `take_done.html`
- `web/static/js/exam-take.js`

## Kriteria selesai
- [ ] Payload soal untuk peserta tidak memuat `answer_key` atau pembahasan (uji unit dan uji HTTP)
- [ ] Muat ulang halaman mempertahankan urutan soal, opsi, dan jawaban
- [ ] Bisa dikerjakan penuh di layar 360 px dan di Chrome Android lama (uji visual)
- [ ] Peserta yang sudah mengumpulkan tidak bisa membuka soal lagi
