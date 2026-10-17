# T15 — Tabel audit dasar

**Fase:** 01 Fondasi
**Dependensi:** T05
**Estimasi:** 2–3 jam

## Tujuan
Ada tabel dan fungsi pencatatan audit yang bisa dipakai modul lain. Data audit tidak bisa diubah atau dihapus lewat aplikasi.

## Langkah
1. Buat migrasi `000006_audit_logs`:
   - `id bigserial PK`
   - `school_id uuid NOT NULL REFERENCES schools(id)`
   - `user_id uuid NULL` (NULL untuk aksi sistem)
   - `action text NOT NULL` (contoh: `invoice.create`, `payment.void`)
   - `entity_type text NOT NULL`
   - `entity_id text NOT NULL`
   - `before jsonb NULL`
   - `after jsonb NULL`
   - `ip inet NULL`
   - `created_at timestamptz NOT NULL DEFAULT now()`
2. Index `(school_id, created_at)` dan `(entity_type, entity_id)`.
3. Buat aturan database: `REVOKE UPDATE, DELETE ON audit_logs FROM <user_aplikasi>`. Jika user yang sama dipakai untuk migrasi, catat batasannya di README.
4. Buat `internal/audit/audit.go`:
   - Struct `Entry` dengan field di atas.
   - Fungsi `Record(ctx, tx pgx.Tx, entry Entry) error`. Menerima transaksi agar audit ikut rollback bila aksi utama gagal.
   - Validasi: `action`, `entity_type`, `entity_id`, dan `school_id` wajib diisi.
5. Tambahkan uji integrasi: aksi dengan transaksi yang di-rollback tidak menyisakan baris audit.

## File yang disentuh
- `db/migrations/000006_audit_logs.up.sql` dan `.down.sql`
- `internal/audit/audit.go`
- `internal/audit/audit_test.go`

## Kriteria selesai
- [x] `audit.Record` menyimpan satu baris dengan `before` dan `after` sebagai JSON
- [x] Transaksi yang di-rollback tidak menyisakan baris audit
- [x] Validasi menolak `action` kosong
- [x] Perintah `UPDATE audit_logs` dari user aplikasi ditolak database (diuji manual dengan `psql`)

## Catatan
- Semua aksi keuangan (T40) dan aksi master data penting memakai fungsi ini.
- Jangan mencatat password atau token dalam kolom `before` atau `after`.
