# T133 — Antikecurangan

**Fase:** 16 Pelaksanaan
**Dependensi:** T130
**Estimasi:** 2 hari
**Keputusan:** SEB opsional; bunyi tanda keluar halaman wajib, diatur per sesi (lihat indeks)

## Tujuan
Mengurangi kecurangan di HP dan lab secara wajar, tanpa membuat ujian gagal karena aturan yang terlalu ketat.

## Prinsip
Browser biasa tidak bisa mencegah kecurangan secara mutlak. Yang bisa dilakukan:
1. **mempersulit**: acak soal/opsi, token, satu perangkat;
2. **mencatat**: keluar tab, layar penuh dilepas;
3. **memberi pengawas alat untuk bertindak**.

Pencegahan penuh di lab memakai exam browser (opsional).

## Langkah
1. **Satu perangkat aktif per attempt:** `device_id` acak disimpan di perangkat saat Mulai. Perangkat lain ditolak dengan pesan "Minta pengawas mengizinkan pindah perangkat". Pengawas bisa mengizinkan (T134).
2. **Log kejadian** (`exam_events`):
   - keluar tab atau aplikasi (`visibilitychange`, `blur`);
   - layar penuh dilepas, muat ulang;
   - salin/tempel;
   - login dari perangkat lain.

   Dikirim bersama simpan jawaban, bukan request terpisah.
3. **Aturan per paket:** hanya catat (bawaan), atau kunci sementara setelah N kali keluar sampai pengawas membuka. Tidak ada diskualifikasi otomatis.
4. **Bunyi tanda keluar halaman** (sesi `alert_sound` aktif, T128):
   - **Pemicu:** peserta keluar dari halaman ujian, misalnya pindah tab, membuka aplikasi lain, menekan tombol home, menarik panel notifikasi lalu membuka aplikasi lain, atau keluar dari layar penuh.
   - **Bunyi:** nada peringatan yang keras dan berbeda dari nada HP biasa, berulang selama peserta di luar halaman.
     - Di komputer, bunyi bisa tetap terdengar saat tab lain dibuka.
     - Di HP, browser menghentikan halaman di latar belakang, sehingga bunyi diputar **segera saat peserta kembali** ke halaman ujian, minimal 5 detik.
   - **Layar merah** saat kembali: "Anda keluar dari halaman ujian (ke-N kali)". Bunyi berhenti setelah peserta menekan **Lanjutkan**; kejadian tetap tercatat.
   - **Izin audio:** browser hanya mengizinkan suara setelah ada sentuhan pengguna, jadi audio "dibuka" saat peserta menekan **Mulai** (T130). Halaman konfirmasi punya **tes suara**: peserta menekan tombol, bunyi diputar, peserta mengonfirmasi terdengar, dan pengawas mengingatkan agar volume dinaikkan dan mode senyap dimatikan.
   - **Keterbatasan** (dijelaskan di panduan pengawas): volume dan mode senyap HP tidak bisa diatur dari browser. Indikator di halaman pengawas (T134) tetap jadi acuan utama.
   - Sesi `jarak_jauh`: tidak ada bunyi; kejadian hanya dicatat.
5. Mode layar penuh diminta saat Mulai (di HP: saran "pasang ke layar utama"). Teks soal tidak bisa diseleksi, dan klik kanan dimatikan. Ini hanya penghalang ringan.
6. Acak soal dan opsi per peserta (T127) adalah perlindungan utama untuk ujian di lab.
7. **Opsional, per sesi (boleh tidak diimplementasikan di MVP 4):** wajib **Safe Exam Browser**. Server memeriksa header `X-SafeExamBrowser-ConfigKeyHash`. Disediakan unduhan file konfigurasi `.seb` per sesi.
8. Pengawas melihat jumlah kejadian per peserta secara langsung (T134). Rekap kejadian ikut di hasil (T136).

## File yang disentuh
- `db/migrations/0000XX_exam_events.up.sql` dan `.down.sql`
- `internal/exam/integrity.go`, `integrity_test.go`, `seb.go`
- `web/static/js/exam-take.js`

## Kriteria selesai
- [ ] Login dari perangkat kedua ditolak sampai diizinkan pengawas
- [ ] Keluar tab tercatat dan terlihat di halaman pengawas
- [ ] Kunci sementara setelah N kali keluar bisa dibuka pengawas, dan waktunya tetap berjalan
- [ ] Sesi `di_ruang`: keluar tab di komputer memutar bunyi; di HP, bunyi dan layar merah muncul saat kembali (uji browser dan uji manual di Android/iOS)
- [ ] Sesi `jarak_jauh` tidak memutar bunyi, tetapi kejadian tetap tercatat
- [ ] (Jika SEB diimplementasikan) Sesi dengan SEB wajib menolak browser biasa; SEB dengan konfigurasi benar diterima
