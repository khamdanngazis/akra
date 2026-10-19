# T78 — Antarmuka report builder

**Fase:** 08 Laporan dan Impor
**Dependensi:** T77
**Estimasi:** 2–3 hari

## Tujuan
Pengguna memilih sumber, kolom, filter, dan urutan dari layar, lalu melihat hasilnya sebelum disimpan atau diunduh.

## Langkah
1. Halaman `GET /laporan/bangun`:
   - Pilih sumber: daftar hanya menampilkan sumber yang boleh diakses peran pengguna.
   - Pilih kolom: daftar kolom dengan centang. Urutan bisa diatur.
   - Filter: baris filter dengan kolom, operator, dan nilai. Pilihan kolom dan operator sesuai tipe.
   - Urutan: satu atau dua kolom dengan arah.
2. Alpine.js dipakai untuk menambah atau menghapus baris filter dan mengatur urutan kolom. Semua validasi ulang dilakukan di server.
3. Tombol "Pratinjau" menjalankan query dengan batas 50 baris dan menampilkan hasilnya.
4. Tombol "Simpan" dan "Unduh" memanggil T77 dan T79.
5. Validasi akses sumber dan kolom dilakukan di server saat pratinjau dan simpan, bukan hanya di menu.

## File yang disentuh
- `web/templates/reports/builder.html`
- `internal/reports/builder/preview_handler.go`
- `web/static/js/report-builder.js`

## Kriteria selesai
- [ ] Guru tidak melihat sumber keuangan di daftar, dan `POST` langsung ke sumber itu ditolak
- [ ] Pratinjau 50 baris selesai di bawah 2 detik
- [ ] Filter tanggal dengan operator yang tidak cocok tipe ditolak di server
- [ ] Halaman berfungsi di layar 360px tanpa scroll horizontal
- [ ] Uji otorisasi T58 dan T59 diperbarui dengan route baru

## Catatan
- JavaScript di halaman ini hanya mengatur tampilan. Keputusan akses dan validasi ada di server.
