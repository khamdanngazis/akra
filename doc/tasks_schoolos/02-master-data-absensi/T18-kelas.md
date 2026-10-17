# T18 — Kelas

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T16
**Estimasi:** 2 jam

## Tujuan
Kelas dibuat per tahun ajaran dengan nama, tingkat, dan wali kelas. Setiap kelas terikat ke sekolah.

## Langkah
1. Buat migrasi `000008_classes`:
   - `id uuid PK`, `school_id uuid NOT NULL REFERENCES schools(id)`, `academic_year_id uuid NOT NULL REFERENCES academic_years(id)`, `name text NOT NULL` (contoh: `1A`), `grade_level smallint NOT NULL` (1–12), `homeroom_teacher_id uuid NULL REFERENCES users(id)`, `created_at`, `updated_at`.
2. Constraint:
   - `CHECK (grade_level BETWEEN 1 AND 12)`
   - Unique `(academic_year_id, name)`
3. Index `(school_id, academic_year_id)`.
4. Validasi di database dan service bahwa `academic_year_id` milik `school_id` yang sama. Constraint database tidak bisa langsung mengecek ini, jadi cek di repository dengan query `WHERE school_id = $1 AND id = $2`.
5. Tulis fungsi repository `Create`, `GetByID(schoolID, id)`, dan `ListByYear(schoolID, yearID)`.
6. Tulis `down` migrasi.

## File yang disentuh
- `db/migrations/000008_classes.up.sql` dan `.down.sql`
- `internal/classes/repo.go`
- `internal/classes/model.go`

## Kriteria selesai
- [x] Dua kelas dengan nama sama dalam tahun ajaran yang sama ditolak
- [x] Dua kelas dengan nama sama di tahun ajaran berbeda diizinkan
- [x] `grade_level` 0 atau 13 ditolak database
- [x] Membuat kelas dengan `academic_year_id` milik sekolah lain ditolak di repository
- [x] `ListByYear` hanya mengembalikan kelas dari sekolah pemanggil

## Catatan
- `homeroom_teacher_id` merujuk ke `users`. Validasi bahwa pengguna tersebut memang berperan guru dilakukan di T19.
