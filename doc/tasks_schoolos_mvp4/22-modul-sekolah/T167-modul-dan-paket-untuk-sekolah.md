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

## Kriteria selesai
- [ ] Uji: halaman menampilkan modul aktif dan tidak aktif sesuai `school_modules`; bendahara dan guru tidak bisa membuka bagian ini
- [ ] Bagian pengaturan modul mati tidak tampil
