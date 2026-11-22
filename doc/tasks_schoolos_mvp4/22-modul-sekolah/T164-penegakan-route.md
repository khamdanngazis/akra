# T164 — Penegakan modul di semua route

**Fase:** 22 Modul per Sekolah
**Dependensi:** T163
**Estimasi:** 2 hari

## Latar belakang
Hanya route modul ujian yang memeriksa status modul. Dari sekitar 400 route di registry, lebih dari 200 milik modul opsional (keuangan, pendaftaran, Dapodik, absensi, jadwal, nilai, pengumuman, portal) dan selalu terbuka.

## Langkah
1. **Middleware `module.Require(id)`:**
   - dipasang di grup route setiap paket: `attendance`, `schedule`, `grades` (termasuk `/pengaturan/rapor` dan `/impor/nilai`), `billing` dan `payments`, `admission` (staf dan publik `/s/...`), `dapodik`, `exam` (mengganti `exam.RequireModule`, tetap dengan cek tahap rilis), `announcements`, dan `portal`;
   - laporan per jenis: kehadiran → attendance, tunggakan → billing.
2. **Halaman staf:** status 404 dengan pesan "Modul {nama} tidak aktif untuk sekolah Anda. Hubungi admin TU atau tim AKRA." Tanpa data apa pun dari modul itu. **Route publik dan JSON:** 404 biasa.
3. **Registry route** (`internal/testutil/routes.go`): field `Module` di setiap entri.
4. **Uji generik:** untuk setiap modul opsional, matikan modul itu pada sekolah A, lalu:
   - semua route modul itu 404;
   - satu route contoh dari setiap modul lain tetap 200;
   - sekolah B (modul aktif) tidak terpengaruh.
5. **Uji kelengkapan registry:** route yang path-nya cocok dengan prefix modul di katalog wajib mencantumkan modul itu.

## Kriteria selesai
- [ ] Semua route modul opsional tertutup bila modulnya nonaktif (uji generik per modul)
- [ ] Uji lintas sekolah (T58) dan orang tua (T59) lulus
- [ ] Halaman "modul tidak aktif" tidak memuat data modul (pemindai seperti T153)
