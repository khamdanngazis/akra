# T69 — Komponen penilaian **[wawancara]**

**Fase:** 07 Nilai
**Dependensi:** T64
**Estimasi:** 1 hari

## Tujuan
Setiap mapel punya komponen penilaian (misalnya tugas, UTS, UAS) dengan bobot per tahun ajaran. Bobot per mapel harus berjumlah 100.

## Syarat mulai
Komponen dan bobot default harus dikonfirmasi dari wawancara guru dan kepala sekolah.

## Langkah
1. Migrasi `grade_components`:
   - `id uuid PK`, `school_id uuid NOT NULL`, `academic_year_id uuid NOT NULL`, `subject_id uuid NOT NULL REFERENCES subjects(id)`, `name text NOT NULL`, `weight_percent smallint NOT NULL`, `sort_order smallint NOT NULL DEFAULT 0`, `is_active boolean NOT NULL DEFAULT true`.
2. Constraint:
   - `CHECK (weight_percent BETWEEN 1 AND 100)`
   - `UNIQUE (subject_id, academic_year_id, name)`
3. Aturan total bobot: dicek di service saat menyimpan dan saat mengaktifkan komponen. Total bobot aktif per mapel harus 100. Database tidak memeriksanya langsung, jadi tambahkan uji.
4. Halaman `GET /nilai/komponen?mapel=` untuk admin dan guru mapel yang ditugaskan (T65). Form menampilkan total bobot secara langsung dengan Alpine.js.
5. Audit: `grade_component.create`, `grade_component.update`.

## File yang disentuh
- `db/migrations/0000XX_grade_components.up.sql` dan `.down.sql`
- `internal/grades/component_service.go`, `component_handler.go`
- `web/templates/grades/components.html`

## Kriteria selesai
- [ ] Bobot 0 atau 101 ditolak
- [ ] Total bobot selain 100 menolak aktivasi
- [ ] Dua komponen dengan nama sama di mapel dan tahun yang sama ditolak
- [ ] Guru tanpa penugasan mapel mendapat `403`

## Catatan
- Komponen dinonaktifkan, tidak dihapus, karena nilai lama masih merujuk ke situ.
