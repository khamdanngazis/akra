# T64 — Mata pelajaran

**Fase:** 06 Jadwal dan Penugasan
**Dependensi:** T05, T10
**Estimasi:** 3 jam

## Tujuan
Sekolah punya daftar mata pelajaran yang dipakai di penugasan guru, jadwal, dan nilai.

## Langkah
1. Migrasi `subjects`: `id uuid PK`, `school_id uuid NOT NULL REFERENCES schools(id)`, `name text NOT NULL`, `code text NULL`, `is_active boolean NOT NULL DEFAULT true`, `created_at`, `updated_at`.
2. `UNIQUE (school_id, name)`. Kode mapel, jika diisi, juga unik per sekolah.
3. Repository: `Create`, `Update`, `ListActive(schoolID)`, `GetByID(schoolID, id)`.
4. Halaman `GET /pengaturan/mapel`, `POST /pengaturan/mapel`, `POST /pengaturan/mapel/{id}`. Akses: `admin_tu`.
5. Mapel dinonaktifkan, tidak dihapus.
6. Audit: `subject.create`, `subject.update`, `subject.deactivate`.
7. `down` migrasi.

## File yang disentuh
- `db/migrations/0000XX_subjects.up.sql` dan `.down.sql`
- `internal/academic/subject_repo.go`, `subject_service.go`, `subject_handler.go`
- `web/templates/academic/subjects.html`

## Kriteria selesai
- [ ] Dua mapel dengan nama sama di sekolah yang sama ditolak
- [ ] Mapel nonaktif tidak muncul di pilihan penugasan dan jadwal
- [ ] Guru mendapat `403`
- [ ] Mapel sekolah lain tidak terlihat

## Catatan
- Daftar awal mapel bisa diimpor dari Excel memakai mekanisme T81. Itu opsional.
