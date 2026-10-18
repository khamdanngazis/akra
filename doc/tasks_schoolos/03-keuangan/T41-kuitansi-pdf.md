# T41 — Kuitansi PDF

**Fase:** 03 Keuangan
**Dependensi:** T34
**Estimasi:** 1–2 hari

## Tujuan
Setiap pembayaran yang berhasil bisa dicetak sebagai kuitansi PDF dengan nomor unik dan berurutan per sekolah.

## Langkah
1. Buat migrasi `000019_receipt_sequence`:
   - Tabel `receipt_counters`: `school_id uuid PK`, `last_number bigint NOT NULL DEFAULT 0`.
   - Isi `receipt_number` di `payments` tetap diisi saat pembayaran dibuat (T35), bukan saat PDF dibuat.
2. Fungsi `NextReceiptNumber(ctx, tx, schoolID) (string, error)`:
   - `UPDATE receipt_counters SET last_number = last_number + 1 WHERE school_id = $1 RETURNING last_number`
   - Jika baris belum ada, buat dengan `INSERT ... ON CONFLICT DO NOTHING` lalu ulangi update.
   - Format nomor: `KW/{tahun}/{nomor 6 digit}`, contoh `KW/2026/000001`.
   - Dipanggil di transaksi yang sama dengan `payments` insert. Jika transaksi gagal, nomor ikut dikembalikan.
3. Buat `internal/payments/receipt_pdf.go`:
   - Gunakan `maroto` atau `gofpdf`.
   - Isi: nama sekolah, alamat, nomor kuitansi, tanggal, nama siswa, kelas, nama tagihan dan periode, nominal (dengan format rupiah dan terbilang jika mudah), metode, rekening tujuan (untuk transfer), nama bendahara.
   - Ukuran kertas A5 atau setengah A4.
4. Handler `GET /keuangan/pembayaran/{id}/kuitansi`:
   - Pasang `RequireRole(RoleBendahara, RoleAdminTU)`.
   - Generate PDF saat diminta. Tidak perlu disimpan, karena data sumbernya tetap.
   - `Content-Disposition: attachment; filename="kuitansi-KW-2026-000001.pdf"`.
5. Jika pembayaran dibatalkan, kuitansi tetap bisa dicetak dengan watermark "DIBATALKAN" dan alasan pembatalan.

## File yang disentuh
- `db/migrations/000019_receipt_sequence.up.sql` dan `.down.sql`
- `internal/payments/receipt_pdf.go`, `receipt_handler.go`
- `internal/payments/receipt_number.go`

## Kriteria selesai
- [x] Dua pembayaran berurutan menghasilkan nomor `...000001` dan `...000002`
- [x] Dua pembayaran bersamaan (uji paralel) tidak menghasilkan nomor kembar
- [x] Pembayaran yang di-rollback tidak meninggalkan celah yang tercatat sebagai nomor terpakai (diuji dan dicatat)
- [x] PDF terbuka dengan benar di Chrome dan pembaca PDF umum
- [x] Kuitansi pembayaran yang dibatalkan menampilkan keterangan dibatalkan
- [x] Orang tua bisa mengunduh kuitansi anaknya lewat portal (dikerjakan di T46)

## Catatan
- Celah nomor karena rollback mungkin tetap terjadi pada sequence. Karena nomor diambil lewat counter di transaksi, celah tidak seharusnya muncul. Uji ini penting untuk memastikan.
- Terbilang rupiah ditulis sendiri atau memakai library kecil. Jika terlalu lama, tunda ke catatan dan tampilkan angka saja.
