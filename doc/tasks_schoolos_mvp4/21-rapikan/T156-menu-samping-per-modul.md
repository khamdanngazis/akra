# T156 — Menu samping dikelompokkan per modul

**Fase:** 21 Rapikan
**Dependensi:** —
**Estimasi:** 1,5 hari

## Latar belakang
Menu samping berupa satu daftar datar tanpa kelompok:
- **Admin TU** melihat 24 item.
- **Menu ujian** (Ujian, Agenda Ujian, Telaah Soal, Pengaturan Ujian, Tugas Tambahan) tercecer di bawah menu nilai.
- **Pengaturan** tersebar: Profil Sekolah dan Tahun Ajaran di tengah, Pengaturan Rapor dan Mata Pelajaran paling bawah.
- **Bottom navigation HP** mengambil 4 item pertama sesuai urutan daftar, sehingga ikut berubah setiap kali urutan menu diubah.

## Keputusan
Menu dikelompokkan per modul. Isi dan hak akses tiap item tidak berubah.

| Kelompok | Item (bila peran boleh) |
|---|---|
| (tanpa judul) | Beranda, Anak Saya (orang tua) |
| Akademik | Absensi, Rekap Absensi, Jadwal / Jadwal Saya, Kelas, Siswa, Input Nilai, Rekap Nilai, Komponen Nilai |
| Pendaftaran | Pendaftaran, Biaya Pendaftaran |
| Ujian | Ujian, Agenda Ujian (Telaah Soal dan Pengaturan Ujian pindah ke sub-navigasi modul di T157) |
| Keuangan | Pembayaran, Tagihan Massal, Rekap Pembayaran, Tunggakan, Jenis Tagihan, Pembatalan, Audit |
| Dapodik | Dapodik, Nilai ke Dapodik |
| Informasi | Pengumuman, Laporan |
| Pengaturan | Profil Sekolah, Tahun Ajaran, Mata Pelajaran, Penugasan Guru, Tugas Tambahan, Pengaturan Rapor, Pengaturan Keuangan |

- **Kelompok kosong** tidak tampil.
- **Kelompok dengan satu item** tampil tanpa judul, agar menu guru dan bendahara tetap ringkas.
- **Kelompok Pengaturan** tertutup secara bawaan (accordion) dan terbuka otomatis bila halaman aktif ada di dalamnya.

## Langkah
1. **Data menu:** tambah field `Group` di `nav.MenuItem` dan urutan kelompok di `nav`. Fungsi `MenuForUser` tetap mengembalikan item terurut. Tambah `nav.Groups(items)` yang mengembalikan kelompok beserta itemnya untuk template.
2. **Sidebar dan drawer** (`partials/app_header.html`):
   - render per kelompok dengan judul kecil (huruf kapital kecil, navy-200);
   - Pengaturan memakai `<details>`;
   - navigasi keyboard dan `aria-current` tetap berfungsi.
3. **Bottom navigation HP:** tambah field `Bottom` (prioritas) di item. `nav.Bottom` memilih maksimal 4 item berprioritas sesuai peran, bukan 4 item pertama. Usulan:
   - guru: Beranda, Absensi, Input Nilai, Ujian;
   - admin TU: Beranda, Siswa, Pendaftaran, Ujian;
   - bendahara: Beranda, Pembayaran, Tunggakan, Rekap;
   - kepala sekolah: Beranda, Rekap Absensi, Laporan, Ujian.
4. **Kartu pintasan beranda** (`nav.Shortcuts`) memakai prioritas yang sama dengan bottom navigation, agar tidak bergantung pada urutan menu.
5. **Halaman aktif:** `nav.Current` tidak berubah. Pastikan halaman yang pindah ke sub-navigasi modul (Telaah, Pengaturan Ujian) tetap menyorot item "Ujian".

## File yang disentuh
- `internal/platform/nav/menu.go`, `menu_test.go`
- `internal/platform/render/render.go` (fungsi template `menuGroups`)
- `web/templates/partials/app_header.html`, `web/templates/home.html`
- `web/tailwind/app.css`

## Kriteria selesai
- [ ] Uji `nav`: setiap item punya kelompok; snapshot urutan menu per peran (admin TU, kepala sekolah, guru, guru koordinator, bendahara, orang tua, sekolah tanpa modul ujian)
- [ ] Uji: bottom navigation dan pintasan beranda sesuai prioritas per peran, maksimal 4 dan 6 item
- [ ] Kelompok kosong tidak dirender; kelompok satu item tanpa judul
- [ ] Pengaturan tertutup bawaan, terbuka bila halaman aktif di dalamnya
- [ ] Tangkapan layar sidebar desktop, drawer tablet, dan bottom nav HP (360 px) untuk admin TU dan guru diperiksa
