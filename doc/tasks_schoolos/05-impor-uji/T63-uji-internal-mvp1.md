# T63 — Uji internal MVP 1

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T58, T59, T60, T61, T62
**Estimasi:** 3–5 hari

## Tujuan
Tim menjalankan alur kerja utama dari awal sampai akhir, lalu memeriksa setiap kriteria selesai MVP 1 satu per satu.

## Langkah
1. Siapkan lingkungan staging dengan data uji sekolah fiktif (bukan data sekolah nyata).
2. Jalankan skenario berurutan:
   - **Awal tahun ajaran:** buat tahun ajaran, aktifkan, buat kelas, impor 200 siswa dari Excel, cek laporan kesalahan untuk baris yang sengaja salah.
   - **Tagihan bulanan:** buat jenis tagihan SPP, jalankan tagihan massal untuk satu kelas, lalu untuk semua kelas, dan pastikan tidak ada duplikat.
   - **Pembayaran:** catat pembayaran tunai, catat transfer dengan bukti, cetak kuitansi, batalkan satu pembayaran dengan alasan, cek status tagihan.
   - **Absensi:** isi absensi satu kelas sebagai wali kelas, cek rekap.
   - **Portal:** login sebagai orang tua, lihat anak, tagihan, dan unduh kuitansi.
   - **Laporan:** unduh rekap kehadiran dan tunggakan, bandingkan angka dengan layar.
3. Catat setiap kendala di `docs/uji-internal.md` dengan kolom: skenario, langkah, hasil, prioritas (kritis, tinggi, sedang, rendah).
4. Periksa kriteria selesai MVP 1 dari `rencana_mvp_schoolos.md` bagian 3.2 satu per satu.
5. Buat daftar perbaikan dan urutkan berdasarkan prioritas.

## File yang disentuh
- `docs/uji-internal.md`
- Perbaikan kecil di kode (jika ditemukan kendala) dibuat sebagai task baru

## Kriteria selesai
- [x] Semua skenario di langkah 2 selesai dijalankan minimal sekali
- [x] Semua kriteria selesai MVP 1 dicek, masing-masing lulus atau punya catatan
- [ ] Tidak ada kendala kritis yang terbuka: K-7 (domain email Resend belum diverifikasi) masih terbuka. Masalahnya di konfigurasi, bukan kode. Atas keputusan pemilik proyek, K-7 dipindah menjadi syarat wajib sebelum pilot
- [x] Daftar perbaikan tinggi dan sedang sudah masuk backlog (`doc/uji-internal.md`, bagian Daftar perbaikan)
- [x] Hasil uji dibahas dan disetujui: T63 ditutup oleh pemilik proyek pada 18 Oktober 2026

Hasil: [doc/uji-internal.md](../../uji-internal.md). File itu ada di `doc/`, bukan `docs/`, mengikuti struktur repo.

## Catatan
- Uji ini dilakukan oleh orang yang tidak menulis fiturnya, jika memungkinkan. Itu membantu menemukan asumsi yang tersembunyi.
- Setelah T63 selesai, fase wawancara dimulai sesuai rencana MVP.
