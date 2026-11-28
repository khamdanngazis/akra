# T165 — Beranda, portal orang tua, laporan, dan impor mengikuti modul

**Fase:** 22 Modul per Sekolah
**Dependensi:** T163, T164
**Estimasi:** 2 hari

## Latar belakang
Halaman lintas modul selalu menampilkan bagian modul lain:
- **Beranda** (`internal/home`) menarik data dari absensi, keuangan, ujian, pengumuman, dan kesiapan Dapodik.
- **Portal orang tua** punya tab absensi, jadwal, nilai/rapor, tagihan/kuitansi, ujian, dan pengumuman.
- **Laporan** berisi kehadiran, tunggakan, dan pembuat laporan dengan sumber attendance, arrears, payments, dan grades.
- **Impor** punya target siswa dan nilai.
- **Pencarian header bendahara** menuju pembayaran.

## Langkah
1. **Beranda:** setiap kartu dan query diberi syarat modul. Query modul yang mati tidak dijalankan.
2. **Portal:**
   - tab dan kartu ringkasan anak per modul;
   - kuitansi lama tidak bisa dibuka bila billing mati (route sudah tertutup di T164; tautannya ikut disembunyikan);
   - **rapor PDF** tanpa bagian kehadiran bila attendance mati.
3. **Laporan:**
   - hub hanya menampilkan laporan modul aktif;
   - sumber pembuat laporan diberi `Module`, dan sumber modul mati disembunyikan;
   - konfigurasi tersimpan yang sumbernya mati tampil terkunci dengan keterangan.
4. **Impor:** target nilai hanya bila grades aktif; pemetaan kolom tersimpan untuk target itu tidak muncul.
5. **Pencarian header bendahara:** hanya bila billing aktif. Tanpa billing, header bendahara tanpa pencarian.
6. **Pintasan beranda dan bottom nav (T156):** cadangan dipakai bila modul item utama mati.

## Catatan implementasi
- **Pemindai tautan** (`TestCrossModulePagesFollowModules`): setiap modul dimatikan bergantian, lalu dibuka beranda kelima peran, portal orang tua (daftar anak dan detail anak), hub laporan, pembuat laporan, impor siswa, mapping impor, pusat Pengaturan, dan detail siswa. Halaman harus terbuka dan tidak memuat `href`/`action` ke route yang butuh modul itu (`module.Required`). Temuan awal: 30 tautan, kini 0.
- **Beranda:**
  - kartu ujian (sesi diawasi, koreksi esai) hanya bila ujian aktif;
  - ringkasan dan pembayaran terbaru bendahara hanya bila keuangan aktif;
  - kartu absensi guru dan kehadiran minggu ini hanya bila absensi aktif;
  - angka pengumuman dan "Belum siap Dapodik" hanya bila modulnya aktif, dan query-nya tidak dijalankan (`CASE WHEN`);
  - kartu pengumuman terbaru hanya bila pengumuman aktif.
- **Portal:**
  - tautan tab anak (tagihan, jadwal, nilai/rapor, ujian) hanya untuk modul aktif;
  - bagian absensi di detail anak hanya bila absensi aktif ("Profil dan absensi" menjadi "Profil");
  - kuitansi lama ikut tertutup karena halaman tagihannya tidak tampil dan route-nya sudah 404 (T164).
- **Rapor PDF:** tanpa modul absensi, absensi tidak dihitung dan tabel ketidakhadiran tidak dicetak. Rapor tetap terbentuk.
- **Laporan:**
  - hub hanya menampilkan rekap kehadiran dan tunggakan bila modulnya aktif;
  - riwayat ekspor di server menyembunyikan ekspor modul mati, dan halaman/unduhan job-nya menjawab "Modul tidak aktif".
- **Pembuat laporan:**
  - setiap sumber punya `Module` (kehadiran → absensi, tunggakan dan pembayaran → keuangan, nilai → nilai);
  - sumber modul mati tidak ditawarkan di pembangun maupun form konfigurasi, dan pratinjau atau simpan dengan sumber itu ditolak dengan pesan;
  - konfigurasi tersimpan tetap tampil dengan badge "Terkunci" dan keterangan, tanpa tombol Buka/Excel/PDF/Ubah (pemilik tetap bisa menghapus);
  - membuka atau mengunduhnya lewat URL menjawab 404 "terkunci".
- **Impor:** target nilai (`Target.Module`) tidak ditawarkan di mapping, mapping nilai yang tersimpan tidak tampil, dan tautan "Impor nilai" di halaman impor siswa hilang.
- **Pencarian header bendahara** hanya bila keuangan aktif.
- **Menu bawah dan pintasan:** cadangan sudah otomatis karena item menu modul mati tersaring sejak T163; kini diuji dengan modul mati.

## Kriteria selesai
- [x] Uji per modul mati: beranda setiap peran dan portal orang tua dirender tanpa error, dan tanpa tautan ke route modul itu (pemindai tautan)
- [x] Laporan dan pembuat laporan hanya menampilkan sumber modul aktif; konfigurasi lama terkunci, tidak error
- [x] Rapor PDF tetap terbentuk tanpa modul absensi
