# T65 — Penugasan guru mapel

**Fase:** 06 Jadwal dan Penugasan
**Dependensi:** T64, T18, T10
**Estimasi:** 1 hari

## Tujuan
Setiap kelas punya guru untuk tiap mapel pada tahun ajaran tertentu. Ini menggantikan aturan sementara di T26 yang hanya mengandalkan wali kelas.

## Langkah
1. Migrasi `teaching_assignments`:
   - `id uuid PK`, `school_id uuid NOT NULL`, `academic_year_id uuid NOT NULL REFERENCES academic_years(id)`, `class_id uuid NOT NULL REFERENCES classes(id)`, `subject_id uuid NOT NULL REFERENCES subjects(id)`, `teacher_id uuid NOT NULL REFERENCES users(id)`, `created_at`.
2. `UNIQUE (class_id, subject_id)`. Satu mapel di satu kelas hanya punya satu guru.
3. Validasi di service: guru harus aktif, berperan `guru`, dan dari sekolah yang sama.
4. Ubah `CanEditClass` di T26: true jika guru adalah wali kelas **atau** punya penugasan mapel di kelas tersebut.
5. Halaman `GET /pengaturan/penugasan?kelas=` dengan form per kelas: satu baris per mapel, pilihan guru.
6. Akses tulis: `admin_tu`. Guru hanya bisa melihat penugasannya sendiri.
7. Audit: `teaching.assign`, `teaching.unassign`.

## File yang disentuh
- `db/migrations/0000XX_teaching_assignments.up.sql` dan `.down.sql`
- `internal/academic/assignment_service.go`, `assignment_handler.go`
- `internal/attendance/access.go` (perubahan `CanEditClass`)
- `web/templates/academic/assignments.html`

## Kriteria selesai
- [ ] Dua guru untuk mapel yang sama di kelas yang sama ditolak database
- [ ] Guru mapel yang punya penugasan bisa mengisi absensi kelas tersebut (uji T26 tetap lulus)
- [ ] Guru tanpa penugasan dan bukan wali kelas mendapat `403` di absensi
- [ ] Penugasan dari sekolah lain ditolak
- [ ] Uji otorisasi T58 dan T59 diperbarui untuk route baru ini

## Catatan
- Penugasan ini bukan jadwal. Jam dan hari diatur di T66.
