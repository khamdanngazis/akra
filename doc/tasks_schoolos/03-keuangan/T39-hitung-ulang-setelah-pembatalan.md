# T39 — Hitung ulang status setelah pembatalan (unit test)

**Fase:** 03 Keuangan
**Dependensi:** T38, T31
**Estimasi:** 3 jam

## Tujuan
Setelah pembatalan disetujui, status tagihan dihitung ulang hanya dari pembayaran yang masih aktif.

## Langkah
1. Buat fungsi `ActivePaid(payments []Payment, voids map[PaymentID]Void) int64` di `internal/billing/` (paket yang sama dengan `ComputeStatus`).
   - Jumlahkan `amount` hanya untuk pembayaran yang tidak punya void disetujui.
   - Void yang belum disetujui tidak mengurangi total.
2. Fungsi `RecalculateInvoice(ctx, tx, invoiceID)`:
   - Ambil total tagihan, semua pembayaran, dan semua void.
   - Panggil `ActivePaid`, lalu `ComputeStatus`.
   - Update `invoices.status`.
3. Panggil `RecalculateInvoice` setelah:
   - pembayaran dibuat (T35),
   - void disetujui (T38).
4. Buat `status_recalc_test.go`:
   - Tagihan 100.000, dua pembayaran 50.000 → lunas. Void satu → sebagian.
   - Tagihan 100.000, satu pembayaran 100.000, void disetujui → belum bayar.
   - Tagihan 100.000, satu pembayaran 100.000, void diajukan tapi belum disetujui → lunas.
   - Dua void untuk dua pembayaran berbeda → total dihitung dengan benar.

## File yang disentuh
- `internal/billing/active_paid.go`
- `internal/billing/recalc.go`
- `internal/billing/status_recalc_test.go`

## Kriteria selesai
- [x] Semua kasus uji di atas lulus
- [x] Status tagihan berubah dari lunas ke sebagian atau belum bayar setelah void disetujui
- [x] Void yang belum disetujui tidak mengubah status
- [x] Recalculate dijalankan dalam transaksi yang sama dengan aksi pemicunya

## Catatan
- Status "terlambat" tetap dihitung saat tampil (T30), bukan disimpan di sini.
