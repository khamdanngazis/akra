# T29 — CRUD jenis tagihan

**Fase:** 03 Keuangan
**Dependensi:** T28, T10
**Estimasi:** 3 jam

## Tujuan
Bendahara dan admin bisa mengelola jenis tagihan dari antarmuka. Jenis tagihan yang nonaktif tidak bisa dipakai untuk tagihan baru.

## Langkah
1. Buat `internal/billing/fee_type_handler.go`:
   - `GET /keuangan/jenis-tagihan`: daftar semua jenis tagihan, dengan status aktif.
   - `GET /keuangan/jenis-tagihan/baru`, `POST /keuangan/jenis-tagihan`
   - `GET /keuangan/jenis-tagihan/{id}/ubah`, `POST /keuangan/jenis-tagihan/{id}`
   - `POST /keuangan/jenis-tagihan/{id}/nonaktifkan` dan `/aktifkan`
2. Pasang `RequireRole(RoleBendahara, RoleAdminTU)`. Kepala sekolah hanya `GET` daftar.
3. Perubahan nominal default tidak mengubah tagihan yang sudah dibuat. Tampilkan catatan ini di form.
4. Audit: `fee_type.create`, `fee_type.update`, `fee_type.deactivate`, `fee_type.activate`.
5. Template: `web/templates/billing/fee_types.html` dan `fee_type_form.html`.

## File yang disentuh
- `internal/billing/fee_type_handler.go`
- `web/templates/billing/fee_types.html`, `fee_type_form.html`

## Kriteria selesai
- [x] Bendahara bisa membuat, mengubah, dan menonaktifkan jenis tagihan
- [x] Jenis tagihan nonaktif tidak muncul di pilihan form tagihan massal (uji di T32)
- [x] Guru mendapat `403` pada semua route ini
- [x] Mengubah nominal default tidak mengubah nilai `invoice_items` yang sudah ada (diuji di T30)
- [x] Setiap perubahan tercatat di audit log

## Catatan
- Tidak ada tombol hapus. Jenis tagihan yang pernah dipakai tidak boleh dihapus.
