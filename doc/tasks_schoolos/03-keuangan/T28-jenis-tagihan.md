# T28 — Jenis tagihan

**Fase:** 03 Keuangan
**Dependensi:** T05
**Estimasi:** 2–3 jam

## Tujuan
Sekolah bisa mendefinisikan jenis tagihan (SPP, uang pangkal, kegiatan) dengan nominal dan jatuh tempo default. Nominal selalu bilangan bulat rupiah.

## Langkah
1. Buat migrasi `000014_fee_types`:
   - `id uuid PK`, `school_id uuid NOT NULL REFERENCES schools(id)`, `name text NOT NULL`, `default_amount bigint NOT NULL`, `default_due_day smallint NULL` (1–28), `is_active boolean NOT NULL DEFAULT true`, `created_at`, `updated_at`.
2. Constraint:
   - `CHECK (default_amount >= 0)`. Nol diizinkan untuk jenis tagihan gratis, tetapi nominal negatif tidak.
   - `CHECK (default_due_day BETWEEN 1 AND 28)`. Batas 28 menghindari masalah bulan Februari.
   - `UNIQUE (school_id, name)`.
3. Di service, validasi nama tidak kosong dan maksimal 60 karakter.
4. Repository: `Create`, `Update`, `ListActive(schoolID)`, `ListAll(schoolID)`, `GetByID(schoolID, id)`.
5. Nonaktifkan, bukan hapus. Tagihan lama tetap merujuk ke jenis tagihan ini.
6. Tulis `down` migrasi.

## File yang disentuh
- `db/migrations/000014_fee_types.up.sql` dan `.down.sql`
- `internal/billing/fee_type_repo.go`, `fee_type_service.go`, `model.go`

## Kriteria selesai
- [x] Nominal `-1` ditolak database
- [x] Nominal `0` diterima
- [x] Tanggal jatuh tempo `29` ditolak
- [x] Nama sama di sekolah yang sama ditolak
- [x] `ListActive` tidak mengembalikan jenis tagihan nonaktif
- [x] Jenis tagihan sekolah lain tidak muncul

## Catatan
- Nominal default bisa berbeda per kelas (misalnya SPP kelas 1 dan kelas 6). Itu ditangani di T30 lewat nominal per item tagihan, bukan dengan membuat banyak jenis tagihan. Keputusan ini dicatat di sini agar tidak dibahas ulang.
