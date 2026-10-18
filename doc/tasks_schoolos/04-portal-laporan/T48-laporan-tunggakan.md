# T48 — Laporan: tunggakan

**Fase:** 04 Portal dan Laporan
**Dependensi:** T42
**Estimasi:** 1 hari

## Tujuan
Daftar tunggakan bisa diunduh sebagai Excel dan PDF dengan angka yang sama seperti di layar.

## Langkah
1. Gunakan `ListArrears` dari T42 sebagai sumber data. Jangan menulis query kedua.
2. Tambahkan fungsi `ArrearsReport` di `internal/reports/arrears.go` yang memanggil `ListArrears` dan memetakan hasilnya ke kolom laporan.
3. Kolom Excel: NIS, nama siswa, kelas, jenis tagihan, periode, jatuh tempo, total, sudah dibayar, sisa, hari terlambat.
4. Sheet kedua "Ringkasan": total sisa, jumlah siswa menunggak, jumlah tagihan.
5. PDF: judul, tanggal cetak, tabel per kelas, total di akhir.
6. Handler:
   - `GET /laporan/tunggakan.xlsx?...`
   - `GET /laporan/tunggakan.pdf?...`
7. Akses: `bendahara`, `admin_tu`, `kepala_sekolah`.

## File yang disentuh
- `internal/reports/arrears.go`, `arrears_test.go`
- `internal/reports/export/excel.go` (tambahan sheet)
- `internal/reports/export/pdf.go` (tambahan template)
- `internal/reports/handler.go`

## Kriteria selesai
- [x] Total sisa di file sama dengan total di halaman T42 untuk filter yang sama
- [x] Jumlah baris di file sama dengan jumlah tagihan yang muncul di T42
- [x] Hari terlambat dihitung dari jatuh tempo ke tanggal cetak
- [x] Guru mendapat `403`
- [x] Ekspor 500 baris selesai di bawah 2 detik

## Catatan
- Laporan ini sering dibawa ke rapat yayasan. Pastikan nama sekolah dan tanggal cetak selalu tampil di PDF.
