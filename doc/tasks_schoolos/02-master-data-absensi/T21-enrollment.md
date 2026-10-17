# T21 — Enrollment

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T18, T20
**Estimasi:** 3 jam

## Tujuan
Penempatan siswa di kelas pada tahun ajaran tertentu tercatat sebagai riwayat. Satu siswa hanya aktif di satu kelas per tahun ajaran.

## Langkah
1. Buat migrasi `000010_enrollments`:
   - `id uuid PK`, `school_id uuid NOT NULL REFERENCES schools(id)`, `student_id uuid NOT NULL REFERENCES students(id)`, `class_id uuid NOT NULL REFERENCES classes(id)`, `academic_year_id uuid NOT NULL REFERENCES academic_years(id)`, `start_date date NOT NULL`, `end_date date NULL`, `created_at`.
2. Unique partial index: satu enrollment aktif per siswa per tahun ajaran:
   ```sql
   CREATE UNIQUE INDEX enrollments_one_active
     ON enrollments(student_id, academic_year_id) WHERE end_date IS NULL;
   ```
3. Index `(class_id)` dan `(school_id, academic_year_id)`.
4. Fungsi `Place(ctx, tx, schoolID, studentID, classID, yearID, date)`:
   - Dalam satu transaksi: tutup enrollment aktif siswa di tahun itu (`end_date = date - 1 hari`), lalu buat enrollment baru.
   - Pastikan `class_id` dan `academic_year_id` milik sekolah yang sama.
5. Tulis `down` migrasi.

## File yang disentuh
- `db/migrations/000010_enrollments.up.sql` dan `.down.sql`
- `internal/students/enrollment_repo.go`
- `internal/students/enrollment_service.go`

## Kriteria selesai
- [ ] Dua enrollment aktif untuk siswa dan tahun ajaran yang sama ditolak database
- [ ] Memindahkan siswa ke kelas lain menutup enrollment lama dan membuat yang baru dalam satu transaksi
- [ ] Jika salah satu langkah pemindahan gagal, tidak ada enrollment yang setengah jadi (diuji dengan transaksi yang sengaja gagal)
- [ ] Enrollment ke kelas sekolah lain ditolak
- [ ] Riwayat enrollment lama tetap ada setelah pemindahan

## Catatan
- Tanggal `start_date` dan `end_date` menjadi dasar rekap absensi per periode. Gunakan zona waktu Asia/Jakarta saat menentukan "hari ini" di aplikasi.
