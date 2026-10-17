# T23 — Orang tua/wali

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T20
**Estimasi:** 3 jam

## Tujuan
Data orang tua atau wali tersimpan dan terhubung ke siswa. Satu siswa bisa punya lebih dari satu wali, dan satu wali bisa punya lebih dari satu anak.

## Langkah
1. Buat migrasi `000011_guardians`:
   - `guardians`: `id uuid PK`, `school_id uuid NOT NULL REFERENCES schools(id)`, `full_name text NOT NULL`, `phone text NULL`, `email text NULL`, `address text NULL`, `created_at`, `updated_at`.
   - `student_guardians`: `student_id uuid REFERENCES students(id)`, `guardian_id uuid REFERENCES guardians(id)`, `relationship text NOT NULL` (`ayah`, `ibu`, `wali`, lainnya), `is_primary boolean NOT NULL DEFAULT false`, `PRIMARY KEY (student_id, guardian_id)`.
2. Unique partial index: satu penanggung jawab utama per siswa.
   ```sql
   CREATE UNIQUE INDEX student_one_primary
     ON student_guardians(student_id) WHERE is_primary;
   ```
3. Constraint `CHECK (relationship IN ('ayah','ibu','wali','lainnya'))`.
4. Normalisasi nomor telepon di service ke format `08xxxx` atau `+62xxxx`. Simpan dalam satu format.
5. Tulis repository: `Create`, `LinkToStudent`, `UnlinkFromStudent`, `ListByStudent`, `ListByGuardian`.
6. `ListByStudent` dan `ListByGuardian` wajib memfilter `school_id`.
7. Tulis `down` migrasi.

## File yang disentuh
- `db/migrations/000011_guardians.up.sql` dan `.down.sql`
- `internal/students/guardian_repo.go`
- `internal/students/guardian_service.go`

## Kriteria selesai
- [x] Satu siswa dengan dua wali tersimpan dan keduanya tampil
- [x] Satu wali dengan dua anak tersimpan dan keduanya tampil
- [x] Dua penanggung jawab utama untuk satu siswa ditolak database
- [x] Nomor `0812-3456-7890` dan `+62 812 3456 7890` tersimpan dengan format yang sama
- [x] Wali sekolah lain tidak muncul di `ListByStudent`

## Catatan
- Email wali bersifat opsional di tahap ini, tetapi diperlukan untuk akun portal di T24.
- Pilihan `relationship` bisa diperluas setelah wawancara dengan sekolah.
