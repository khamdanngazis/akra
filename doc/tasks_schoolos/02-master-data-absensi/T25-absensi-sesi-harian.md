# T25 — Absensi: sesi harian

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T21
**Estimasi:** 3 jam

## Tujuan
Setiap kelas punya satu sesi absensi per tanggal, dan setiap siswa aktif di kelas itu punya satu catatan status.

## Langkah
1. Buat migrasi `000013_attendance`:
   - `attendance_sessions`: `id uuid PK`, `school_id uuid NOT NULL`, `academic_year_id uuid NOT NULL REFERENCES academic_years(id)`, `class_id uuid NOT NULL REFERENCES classes(id)`, `session_date date NOT NULL`, `created_by uuid NOT NULL REFERENCES users(id)`, `created_at`, `updated_at`.
   - `attendance_records`: `session_id uuid REFERENCES attendance_sessions(id)`, `enrollment_id uuid REFERENCES enrollments(id)`, `status text NOT NULL`, `note text NULL`, `updated_by uuid`, `updated_at`, `PRIMARY KEY (session_id, enrollment_id)`.
2. Constraint:
   - `UNIQUE (class_id, session_date)` pada `attendance_sessions`
   - `CHECK (status IN ('hadir','sakit','izin','alpa'))` pada `attendance_records`
3. Index `(school_id, session_date)` dan `(enrollment_id)`.
4. Fungsi `GetOrCreateSession(ctx, schoolID, classID, date, userID)`:
   - Gunakan `INSERT ... ON CONFLICT (class_id, session_date) DO NOTHING`, lalu `SELECT`. Ini mencegah dua guru membuat sesi ganda pada waktu bersamaan.
5. Fungsi `EnsureRecords(ctx, tx, sessionID)`: buat baris `attendance_records` untuk setiap enrollment aktif di kelas dengan status default `hadir`.
6. Tulis `down` migrasi.

## File yang disentuh
- `db/migrations/000013_attendance.up.sql` dan `.down.sql`
- `internal/attendance/repo.go`
- `internal/attendance/service.go`

## Kriteria selesai
- [x] Dua sesi untuk kelas dan tanggal yang sama ditolak database
- [x] Dua permintaan `GetOrCreateSession` bersamaan menghasilkan satu sesi (diuji dengan dua goroutine)
- [x] Status selain empat nilai yang disepakati ditolak
- [x] Siswa yang pindah kelas di tengah periode tidak masuk ke sesi kelas barunya untuk tanggal sebelum pindah (dicek lewat `enrollments.start_date`)

## Catatan
- Tanggal sesi memakai tanggal lokal Asia/Jakarta, bukan UTC.
- Absensi siswa yang sudah dibuat tidak dihapus. Koreksi dilakukan dengan mengubah status.
