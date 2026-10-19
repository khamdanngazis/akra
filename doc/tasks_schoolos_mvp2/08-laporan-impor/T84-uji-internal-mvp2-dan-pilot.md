# T84 — Uji internal MVP 2 dan kesiapan pilot

**Fase:** 08 Laporan dan Impor
**Dependensi:** T83
**Estimasi:** 3–5 hari

## Tujuan
Memastikan MVP 2 siap dipakai sekolah pilot, dengan alur yang diuji dari awal sampai akhir oleh orang yang tidak menulis fiturnya.

## Langkah
1. Siapkan staging dengan data fiktif tiga sekolah.
2. Jalankan skenario:
   - **Jadwal:** isi penugasan guru mapel, buat jadwal, coba buat bentrok dan pastikan ditolak, cek tampilan guru dan orang tua.
   - **Nilai:** isi komponen dengan total 100, input nilai satu kelas, cek rekap, cetak rapor dari satu siswa lengkap dan satu yang belum lengkap.
   - **Laporan:** buat konfigurasi pribadi, bagikan ke guru, cek guru bisa melihat dan tidak bisa mengubah, unduh Excel.
   - **Pembayaran:** cek rekap harian dan bulanan sama dengan total `ActivePaid`.
   - **Impor:** impor siswa dengan mapping dari format sekolah, impor nilai, cek laporan kesalahan.
3. Catat temuan di `docs/uji-internal-mvp2.md` dengan prioritas.
4. Periksa kriteria selesai MVP 2 dari rencana, satu per satu.
5. Siapkan panduan singkat untuk sekolah pilot dan jadwal dukungan awal.

## File yang disentuh
- `docs/uji-internal-mvp2.md`
- `docs/panduan-pilot.md`

## Kriteria selesai
- [ ] Semua skenario dijalankan minimal sekali
- [ ] Tidak ada temuan kritis atau tinggi yang terbuka
- [ ] Angka laporan, rekap, dan rapor saling cocok untuk data uji
- [ ] Panduan pilot tersedia
- [ ] Hasil dibahas sebelum sekolah pertama diundang

## Catatan
- Pilot 2–3 sekolah sesuai rencana dimulai setelah task ini selesai.
