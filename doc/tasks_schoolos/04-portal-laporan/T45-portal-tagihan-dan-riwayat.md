# T45 — Portal: tagihan dan riwayat pembayaran

**Fase:** 04 Portal dan Laporan
**Dependensi:** T44, T39
**Estimasi:** 1 hari

## Tujuan
Orang tua melihat tagihan anaknya, status, dan riwayat pembayaran. Pembayaran yang dibatalkan tetap terlihat dengan keterangannya.

## Langkah
1. Buat `internal/portal/billing_service.go`:
   - `ListInvoices(ctx, guardianID, studentID)`: semua tagihan dari enrollment siswa, dengan total, sisa, status, dan status terlambat (hasil `IsOverdue` dari T31).
   - `ListPayments(ctx, guardianID, studentID)`: pembayaran aktif dan pembayaran yang dibatalkan, dengan kolom `voided` dan alasan pembatalan jika sudah disetujui.
   - Pertama panggil cek kepemilikan dari T44.
2. Pembayaran yang belum disetujui pembatalannya tetap tampil sebagai pembayaran biasa. Status "menunggu pembatalan" tidak ditampilkan ke orang tua.
3. Nominal pembayaran dan tagihan diformat sebagai rupiah (`Rp 150.000`).
4. Handler `GET /portal/siswa/{id}/tagihan`.
5. Template `web/templates/portal/billing.html`:
   - Bagian atas: total sisa tagihan
   - Tabel tagihan dengan status dan label "Terlambat" jika berlaku
   - Tabel riwayat pembayaran dengan tanggal, nominal, metode, dan nomor kuitansi
6. Tautan unduh kuitansi di setiap pembayaran (dikerjakan di T46).

## File yang disentuh
- `internal/portal/billing_service.go`, `handler.go`
- `web/templates/portal/billing.html`

## Kriteria selesai
- [ ] Angka total dan sisa di portal sama dengan `ListArrears` (T42) dan rekap keuangan untuk siswa yang sama
- [ ] Pembayaran yang dibatalkan tampil dengan keterangan dibatalkan
- [ ] Label "Terlambat" muncul hanya untuk tagihan yang lewat jatuh tempo dan belum lunas
- [ ] Orang tua tidak bisa membuka tagihan siswa lain (`404`)
- [ ] Bukti transfer tidak ditampilkan dan tidak ada tautannya

## Catatan
- Orang tua tidak melihat catatan internal pembatalan atau nama bendahara yang mencatat pembayaran.
