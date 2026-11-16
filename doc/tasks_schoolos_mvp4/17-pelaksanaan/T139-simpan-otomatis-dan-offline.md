# T139 — Simpan otomatis dan mode offline

**Fase:** 17 Pelaksanaan
**Dependensi:** T123, T138
**Estimasi:** 4 hari

## Tujuan
**Tidak ada jawaban yang hilang**, walau sinyal putus-nyambung, WiFi lab penuh, HP kehabisan baterai, atau browser tertutup.

## Protokol (sesuai ADR T123)
1. Saat Mulai, peserta menerima **token attempt** bertanda tangan (HMAC: `attempt_id`, `device_id`, kedaluwarsa). Endpoint simpan memakai token ini, tanpa membaca session `scs`, agar ringan pada ribuan request.
2. Setiap perubahan jawaban:
   1. langsung ditulis ke **IndexedDB** (antrian per attempt);
   2. dikirim `POST /u/a/{attempt}/simpan` berisi daftar perubahan `{question_id, answer, seq}` beserta kejadian integritas (T141);
   3. perubahan dalam 1 detik digabung jadi satu request.
3. Server melakukan satu upsert batch ke `exam_answers` (`attempt_id`, `question_id`, `answer jsonb`, `seq`, `saved_at`):
   - `seq` lebih kecil diabaikan, sehingga request terlambat tidak menimpa jawaban baru;
   - ditolak setelah `deadline_at` + tenggang 2 menit;
   - **commit sinkron** sebelum membalas.
   - Respons berisi `seq` terakhir yang tersimpan, jam server, `deadline_at` terbaru, dan pesan pengawas bila ada (T144).
4. Gagal kirim: coba lagi dengan jeda 2, 4, 8 … maks. 30 detik. Status di header berubah menjadi "Offline — jawaban aman di perangkat ini".
5. **Service worker** menyimpan cangkang halaman ujian, payload soal, CSS/font, dan gambar soal. Muat ulang saat offline tetap membuka ujian dengan jawaban dari IndexedDB.
6. **Kumpul:**
   - antrian wajib terkirim dulu;
   - bila offline: "Hubungkan internet untuk mengumpulkan. Jawaban tidak hilang, waktu tetap berjalan";
   - kumpul otomatis di server (T140) memakai jawaban yang sudah sampai.
7. **Pindah perangkat:** dengan izin pengawas, perangkat baru memuat jawaban dari server. Token perangkat lama dicabut (T141).
8. Tidak ada job River dan audit log per jawaban. Audit hanya untuk mulai, kumpul, reset, dan aksi pengawas.

## File yang disentuh
- `db/migrations/0000XX_exam_answers.up.sql` dan `.down.sql`
- `internal/exam/answer.go`, `attempt_token.go`, tes, `save_handler.go`
- `web/static/js/exam-take.js`, `web/static/js/exam-sw.js`

## Kriteria selesai
- [ ] Uji Playwright: offline → jawab 10 soal → muat ulang (tetap terbuka, jawaban ada) → online → semua tersimpan di server
- [ ] Request lama tidak menimpa jawaban baru (uji unit `seq`)
- [ ] Simpan setelah tenggat + tenggang ditolak; jawaban sebelum tenggat tetap dinilai
- [ ] Endpoint simpan tidak menyentuh tabel session (uji: hitung query per request = 1)
- [ ] Setelah kumpul, cache soal di perangkat terhapus
