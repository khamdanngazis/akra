# T89 — Periode dan gelombang pendaftaran **[keputusan]**

**Fase:** 10 Pendaftaran
**Dependensi:** T16 (tahun ajaran)
**Estimasi:** 1 hari

## Tujuan
Sekolah bisa membuka pendaftaran untuk tahun ajaran tertentu, dengan satu atau beberapa gelombang.

## Syarat mulai
Keputusan apakah ada jalur atau seleksi. Jika ada, kolom `track` ditambah di task ini dan alur seleksi menjadi task terpisah.

## Langkah
1. Migrasi `admission_periods`:
   - `id`, `school_id`, `academic_year_id`, `name` (mis. "Gelombang 1"), `target_grade` (tingkat tujuan), `quota int NULL`, `opens_at`, `closes_at`, `fee_amount` (pakai tipe uang yang sudah ada, `0` = gratis), `required_docs text[]` (`kk`, `akta`, `ijazah`, `foto`, `kip`), `status` (`draf`, `buka`, `tutup`), `created_by`, timestamps.
   - `CHECK (closes_at > opens_at)`.
2. Pendaftaran dianggap terbuka jika `status = 'buka'` dan sekarang di antara `opens_at` dan `closes_at` (pakai `clock`).
3. Halaman `GET/POST /pendaftaran/periode`. Akses: `admin_tu`, `kepala_sekolah`.
4. Periode yang sudah punya pendaftar tidak bisa dihapus, hanya ditutup.

## File yang disentuh
- `db/migrations/0000XX_admission_periods.up.sql` dan `.down.sql`
- `internal/admission/period.go`, `period_test.go`, `period_handler.go`
- `web/templates/admission/periods.html`

## Kriteria selesai
- [ ] Periode di luar rentang tanggal dianggap tutup walau status `buka`
- [ ] Periode dengan pendaftar tidak bisa dihapus
- [ ] Periode sekolah lain tidak terlihat (T58)
