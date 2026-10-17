# T16 — Tahun ajaran

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T05
**Estimasi:** 3 jam

## Tujuan
Setiap sekolah punya daftar tahun ajaran, dan tepat satu tahun ajaran aktif per sekolah pada satu waktu.

## Langkah
1. Buat migrasi `000007_academic_years`:
   - `id uuid PK`, `school_id uuid NOT NULL REFERENCES schools(id)`, `name text NOT NULL` (contoh: `2026/2027`), `start_date date NOT NULL`, `end_date date NOT NULL`, `is_active boolean NOT NULL DEFAULT false`, `created_at`, `updated_at`.
2. Tambahkan constraint `CHECK (end_date > start_date)`.
3. Tambahkan unique constraint `(school_id, name)`.
4. Tambahkan unique partial index untuk status aktif:
   ```sql
   CREATE UNIQUE INDEX academic_years_one_active
     ON academic_years(school_id) WHERE is_active;
   ```
5. Buat fungsi repository `SetActive(ctx, tx, schoolID, yearID)`:
   - Dalam satu transaksi: set `is_active = false` untuk tahun aktif lain di sekolah yang sama, lalu set `is_active = true` untuk tahun yang dipilih.
   - Urutan ini penting agar tidak melanggar unique index di tengah transaksi.
6. Tulis `down` migrasi yang menghapus index dan tabel.

## File yang disentuh
- `db/migrations/000007_academic_years.up.sql` dan `.down.sql`
- `internal/school/academic_year_repo.go`

## Kriteria selesai
- [x] Dua tahun aktif di sekolah yang sama ditolak database
- [x] Fungsi `SetActive` pada tahun kedua menonaktifkan tahun pertama dalam satu transaksi
- [x] Tahun aktif di sekolah A tidak mempengaruhi sekolah B
- [x] `end_date` sebelum `start_date` ditolak database
- [x] Nama tahun ajaran yang sama di sekolah yang sama ditolak

## Catatan
- Tahun ajaran baru dibuat dengan `is_active = false`. Aktivasi dilakukan eksplisit oleh admin.
- Tidak ada aturan otomatis berdasarkan tanggal. Itu bisa ditambahkan nanti jika diminta sekolah.
