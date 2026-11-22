# T156 — Menu seluruh aplikasi: kelompok, pusat pengaturan, dan navigasi HP

**Fase:** 21 Rapikan
**Dependensi:** —
**Estimasi:** 3 hari
**Analisis:** [doc/analisis-menu.md](../../analisis-menu.md)

## Latar belakang
Ringkasan analisis menu seluruh aplikasi:
- **Datar dan tanpa kelompok:** admin TU melihat 24 item, kepala sekolah 20.
- **Urutan mencampur area:** Dapodik di antara Pendaftaran dan Kelas; menu nilai di bawah pengaturan.
- **Pengaturan tercecer di 8 tempat**, termasuk absensi per mapel di halaman Mata Pelajaran.
- **Label tidak menyebut objek:** "Audit" yang sebenarnya audit keuangan; "Pembayaran" yang langsung membuka formulir.
- **Impor nilai tanpa titik masuk.**
- **Bottom nav HP dan pintasan beranda mengikuti urutan daftar.** Akibatnya guru tidak punya Input Nilai maupun Ujian di bottom nav.

## Keputusan
Sesuai bagian 3 analisis.

1. **Kelompok:**
   - Beranda tanpa judul;
   - Akademik, Ujian, Pendaftaran, Keuangan, Dapodik, Informasi;
   - Pengaturan di bawah.
2. **Setiap item membawa modulnya** (`Module`), lihat [ADR 0002](../../adr/0002-modul-per-sekolah.md):

   | Modul | Item |
   |---|---|
   | inti | Beranda, Siswa, Kelas, Mapel Saya, Laporan, Pengaturan |
   | attendance | Absensi, Rekap Absensi |
   | schedule | Jadwal, Jadwal Saya |
   | grades | Input Nilai, Rekap Nilai, Komponen Nilai |
   | exam | Ujian, Agenda Ujian |
   | admission | Pendaftaran, Biaya Pendaftaran |
   | billing | semua item Keuangan |
   | dapodik | Dapodik, Nilai ke Dapodik |
   | announcements | Pengumuman (staf dan orang tua) |
   | portal | Anak Saya |

   Sampai T163 selesai, modul selain `exam` dianggap selalu aktif.
3. **Pusat Pengaturan** (`/pengaturan`) menggantikan item Profil Sekolah, Tahun Ajaran, Mata Pelajaran, Penugasan Guru (admin), Tugas Tambahan, Pengaturan Rapor, Pengaturan Keuangan, dan Pengaturan Ujian di menu.
   - Isinya: kartu per bagian (Sekolah, Akademik, Absensi, Nilai & rapor, Keuangan, Pendaftaran, Ujian), masing-masing menuju halaman yang sudah ada.
   - Hanya bagian yang boleh dibuka peran itu yang tampil.
   - **Absensi per mapel** dipindah dari halaman Mata Pelajaran ke bagian Absensi.
   - **Pengaturan Pendaftaran** (`/pendaftaran/pengaturan`) juga ditautkan dari bagian Pendaftaran.
4. **Label:**
   - Audit → Audit Keuangan;
   - Pembayaran → Catat Pembayaran;
   - Penugasan Guru untuk guru → Mapel Saya (tetap `/pengaturan/penugasan`).
5. **Bottom nav dan pintasan beranda per peran** (field `Bottom` dan `Shortcut` berprioritas). Bila item tidak tersedia, prioritas berikutnya dipakai:

   | Peran | Bottom nav (maks. 4) |
   |---|---|
   | Admin TU | Beranda, Siswa, Tunggakan, Ujian (cadangan: Rekap Absensi, Pengumuman) |
   | Kepala sekolah | Beranda, Rekap Absensi, Laporan, Tunggakan (cadangan: Siswa, Pengumuman) |
   | Guru | Beranda, Absensi, Input Nilai, Ujian (cadangan: Jadwal Saya, Pengumuman) |
   | Bendahara | Beranda, Catat Pembayaran, Tunggakan, Rekap Pembayaran (cadangan: Laporan) |
   | Orang tua | Anak Saya, Pengumuman |

6. **Titik masuk:**
   - tombol "Impor dari Excel" di Input Nilai dan Rekap Nilai (menuju `/impor/nilai`);
   - halaman impor menautkan pemetaan kolom.
7. Kelompok kosong tidak tampil. Kelompok satu item tampil tanpa judul. Item aktif tetap disorot untuk halaman turunan, termasuk halaman pengaturan yang kini dibuka dari pusat Pengaturan (item "Pengaturan" yang menyala).

## Langkah
1. **`nav.MenuItem`:** tambah `Group`, `Bottom`, dan `Shortcut` (prioritas per peran). Isi `Module` untuk semua item. Tambah `nav.Groups(items)`. `Bottom` dan `Shortcuts` memakai prioritas, bukan urutan daftar.
2. **`partials/app_header.html`:**
   - sidebar dan drawer per kelompok, dengan judul kelompok kecil;
   - bottom nav dari `nav.Bottom`;
   - tombol Menu tetap ada bila item lebih dari 4.
3. **Pusat Pengaturan:**
   - handler dan template `/pengaturan` (registry: `KindPage`, peran admin TU, kepala sekolah, bendahara);
   - pindahkan absensi per mapel ke bagian Absensi (form sama, route baru di bawah `/pengaturan/absensi` atau bagian di halaman pusat);
   - halaman Mata Pelajaran tidak lagi memuat pengaturan absensi.
4. **Label dan judul halaman:** sesuaikan dengan label menu baru (Audit Keuangan, Catat Pembayaran, Mapel Saya), termasuk breadcrumb.
5. **Tombol impor** di halaman nilai (hanya guru dan admin TU yang boleh impor).
6. **Beranda:** kartu pintasan dari `nav.Shortcuts` berprioritas.

## File yang disentuh
- `internal/platform/nav/menu.go`, `menu_test.go`, `internal/platform/render/render.go`
- `web/templates/partials/app_header.html`, `web/templates/home.html`
- `internal/school` atau paket baru untuk pusat pengaturan; `internal/attendance` (pengaturan per mapel); `web/templates/academic/subjects.html`
- `web/templates/grades/*.html` (tombol impor), `internal/testutil/routes.go`
- `web/tailwind/app.css`

## Kriteria selesai
- [ ] Uji snapshot menu per peran (admin TU, kepala sekolah, guru, guru koordinator, bendahara, orang tua): kelompok, urutan, dan jumlah sesuai tabel analisis
- [ ] Uji bottom nav dan pintasan per peran, termasuk cadangan saat modul ujian nonaktif
- [ ] Semua halaman pengaturan lama tercapai dari pusat Pengaturan sesuai peran (uji tautan); absensi per mapel tersimpan dari lokasi baru
- [ ] Setiap item menu punya `Module`
- [ ] Tangkapan layar sidebar desktop, drawer tablet, dan bottom nav HP 360 px untuk semua peran diperiksa
