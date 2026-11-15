# T89 — Periode, gelombang, dan jalur pendaftaran

**Fase:** 10 Pendaftaran
**Dependensi:** T16 (tahun ajaran)
**Estimasi:** 1,5 hari

## Tujuan
Sekolah bisa membuka pendaftaran untuk tahun ajaran tertentu, dengan satu atau beberapa gelombang, dan setiap gelombang punya satu atau beberapa jalur (mis. reguler, prestasi, saudara kandung) dengan kuota sendiri.

## Keputusan
- Ada jalur dan tes/seleksi (keputusan 2026-11-14). Tes dan perankingan dikerjakan di T93a dan T93b.
- Biaya formulir diatur per periode, termasuk apakah wajib lunas sebelum verifikasi (T94).

## Langkah
1. Migrasi `admission_periods`:
   - `id`, `school_id`, `academic_year_id`, `name` (mis. "Gelombang 1"), `target_grade` (tingkat tujuan), `opens_at`, `closes_at`, `fee_amount` (bigint rupiah, `0` = gratis), `fee_required_before_verify boolean`, `required_docs text[]` (`kk`, `akta`, `ijazah`, `foto`, `kip`), `status` (`draf`, `buka`, `tutup`), `created_by`, timestamps.
   - `CHECK (closes_at > opens_at)`.
2. Migrasi `admission_tracks`: `id`, `school_id`, `period_id`, `name`, `quota int NULL`, `uses_test boolean` (jalur yang memakai tes seleksi), `sort_order`. `UNIQUE (period_id, name)`. Setiap periode minimal punya satu jalur sebelum dibuka.
3. Pendaftaran dianggap terbuka jika `status = 'buka'` dan sekarang di antara `opens_at` dan `closes_at` (pakai `clock`).
4. Halaman `GET/POST /pendaftaran/periode` (daftar, buat, ubah, buka/tutup) dan pengelolaan jalur di halaman periode. Akses: `admin_tu` (ubah), `kepala_sekolah` (baca).
5. Periode atau jalur yang sudah punya pendaftar tidak bisa dihapus, hanya ditutup.

## File yang disentuh
- `db/migrations/0000XX_admission_periods.up.sql` dan `.down.sql`
- `internal/admission/period.go`, `period_test.go`, `period_handler.go`
- `web/templates/admission/periods.html`, `period_form.html`

## Kriteria selesai
- [x] Periode di luar rentang tanggal dianggap tutup walau status `buka`
- [x] Periode tanpa jalur tidak bisa dibuka
- [x] Periode atau jalur dengan pendaftar tidak bisa dihapus
- [x] Periode sekolah lain tidak terlihat (T58)
