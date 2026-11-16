# T142 — Kompatibilitas perangkat dan hemat data

**Fase:** 17 Pelaksanaan
**Dependensi:** T138–T141
**Estimasi:** 2,5 hari

## Tujuan
Ujian berjalan di perangkat yang benar-benar ada di sekolah: HP Android murah, iPhone lama, dan komputer lab Windows yang jarang diperbarui.

## Langkah
1. **Matriks dukungan** (dicantumkan di halaman cek perangkat T136 dan di panduan):
   - Chrome Android ≥ 90, Safari iOS ≥ 15;
   - Chrome/Edge desktop 2 tahun terakhir, Firefox ESR;
   - browser bawaan vendor (Samsung Internet, Mi Browser, Opera Mini): diuji, dan yang bermasalah diberi saran memakai Chrome.
2. **Laboratorium uji perangkat:** minimal 6 perangkat nyata, mis. Android Go 2 GB, Android 4 GB, iPhone SE/8, tablet murah, PC Windows 10 dengan Chrome lama, Chromebook. Jalankan skenario ujian penuh di masing-masing.
3. **Hemat data:**
   - gambar soal disajikan dalam ukuran yang sesuai layar (`srcset`), dengan pratinjau ringan;
   - payload soal dikompresi (gzip/brotli);
   - perkiraan data per ujian 40 soal ditampilkan di cek perangkat;
   - gambar dimuat lebih dulu (prefetch) saat koneksi baik.
4. **Mode lab:**
   - kiosk (Chrome `--kiosk`) didokumentasikan untuk operator lab;
   - URL pendek ujian sekolah untuk diketik di semua komputer;
   - penanganan banyak peserta di belakang satu IP (NAT lab): rate limit tidak memblokir satu lab.
5. **Layar kecil dan besar:** 360 px sampai 1920 px; orientasi lanskap di HP; zoom browser 200% tetap bisa dipakai.

## Kriteria selesai
- [ ] Skenario ujian penuh lulus di semua perangkat matriks (laporan dengan tangkapan layar)
- [ ] 40 siswa di belakang satu IP bisa login bersamaan tanpa terkena rate limit
- [ ] Data per ujian 40 soal (10 bergambar) < 2 MB
