# T167 — Halaman "Modul & paket" untuk sekolah dan pusat pengaturan per modul

**Fase:** 22 Modul per Sekolah
**Dependensi:** T156 (pusat Pengaturan), T163
**Estimasi:** 0,5 hari

## Latar belakang
Sekolah perlu tahu modul apa yang dipakainya. Modul diatur tim implementator AKRA (keputusan 1 ADR 0002), sehingga sekolah tidak menyalakan modul sendiri. Pusat Pengaturan dari T156 masih menampilkan semua bagian, termasuk milik modul yang tidak aktif.

## Langkah
1. **Bagian "Modul & paket" di pusat Pengaturan** (admin TU dan kepala sekolah, baca saja):
   - nama paket dan daftar modul aktif, masing-masing dengan isi singkat dan tanggal aktif;
   - modul lain dari katalog sebagai "tidak termasuk paket";
   - keterangan: "Butuh modul lain? Hubungi tim implementator AKRA."

   Tombol permintaan menyusul bersama UI implementator.
2. **Pusat Pengaturan** hanya menampilkan bagian modul aktif: Absensi, Nilai & rapor, Keuangan, Pendaftaran, Ujian.

## Catatan implementasi
- **Halaman `/pengaturan/modul`** (Pengaturan → Sekolah → Modul & Paket), admin TU dan kepala sekolah, baca saja:
  - label paket dari `schools.package` (Paket Dasar/Akademik/Keuangan/Lengkap; kosong = tidak ditulis);
  - modul aktif dengan isi singkat, daftar pengaturannya, dan "Aktif sejak {tanggal}" (dari `school_modules.changed_at`; modul bawaan tanpa baris hanya "Aktif"; Inti "Selalu aktif");
  - modul lain dari katalog sebagai "Tidak termasuk paket";
  - keterangan untuk menghubungi tim implementator AKRA, dan bahwa data modul yang dimatikan tidak hilang.
- **Pusat Pengaturan** sudah hanya menampilkan bagian modul aktif sejak T163 (`nav.SettingsFor` dengan modul sekolah); kini diuji.
- **Tanggal untuk sekolah lama** adalah tanggal migrasi 089, karena tanggal aktivasi sebenarnya tidak tercatat sebelum T163.
- `SettingsHubHandler` kini menerima `db` dan `logger`.

## Kriteria selesai
- [x] Uji: halaman menampilkan modul aktif dan tidak aktif sesuai `school_modules`; bendahara dan guru tidak bisa membuka bagian ini
- [x] Bagian pengaturan modul mati tidak tampil
