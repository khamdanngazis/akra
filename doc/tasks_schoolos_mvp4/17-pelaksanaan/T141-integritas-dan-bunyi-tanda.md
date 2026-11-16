# T141 — Integritas ujian dan bunyi tanda keluar halaman

**Fase:** 17 Pelaksanaan
**Dependensi:** T138, T139
**Estimasi:** 3 hari

## Tujuan
Mengurangi kecurangan di HP dan lab secara wajar, dan memberi pengawas tanda yang langsung terdengar, tanpa membuat ujian gagal karena aturan terlalu ketat.

## Prinsip
Browser tidak bisa mencegah kecurangan secara mutlak. Yang dilakukan:
- **mempersulit:** acak, varian, token, satu perangkat;
- **memberi tanda:** bunyi dan layar merah;
- **mencatat:** log kejadian;
- **memberi pengawas alat bertindak.**

## Langkah
1. **Satu perangkat aktif per attempt:**
   - `device_id` dibuat saat Mulai;
   - login dari perangkat lain ditolak dengan pesan "Minta pengawas mengizinkan pindah perangkat";
   - pengawas mengizinkan (T143), lalu token perangkat lama dicabut.
2. **Kejadian yang dicatat** (`exam_events`):
   - keluar halaman/aplikasi (`visibilitychange`, `pagehide`, `blur`), beserta lamanya;
   - keluar layar penuh, muat ulang, salin/tempel;
   - percobaan login perangkat lain, offline > 1 menit.

   Dikirim bersama simpan jawaban.
3. **Bunyi tanda keluar halaman.** Aktif bila sesi `alert_sound` = ya: bawaan untuk lokasi **di ruang** (lab atau HP di kelas), dan mati untuk **jarak jauh**.
   - Audio "dibuka" saat peserta menekan **Mulai** (browser mewajibkan sentuhan pengguna).
   - **Komputer:** nada peringatan berulang segera saat pindah tab atau jendela, dan tetap terdengar dari tab di latar belakang.
   - **HP:** browser menghentikan halaman di latar belakang, jadi bunyi diputar **segera saat peserta kembali**, minimal 5 detik. Dilengkapi getar (Android) dan **layar merah**: "Anda keluar dari halaman ujian (ke-3 kali, 45 detik)".
   - Bunyi berhenti setelah peserta menekan **Lanjutkan**. Kejadian tetap tercatat dan langsung muncul di layar pengawas.
   - Nada dibuat khas dan berbeda dari nada notifikasi HP, dengan volume maksimum yang diizinkan browser.
   - **Tes suara** wajib di halaman konfirmasi untuk sesi dengan bunyi: peserta menekan **Tes**, mendengar, lalu mengonfirmasi.
   - **Keterbatasan** (tertulis di panduan pengawas):
     - volume dan mode senyap HP tidak bisa diatur dari browser;
     - iOS dalam mode senyap bisa tidak berbunyi.

     Indikator di layar pengawas tetap menjadi acuan utama.
4. **Aturan per ujian** (diatur di wizard):
   - hanya catat (bawaan); atau
   - **kunci sementara** setelah N kali keluar sampai pengawas membuka. Waktu tetap berjalan.
   - Tidak ada diskualifikasi otomatis. Keputusan ada di guru/koordinator.
5. Penghalang ringan: seleksi teks soal dan klik kanan dimatikan; layar penuh diminta. Di HP disarankan "Tambahkan ke layar utama" agar tampil tanpa bilah alamat.
6. **Safe Exam Browser (opsional, boleh tidak diimplementasikan di MVP 4):**
   - per sesi `seb_required`;
   - server memeriksa `X-SafeExamBrowser-ConfigKeyHash`;
   - file `.seb` bisa diunduh per sesi.
7. Ringkasan integritas per peserta muncul di hasil (T148): jumlah keluar, total durasi di luar halaman, pindah perangkat.

## File yang disentuh
- `db/migrations/0000XX_exam_events.up.sql` dan `.down.sql`
- `internal/exam/integrity.go`, `integrity_test.go`, (opsional) `seb.go`
- `web/static/js/exam-take.js`, `web/static/audio/peringatan.mp3` (dan `.ogg`)

## Kriteria selesai
- [ ] Sesi di ruang, komputer: pindah tab langsung berbunyi (uji Playwright: deteksi pemutaran audio)
- [ ] Sesi di ruang, HP: kembali ke halaman memutar bunyi dan menampilkan layar merah (uji manual Android Chrome, iOS Safari)
- [ ] Sesi jarak jauh tidak berbunyi, tetapi kejadian tercatat
- [ ] Kejadian muncul di layar pengawas ≤ 15 detik
- [ ] Login perangkat kedua ditolak sampai diizinkan
