# T34 — Pembayaran

**Fase:** 03 Keuangan
**Dependensi:** T30
**Estimasi:** 1 hari

## Tujuan
Pembayaran tercatat sebagai baris yang tidak pernah diubah atau dihapus. Pembatalan dilakukan di T38 dengan baris terpisah.

## Langkah
1. Buat migrasi `000016_payments`:
   - `id uuid PK`, `school_id uuid NOT NULL`, `invoice_id uuid NOT NULL REFERENCES invoices(id)`, `amount bigint NOT NULL`, `paid_on date NOT NULL`, `method text NOT NULL` (`tunai`, `transfer`), `bank_account text NULL` (rekening tujuan, wajib jika transfer), `reference text NULL` (nomor referensi transfer), `note text NULL`, `receipt_number text NULL`, `recorded_by uuid NOT NULL REFERENCES users(id)`, `created_at timestamptz NOT NULL DEFAULT now()`.
2. Constraint:
   - `CHECK (amount > 0)`
   - `CHECK (method IN ('tunai','transfer'))`
   - `CHECK (method = 'tunai' OR bank_account IS NOT NULL)`
3. Trigger atau aturan database yang menolak `UPDATE` dan `DELETE` pada `payments` untuk user aplikasi. Sama dengan pola di T15.
4. Repository hanya menyediakan `Insert` dan `ListByInvoice`. Tidak ada fungsi `Update` atau `Delete` untuk nominal.
5. Di service `RecordPayment`:
   - Ambil total pembayaran aktif (belum dibatalkan) dan total tagihan.
   - Jika `amount > sisa`, tolak dengan `ErrOverpayment`. Ini sejalan dengan T31.
   - Simpan dalam transaksi bersama pembaruan status (T35).
6. Tulis `down` migrasi.

## File yang disentuh
- `db/migrations/000016_payments.up.sql` dan `.down.sql`
- `internal/payments/repo.go`, `service.go`, `model.go`

## Kriteria selesai
- [x] Pembayaran dengan `amount = 0` atau negatif ditolak
- [x] Pembayaran transfer tanpa `bank_account` ditolak database
- [x] `UPDATE payments SET amount = ...` dari user aplikasi ditolak database (diuji dengan `psql`)
- [x] `DELETE FROM payments` dari user aplikasi ditolak database
- [x] Pembayaran melebihi sisa tagihan ditolak service

## Catatan
- Kolom `receipt_number` diisi saat pembayaran dicatat, dalam transaksi yang sama, lewat `NextReceiptNumber` dari T41. Migrasi `receipt_counters` (T41) harus sudah ada sebelum T35 dijalankan, jadi urutkan migrasinya di awal fase 03.
- Selisih transfer (misalnya transfer kurang Rp 500) belum punya aturan. Ini masuk keputusan terbuka di T35.
