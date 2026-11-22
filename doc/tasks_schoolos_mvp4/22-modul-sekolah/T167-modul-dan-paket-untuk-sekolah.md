# T167 — Halaman "Modul & paket" untuk sekolah dan pusat pengaturan per modul

**Fase:** 22 Modul per Sekolah
**Dependensi:** T156 (pusat Pengaturan), T163
**Estimasi:** 1 hari

## Latar belakang
Sekolah perlu tahu modul apa yang dipakainya dan cara meminta modul lain. Pusat Pengaturan dari T156 masih menampilkan semua bagian, termasuk milik modul yang tidak aktif.

## Langkah
1. **Bagian "Modul & paket" di pusat Pengaturan** (admin TU, kepala sekolah):
   - nama paket dan daftar modul aktif, masing-masing dengan isi singkat dan tanggal aktif;
   - modul lain dari katalog sebagai "tidak termasuk paket", dengan tombol **Minta aktifkan**.
   - Tombol itu mengirim permintaan ke tim AKRA lewat kanal yang sama dengan alert (Telegram/email T145) dan tercatat di audit.
   - Sesuai keputusan 1 ADR: sekolah tidak bisa menyalakan sendiri.
2. **Pusat Pengaturan** hanya menampilkan bagian modul aktif: Absensi, Nilai & rapor, Keuangan, Pendaftaran, Ujian.
3. **Batas permintaan:** satu permintaan per modul per hari per sekolah.

## Kriteria selesai
- [ ] Uji: halaman menampilkan modul aktif dan tidak aktif sesuai `school_modules`; bendahara dan guru tidak bisa membuka bagian ini
- [ ] Permintaan aktivasi terkirim ke kanal tim (uji dengan notifier palsu) dan terbatas satu per hari
- [ ] Bagian pengaturan modul mati tidak tampil
