# T42 — Daftar tunggakan

**Fase:** 03 Keuangan
**Dependensi:** T31, T10
**Estimasi:** 1 hari

## Tujuan
Bendahara dan kepala sekolah bisa melihat daftar tagihan yang sudah lewat jatuh tempo dan belum lunas, dengan filter kelas, jenis tagihan, dan rentang jatuh tempo.

## Langkah
1. Buat `internal/billing/arrears.go`:
   - `ListArrears(ctx, schoolID, filter, today)`
   - Filter:
     - `status <> 'lunas'`
     - `due_date < today`
     - Opsional: kelas, jenis tagihan (`invoice_items.fee_type_id`), rentang `due_date`.
   - Hitung sisa per tagihan: `total_amount - SUM(payments aktif)`.
2. Parameter `today` diambil dari zona Asia/Jakarta di handler, lalu dikirim ke service.
3. Query memakai `school_id` dari identitas, tidak dari URL.
4. Handler:
   - `GET /keuangan/tunggakan?kelas=&jenis=&dari=&sampai=`
   - Akses: `bendahara`, `admin_tu`, `kepala_sekolah` (baca).
5. Tampilkan total sisa tunggakan di bagian atas, dan jumlah siswa yang menunggak.
6. Urutkan default: kelas, lalu nama siswa.
7. Template `web/templates/billing/arrears.html`.

## File yang disentuh
- `internal/billing/arrears.go`, `arrears_test.go`
- `internal/billing/arrears_handler.go`
- `web/templates/billing/arrears.html`

## Kriteria selesai
- [x] Tagihan yang sudah lunas tidak muncul
- [x] Tagihan yang jatuh tempo hari ini tidak muncul (belum terlambat)
- [x] Sisa tunggakan sama dengan `total_amount` dikurangi pembayaran aktif, diuji dengan data contoh
- [x] Pembayaran yang dibatalkan dan belum disetujui tetap menghitung sebagai pembayaran aktif (sesuai T39)
- [x] Filter kelas dan jenis tagihan bekerja
- [x] Data sekolah lain tidak muncul
- [x] Guru mendapat `403`

## Catatan
- Daftar ini adalah bahan untuk laporan tunggakan (T48), jadi query dan angka harus sama persis.
- Untuk ratusan siswa, pastikan index `invoices(school_id, due_date)` dan `payments(invoice_id)` sudah ada.
