# T40 — Audit untuk keuangan

**Fase:** 03 Keuangan
**Dependensi:** T15, T34, T38
**Estimasi:** 3 jam

## Tujuan
Setiap aksi keuangan tercatat di `audit_logs` dengan konsisten, sehingga bisa ditelusuri siapa mengubah apa dan kapan.

## Langkah
1. Daftar aksi keuangan yang wajib diaudit:
   - `fee_type.create`, `fee_type.update`, `fee_type.deactivate`, `fee_type.activate` (T29)
   - `billing.bulk_create` (T32, satu baris per job)
   - `payment.create` (T35)
   - `payment.proof_upload` (T36), `payment.proof_view` (T37)
   - `payment.void_request`, `payment.void_approve`, `payment.void_reject` (T38)
2. Pastikan setiap pemanggilan `audit.Record` (T15) berada di dalam transaksi yang sama dengan perubahan datanya.
3. Untuk `before` dan `after`, simpan hanya field yang relevan (misalnya nominal, status, periode). Jangan simpan seluruh baris atau data pribadi siswa.
4. Tambahkan halaman sederhana `GET /keuangan/audit?entity=...` untuk kepala sekolah dan admin, dengan filter tanggal dan jenis aksi.
5. Buat uji integrasi: setiap aksi di daftar menghasilkan tepat satu baris audit.

## File yang disentuh
- Semua handler dan service keuangan (perubahan kecil di tiap file)
- `internal/audit/actions.go` (daftar konstanta aksi)
- `internal/audit/handler.go` (halaman lihat audit)

## Kriteria selesai
- [x] Uji integrasi untuk setiap aksi di daftar menghasilkan tepat satu baris audit
- [x] Aksi yang gagal (misalnya transaksi rollback) tidak menghasilkan baris audit
- [x] Field `before` dan `after` tidak berisi password, token, atau alamat lengkap siswa
- [x] Halaman audit bisa difilter berdasarkan tanggal dan jenis aksi
- [x] Guru dan bendahara tidak bisa membuka halaman audit (`403`)

## Catatan
- Tujuan audit adalah bisa menjawab pertanyaan "siapa membatalkan pembayaran X". Pertanyaan itu harus bisa dijawab dari halaman ini tanpa akses database.
