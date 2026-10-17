# T30 — Tagihan dan item tagihan

**Fase:** 03 Keuangan
**Dependensi:** T21, T28
**Estimasi:** 1 hari

## Tujuan
Setiap siswa punya tagihan per periode. Tagihan terdiri dari satu atau lebih item, dan total dihitung dari item, bukan diisi manual.

## Langkah
1. Buat migrasi `000015_invoices`:
   - `invoices`: `id uuid PK`, `school_id uuid NOT NULL`, `academic_year_id uuid NOT NULL REFERENCES academic_years(id)`, `enrollment_id uuid NOT NULL REFERENCES enrollments(id)`, `period text NOT NULL` (format `YYYY-MM`), `due_date date NOT NULL`, `total_amount bigint NOT NULL DEFAULT 0`, `status text NOT NULL DEFAULT 'belum_bayar'`, `voided_at timestamptz NULL`, `created_by uuid`, `created_at`, `updated_at`.
   - `invoice_items`: `id uuid PK`, `invoice_id uuid NOT NULL REFERENCES invoices(id)`, `fee_type_id uuid NOT NULL REFERENCES fee_types(id)`, `description text`, `amount bigint NOT NULL`, `CHECK (amount >= 0)`.
2. Constraint:
   - `UNIQUE (enrollment_id, fee_type_id, period)` pada item. Ini mencegah SPP bulan yang sama tercatat dua kali. Jika tabel item tidak punya `period`, tambahkan kolom `period` di item juga, atau cek di service.
   - `CHECK (status IN ('belum_bayar','sebagian','lunas'))`
   - `CHECK (total_amount >= 0)`
3. Status "terlambat" **tidak disimpan**. Status ini dihitung saat ditampilkan: `status <> 'lunas'` dan `due_date < hari ini (Asia/Jakarta)`.
4. Fungsi `CreateInvoice(ctx, tx, enrollmentID, period, items)`:
   - Simpan `invoice` dan `invoice_items` dalam satu transaksi.
   - `total_amount` = jumlah `amount` item, dihitung di service.
   - Nominal item diambil dari default jenis tagihan (T28), tetapi boleh diubah per siswa sebelum disimpan.
5. Tulis `down` migrasi.

## File yang disentuh
- `db/migrations/000015_invoices.up.sql` dan `.down.sql`
- `internal/billing/invoice_repo.go`, `invoice_service.go`, `model.go`

## Kriteria selesai
- [ ] Tagihan dengan dua item menyimpan `total_amount` sesuai jumlah item
- [ ] Membuat item SPP bulan yang sama untuk enrollment yang sama dua kali ditolak
- [ ] Nominal item negatif ditolak
- [ ] Status "terlambat" tidak pernah muncul di kolom `status`
- [ ] Mengubah nominal default di T28 tidak mengubah tagihan yang sudah ada

## Catatan
- Tagihan terikat ke `enrollment_id`, bukan ke `student_id`. Ini agar tagihan kelas tetap tercatat dengan benar saat siswa pindah kelas di tengah tahun.
