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

## Catatan implementasi
- **Satu penjaga terpusat, bukan middleware per paket.** Katalog modul berisi pola route per modul (`Info.Routes`, dicocokkan per segmen, `*` untuk satu segmen, akhiran `.pdf`/`.xlsx` ikut). `module.Required(path)` mengembalikan semua modul yang dibutuhkan path, mis. `/portal/siswa/{id}/tagihan` butuh portal **dan** keuangan. Middleware `module.Enforce` dipasang sekali di grup login, setelah `module.Middleware`. Route baru otomatis ikut tertutup tanpa perlu ingat memasang middleware.
- **Pemetaan tambahan dari analisis registry:**
  - `/pengaturan/absensi` → absensi;
  - tab portal orang tua (`/portal/siswa/*/jadwal|nilai|rapor|tagihan|ujian`, `/portal/pembayaran`) → modul tab itu;
  - `/siswa/{id}/wali/{id}/akun` (akun portal wali) → portal;
  - `/s/{kode}` (login sekolah) dan `/s/{kode}/logo` tetap inti, hanya `/s/*/daftar|pindah|status` milik pendaftaran.
- **Halaman staf:** 404 berjudul "Modul tidak aktif" dengan pesan "Modul {nama} tidak aktif untuk sekolah Anda. Hubungi admin TU atau tim AKRA.", dirender sebelum handler sehingga tanpa data modul. Request tanpa `Accept: text/html` (JSON, unduhan) mendapat 404 biasa.
- **Route publik:**
  - `/s/{kode}/daftar|pindah|status`: 404 seperti sekolah tidak ada bila pendaftaran mati;
  - `/u/{kode}`: tetap diperiksa oleh login peserta (tahap rilis);
  - `/u/...` dengan sesi peserta: kini juga 404 bila modul ujian dimatikan saat peserta sudah masuk;
  - `/u/`, `/u/cek`, panduan PDF, `sw.js` tidak terikat sekolah dan tetap publik;
  - `/aktivasi`: link aktivasi wali tidak berlaku (400 "link tidak berlaku") bila portal mati.
- **`exam.RequireModule` tetap** untuk cek tahap rilis (r1–r3); aktif/nonaktif kini juga dijaga penjaga terpusat.
- **Registry:** field `Module` (324 dari 397 route) diisi pemilik paling spesifik. `TestRegistryModules` memastikan field itu sama dengan `module.Owner(Pattern)`, sehingga registry dan katalog tidak bisa berbeda.
- **Uji generik `TestModuleGate`:** untuk setiap modul opsional di sekolah A, 359 request ke route modul itu 404 (per modul: absensi 14, jadwal 9, nilai 19, keuangan 40, pendaftaran 75, dapodik 16, ujian 167, pengumuman 4, portal 15). Halaman staf memuat pesan modul tanpa nama siswa sekolah itu, contoh halaman modul lain tetap terbuka, dan sekolah B tidak terpengaruh.

## Kriteria selesai
- [x] Semua route modul opsional tertutup bila modulnya nonaktif (uji generik per modul)
- [x] Uji lintas sekolah (T58) dan orang tua (T59) lulus
- [x] Halaman "modul tidak aktif" tidak memuat data modul (dicek terhadap nama siswa sekolah itu)
