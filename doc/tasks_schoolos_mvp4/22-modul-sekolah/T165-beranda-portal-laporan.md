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

## Kriteria selesai
- [ ] Uji per modul mati: beranda setiap peran dan portal orang tua dirender tanpa error, dan tanpa tautan ke route modul itu (pemindai tautan)
- [ ] Laporan dan pembuat laporan hanya menampilkan sumber modul aktif; konfigurasi lama terkunci, tidak error
- [ ] Rapor PDF tetap terbentuk tanpa modul absensi
