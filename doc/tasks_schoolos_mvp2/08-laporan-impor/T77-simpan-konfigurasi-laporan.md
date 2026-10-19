# T77 — Simpan konfigurasi laporan

**Fase:** 08 Laporan dan Impor
**Dependensi:** T76, T10
**Estimasi:** 1 hari

## Tujuan
Konfigurasi laporan (sumber, kolom, filter, urutan) bisa disimpan dengan nama dan dipakai ulang.

## Langkah
1. Migrasi `report_configs`:
   - `id uuid PK`, `school_id uuid NOT NULL`, `owner_user_id uuid NOT NULL REFERENCES users(id)`, `name text NOT NULL`, `source text NOT NULL`, `columns jsonb NOT NULL`, `filters jsonb NOT NULL DEFAULT '[]'`, `sort jsonb NOT NULL DEFAULT '[]'`, `is_shared boolean NOT NULL DEFAULT false`, `created_at`, `updated_at`.
   - `UNIQUE (school_id, owner_user_id, name)`.
2. Validasi isi JSON lewat fungsi T76 sebelum disimpan. Konfigurasi yang tidak valid tidak bisa disimpan.
3. Konfigurasi pribadi hanya bisa diubah pemiliknya. Konfigurasi bersama bisa dibaca semua pengguna sekolah dengan peran yang sama dengan sumbernya.
4. Routes: `GET /laporan/konfigurasi`, `POST /laporan/konfigurasi`, `POST /laporan/konfigurasi/{id}`, `POST /laporan/konfigurasi/{id}/hapus` (soft, set `is_shared = false` dan tandai terhapus).
5. Audit: `report_config.create`, `report_config.update`, `report_config.share`.

## File yang disentuh
- `db/migrations/0000XX_report_configs.up.sql` dan `.down.sql`
- `internal/reports/builder/config_repo.go`, `config_service.go`, `config_handler.go`

## Kriteria selesai
- [ ] Konfigurasi dengan kolom yang tidak terdaftar tidak bisa disimpan
- [ ] Pengguna lain tidak bisa mengubah konfigurasi pribadi (`403`)
- [ ] Konfigurasi bersama dari sekolah lain tidak terlihat
- [ ] Nama duplikat untuk pemilik yang sama ditolak

## Catatan
- Konfigurasi yang dibagikan ke guru tidak boleh memuat sumber yang di luar hak akses guru. Validasi ini ada di T78.
