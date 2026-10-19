# T70 — Input nilai

**Fase:** 07 Nilai
**Dependensi:** T69, T65, T21
**Estimasi:** 2 hari

## Tujuan
Guru mapel bisa mengisi nilai per siswa per komponen untuk kelas yang diampunya.

## Langkah
1. Migrasi `grades`:
   - `id uuid PK`, `school_id uuid NOT NULL`, `enrollment_id uuid NOT NULL REFERENCES enrollments(id)`, `component_id uuid NOT NULL REFERENCES grade_components(id)`, `score numeric(5,2) NULL`, `recorded_by uuid NOT NULL`, `updated_at`, `updated_by uuid`.
   - `UNIQUE (enrollment_id, component_id)`
2. Constraint: `CHECK (score IS NULL OR (score >= 0 AND score <= 100))`. `NULL` berarti belum dinilai, bukan nol.
3. Simpan semua nilai kelas dalam satu transaksi, mirip T26.
4. Akses: guru yang punya penugasan mapel di kelas itu (T65). Wali kelas tidak otomatis bisa mengisi mapel yang bukan miliknya.
5. Nilai diubah dengan menyimpan nilai baru. Riwayat perubahan dicatat di `audit_logs` dengan nilai sebelum dan sesudah.
6. Halaman `GET /nilai/input?kelas=&mapel=&komponen=` dengan tabel satu baris per siswa, input angka, dan penanda sel kosong.

## File yang disentuh
- `db/migrations/0000XX_grades.up.sql` dan `.down.sql`
- `internal/grades/repo.go`, `input_service.go`, `input_handler.go`
- `web/templates/grades/input.html`

## Kriteria selesai
- [ ] Nilai 101 atau -1 ditolak
- [ ] Kosong disimpan sebagai `NULL`, bukan 0
- [ ] Guru lain yang tidak ditugaskan mendapat `403`
- [ ] Perubahan nilai menghasilkan baris audit dengan nilai lama dan baru
- [ ] Mengisi 30 siswa untuk satu komponen selesai di bawah 2 menit

## Catatan
- Nilai bisa diimpor dari Excel. Itu masuk T82 dengan mapping.
