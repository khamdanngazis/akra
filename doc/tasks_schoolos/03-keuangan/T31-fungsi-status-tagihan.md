# T31 — Fungsi hitung status tagihan (unit test)

**Fase:** 03 Keuangan
**Dependensi:** T30
**Estimasi:** 3 jam

## Tujuan
Satu fungsi murni menentukan status tagihan dari total dan pembayaran aktif. Semua modul lain (T35, T39, T42, T45) memakai fungsi ini, bukan menghitung sendiri.

## Langkah
1. Buat `internal/billing/status.go`:
   ```go
   func ComputeStatus(total int64, paid int64) (Status, error)
   ```
   - `paid == 0` → `belum_bayar`
   - `0 < paid < total` → `sebagian`
   - `paid == total` → `lunas`
   - `paid > total` → error `ErrOverpayment`
   - `total < 0` atau `paid < 0` → error `ErrNegativeAmount`
2. Fungsi ini tidak boleh membaca database. Ia hanya menerima angka.
3. Buat `internal/billing/status_test.go` dengan tabel kasus uji:
   - total 100.000, paid 0 → belum bayar
   - total 100.000, paid 50.000 → sebagian
   - total 100.000, paid 100.000 → lunas
   - total 100.000, paid 100.001 → error
   - total 0, paid 0 → lunas (tagihan gratis)
   - total -1 → error
4. Tambahkan fungsi `IsOverdue(status, dueDate, today) bool` dengan uji terpisah. Gunakan `today` sebagai parameter, bukan `time.Now()`, agar uji stabil.

## File yang disentuh
- `internal/billing/status.go`
- `internal/billing/status_test.go`

## Kriteria selesai
- [x] Semua kasus uji di atas lulus
- [x] Tidak ada import `database` atau `context` di file `status.go`
- [x] `IsOverdue` mengembalikan `false` untuk tagihan lunas, walaupun jatuh tempo sudah lewat
- [x] `IsOverdue` dengan `today` sama dengan `due_date` mengembalikan `false` (jatuh tempo hari ini belum terlambat)

## Catatan
- Fungsi ini sengaja sederhana. Pembulatan atau selisih transfer ditangani di T35 sebagai aturan terpisah, bukan di sini.
