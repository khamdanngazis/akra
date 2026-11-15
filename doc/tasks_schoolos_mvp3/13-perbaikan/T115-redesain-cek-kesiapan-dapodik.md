# T115 — Redesain halaman cek kesiapan Dapodik

**Fase:** 13 Perbaikan
**Dependensi:** T103, T114
**Estimasi:** 0,5 hari

## Latar belakang
Halaman **Cek kesiapan Dapodik** (`/dapodik/cek`) menampilkan satu kartu per kelas. Dengan banyak kelas, halaman sangat panjang dan membingungkan (masukan pengguna, 2026-11-15):
- tidak ada ringkasan berapa siswa yang pasti ditolak dan berapa yang hanya peringatan;
- TU tidak bisa memperbaiki satu jenis masalah sekaligus (mis. semua NIK kosong);
- tidak ada pencarian, filter kelas, atau halaman.

## Keputusan (2026-11-15)
Langsung dikerjakan tanpa mockup. Aturan pemeriksaan (`Check`) tidak berubah.

## Langkah
1. **Satu daftar** siswa bermasalah untuk semua kelas. Kelas tampil di bawah nama siswa. Siswa yang punya masalah "pasti ditolak" tampil lebih dulu.
2. **Kartu ringkasan:** siswa diperiksa, pasti ditolak Dapodik, hanya perlu diperiksa, aman. Dua kartu tengah sekaligus menjadi filter tingkat (`?tingkat=error|peringatan`).
3. **Chip jenis masalah** dengan jumlah siswa, urut terbanyak. Jenisnya: NIK, No KK, NISN, nama siswa, tanggal lahir, nama ibu/ayah, tahun lahir orang tua, RT/kode pos, titik lokasi. Klik untuk menyaring (`?masalah=`), klik lagi untuk melepas.
4. **Pencarian** nama/NIS, **filter kelas** (termasuk "Belum punya kelas"), dan **halaman** 50 siswa.
5. **Tiap masalah** diberi label "Ditolak" atau "Periksa".
   - Tautan **Perbaiki** menuju langsung ke bagian halaman data rinci yang tepat (identitas, alamat, ibu, ayah, wali).
   - Untuk nama dan tanggal lahir, tautannya ke halaman ubah data siswa.
   - Satu tautan per bagian tujuan, dan hanya untuk Admin/TU.
6. **Uji:** filter jenis, tingkat, dan pencarian; jenis tak dikenal diabaikan; tautan perbaikan; tanpa siswa sekolah lain. Cek tampilan di 390 px dan 1280 px.

## Kriteria selesai
- [x] Tidak ada lagi kartu per kelas; semua siswa bermasalah dalam satu daftar dengan halaman
- [x] Ringkasan jumlah pasti ditolak / perlu diperiksa / aman
- [x] Filter jenis masalah, tingkat, kelas, dan pencarian
- [x] Tautan Perbaiki menuju bagian data yang tepat
- [x] Tidak melebar di HP

## Catatan implementasi
- `internal/dapodik/check_handler.go`: `issueKind` memetakan field masalah ke jenis, label, dan anchor halaman data rinci.
- Kelas `.stat-filter` kini juga mendukung kartu non-tautan (`<div>`, untuk "Aman") dan warna `is-bad`.
- Dengan data demo 240 siswa, halaman desktop sekitar 4.800 px untuk 50 siswa per halaman, tanpa gulir horizontal di 390 px.
