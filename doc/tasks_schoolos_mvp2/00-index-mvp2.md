# Daftar Task MVP 2 SchoolOS

Acuan: `rencana_mvp_schoolos.md` bagian 5, dan `rancangan_arsitektur_schoolos.md`.

## Gerbang Masuk
Task MVP 2 **tidak dimulai** sebelum:
1. MVP 1 lulus T63 (uji internal).
2. Wawancara minimal 10 calon pengguna selesai (rencana MVP, bagian 4).
3. Daftar fitur MVP 2 disetujui dari hasil wawancara.

Task yang bergantung pada hasil wawancara ditandai **[wawancara]**. Jangan dikerjakan sebelum keputusan terkait sudah ada.

## Aturan Task
- Sama dengan MVP 1: kecil, punya dependensi, punya kriteria selesai yang bisa dicek.
- Setiap route baru wajib masuk daftar uji lintas sekolah (T58) dan uji orang tua (T59).
- Setiap aturan nilai dan rekap pembayaran wajib punya uji unit.

## Daftar Fase

| Folder | Task | Isi | Estimasi |
|---|---|---|---|
| `06-jadwal-penugasan/` | T64–T68 | Mata pelajaran, penugasan guru mapel, jadwal, tampilan jadwal, absensi per mapel | 2 minggu |
| `07-nilai/` | T69–T75 | Komponen penilaian, input nilai, nilai akhir, rekap nilai, format rapor, nilai di portal | 2–3 minggu |
| `08-laporan-impor/` | T76–T84 | Laporan yang bisa disesuaikan, rekap pembayaran harian dan bulanan, impor dengan mapping, uji MVP 2 | 2–3 minggu |

Total perkiraan: 6–8 minggu, sesuai rencana MVP.

## Catatan Cakupan
- **Pengumuman ke kelas atau semua orang tua** sudah ada di MVP 1 (T50). Di MVP 2 tidak ada task pengumuman baru. Yang mungkin ditambahkan hanya kanal baru, dan itu di luar cakupan.
- **Notifikasi WhatsApp dan email** tetap di luar MVP 1 dan MVP 2.

## Urutan Kritis
1. T64 → T65 → T66 (dasar jadwal dan penugasan)
2. T69 → T70 → T71 → T72 (dasar nilai)
3. T76 → T77 → T78 (laporan yang bisa disesuaikan)
4. T83 dan T84 terakhir

## Keputusan yang Harus Diambil dari Wawancara
- Apakah absensi per mapel dibutuhkan (T68)
- Skala nilai, komponen penilaian, dan rumus nilai akhir (T69, T71)
- Format rapor yang dipakai sekolah (T73, T74). T73: format bisa dipilih (angka atau Kurikulum Merdeka); deskripsi capaian Merdeka menjadi task terpisah T74b
- Kolom laporan yang paling sering dibutuhkan (T76)
- Format file impor yang paling sering dipakai sekolah (T81)
- Definisi rekap pembayaran harian (T80): tanggal bayar atau tanggal masuk rekening
