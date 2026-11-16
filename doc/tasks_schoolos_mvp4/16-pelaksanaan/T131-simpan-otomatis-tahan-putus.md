# T131 — Simpan otomatis dan tahan putus koneksi

**Fase:** 16 Pelaksanaan
**Dependensi:** T130
**Estimasi:** 2,5 hari

## Tujuan
Jawaban peserta tidak hilang walau sinyal HP putus-nyambung, WiFi lab penuh, atau browser tertutup tidak sengaja.

## Langkah
1. Setiap kali jawaban berubah, klien langsung menyimpan ke `localStorage` (kunci per attempt), lalu mengirim `POST /u/ujian/{attempt}/jawaban`. Isinya hanya soal yang berubah, dengan nomor urut perubahan (`seq`).
2. Server melakukan upsert `exam_answers (attempt_id, question_id, answer jsonb, seq, saved_at)`.
   - `seq` yang lebih kecil dari yang tersimpan diabaikan, sehingga request yang terlambat tidak menimpa jawaban baru.
   - Penyimpanan ditolak setelah `deadline_at` + masa tenggang 2 menit.
3. Antrian di klien:
   - gagal kirim → coba lagi dengan jeda bertambah (2, 4, 8 … maks. 30 detik);
   - beberapa perubahan digabung jadi satu request;
   - indikator status: "Tersimpan", "Menyimpan…", atau "Offline — jawaban aman di perangkat ini".
4. Saat kumpulkan, klien wajib mengirim semua antrian dulu. Bila masih offline, tampil pesan "Hubungkan internet untuk mengumpulkan; jawaban tidak hilang".
5. Peserta pindah perangkat (HP mati, pindah komputer lab):
   - dengan izin pengawas (T133), jawaban dimuat dari server;
   - antrian lokal perangkat lama tidak bisa dikirim lagi, karena `device_id` beda ditolak.
6. Request simpan dibuat kecil dan murah: satu upsert, tanpa job River, tanpa audit log per jawaban. Audit cukup untuk mulai, kumpul, dan reset.

## File yang disentuh
- `db/migrations/0000XX_exam_answers.up.sql` dan `.down.sql`
- `internal/exam/answer.go`, `answer_test.go`, `take_handler.go`
- `web/static/js/exam-take.js`

## Kriteria selesai
- [ ] Uji browser: matikan jaringan (Playwright `setOffline`), jawab 5 soal, nyalakan lagi → semua tersimpan di server
- [ ] Request lama (`seq` kecil) tidak menimpa jawaban baru (uji unit)
- [ ] Jawaban setelah tenggat + tenggang ditolak
- [ ] Muat ulang saat offline tetap menampilkan jawaban dari `localStorage`
