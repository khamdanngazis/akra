# T37 — Akses bukti transfer

**Fase:** 03 Keuangan
**Dependensi:** T36, T10
**Estimasi:** 3 jam

## Tujuan
Bukti transfer hanya bisa dibuka oleh peran yang berwenang, lewat URL sementara yang kedaluwarsa dalam 5 menit.

## Langkah
1. Buat handler `GET /keuangan/bukti/{proofID}`:
   - Pasang `RequireRole(RoleBendahara, RoleAdminTU, RoleKepalaSekolah)`.
   - Ambil `payment_proofs` dengan join ke `payments` dan filter `school_id` dari identitas.
   - Jika tidak ditemukan atau milik sekolah lain, kembalikan `404`.
   - Jika ditemukan, buat presigned URL dengan TTL 5 menit lalu redirect `302` ke URL tersebut.
2. Jangan menyimpan presigned URL di database atau di template yang di-cache.
3. Header response di URL presigned: `Content-Disposition: inline` untuk gambar, `attachment` untuk PDF.
4. Audit: `payment.proof_view`, dengan `entity_id` = id bukti. Catat setiap akses, karena data ini sensitif.
5. Orang tua tidak punya route ke bukti transfer. Tidak ada tombol di portal (T45 hanya menampilkan status pembayaran).

## File yang disentuh
- `internal/payments/proof_handler.go` (route akses)
- `internal/platform/storage/r2.go` (fungsi presign, sudah dibuat di T36)

## Kriteria selesai
- [ ] Bendahara membuka bukti dan mendapat redirect ke R2
- [ ] Orang tua yang mencoba route yang sama mendapat `403`
- [ ] Guru mendapat `403`
- [ ] Bendahara sekolah lain mendapat `404` untuk id bukti sekolah ini
- [ ] URL yang dibuka setelah 5 menit mengembalikan error dari R2 (diuji dengan menunggu atau mengubah TTL di uji)
- [ ] Setiap akses tercatat di audit log

## Catatan
- Presigned URL yang sudah dibagikan tetap berlaku sampai kedaluwarsa. Itu risiko yang diterima untuk 5 menit.
