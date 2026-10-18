# T51 — Halaman beranda per peran

**Fase:** 04 Portal dan Laporan
**Dependensi:** T10, T42, T27
**Estimasi:** 1 hari

## Tujuan
Setiap peran melihat beranda dan menu yang sesuai. Peran lain tidak melihat menu yang tidak boleh diakses.

## Langkah
1. Buat `internal/platform/nav/menu.go`:
   - Daftar menu dengan kolom `roles []string` untuk setiap item.
   - Fungsi `MenuFor(identity) []MenuItem` yang hanya mengembalikan item yang cocok.
2. Definisi menu minimal:
   - `admin_tu`: Tahun ajaran, Kelas, Siswa, Absensi (rekap), Laporan kehadiran, Pengumuman
   - `bendahara`: Jenis tagihan, Tagihan massal, Pembayaran, Tunggakan, Laporan tunggakan
   - `guru`: Absensi (kelas saya)
   - `kepala_sekolah`: Rekap absensi, Tunggakan (baca), Laporan, Pengumuman (baca), Audit
   - `orang_tua`: Portal (anak saya), Pengumuman
3. Beranda menampilkan ringkasan kecil sesuai peran:
   - Bendahara: total sisa tunggakan dan jumlah pembayaran hari ini
   - Guru: kelas yang belum diisi absensi hari ini
   - Admin/TU: jumlah siswa aktif dan tahun ajaran aktif
   - Kepala sekolah: ringkasan kehadiran minggu ini
4. Jika pengguna punya dua peran, gabungkan menu tanpa duplikat.
5. Template `layout.html` (T11) dipanggil dengan menu hasil `MenuFor`.
6. Jika tahun ajaran aktif belum ada, beranda menampilkan pesan dan tautan ke pengaturan tahun ajaran (untuk admin).

## File yang disentuh
- `internal/platform/nav/menu.go`, `menu_test.go`
- `internal/platform/home/handler.go`
- `web/templates/home.html`, `web/templates/layout.html` (bagian menu)

## Kriteria selesai
- [x] Uji unit: setiap peran hanya mendapat item menu yang sesuai
- [x] Guru tidak melihat menu tagihan atau siswa
- [x] Bendahara tidak melihat menu absensi
- [x] Ringkasan beranda bendahara sama dengan T42 untuk total sisa
- [x] Pengguna dengan dua peran melihat gabungan menu tanpa duplikat

## Catatan
- Menu yang tersembunyi tetap harus dicek di server (`RequireRole`). Menyembunyikan menu saja bukan kontrol akses.
