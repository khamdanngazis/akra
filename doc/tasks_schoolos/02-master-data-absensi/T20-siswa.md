# T20 — Siswa

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T05
**Estimasi:** 2 jam

## Tujuan
Data siswa tersimpan dengan NIS unik per sekolah. Siswa tidak terikat ke kelas di tabel ini; keterikatan kelas ada di `enrollments` (T21).

## Langkah
1. Buat migrasi `000009_students`:
   - `id uuid PK`, `school_id uuid NOT NULL REFERENCES schools(id)`, `nis text NOT NULL`, `full_name text NOT NULL`, `birth_date date NULL`, `gender char(1) NULL` (`L` atau `P`), `status text NOT NULL DEFAULT 'aktif'`, `created_at`, `updated_at`.
2. Constraint:
   - `UNIQUE (school_id, nis)`
   - `CHECK (gender IN ('L','P') OR gender IS NULL)`
   - `CHECK (status IN ('aktif','pindah','lulus','keluar'))`
3. Index `(school_id, full_name)` untuk pencarian nama.
4. Normalisasi NIS: trim spasi di service. NIS disimpan sebagai teks, karena bisa berawalan nol.
5. Tulis repository: `Create`, `GetByID(schoolID, id)`, `Update`, `Search(schoolID, query)`.
6. Tulis `down` migrasi.

## File yang disentuh
- `db/migrations/000009_students.up.sql` dan `.down.sql`
- `internal/students/repo.go`, `model.go`

## Kriteria selesai
- [x] Dua siswa dengan NIS sama di sekolah yang sama ditolak database
- [x] Dua siswa dengan NIS sama di sekolah berbeda diizinkan
- [x] NIS `007` tetap tersimpan sebagai `007`, tidak berubah menjadi `7`
- [x] `gender` selain `L`, `P`, atau NULL ditolak
- [x] `Search` hanya mengembalikan siswa dari sekolah pemanggil

## Catatan
- Status `lulus` dan `pindah` disiapkan sejak awal, walaupun alur kenaikan kelas belum ada di MVP 1.
- Data siswa berisi informasi pribadi. Jangan menampilkannya di log.
