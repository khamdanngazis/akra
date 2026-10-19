# T81 — Mapping impor yang bisa dikonfigurasi **[wawancara]**

**Fase:** 08 Laporan dan Impor
**Dependensi:** T54
**Estimasi:** 2 hari

## Tujuan
Sekolah bisa menyimpan pemetaan kolom dari file mereka ke kolom baku SchoolOS. Impor berikutnya memakai mapping yang sama, tanpa mengubah kode.

## Syarat mulai
Format file yang paling sering dipakai sekolah pilot sudah dikumpulkan dari wawancara (contoh file asli, bukan rangkuman).

## Langkah
1. Migrasi `import_mappings`:
   - `id uuid PK`, `school_id uuid NOT NULL`, `name text NOT NULL`, `target text NOT NULL` (`students`, `grades`, `fee_types`), `column_map jsonb NOT NULL`, `created_by uuid NOT NULL`, `created_at`.
   - `UNIQUE (school_id, name)`.
2. Validasi `column_map`: setiap kolom wajib di target harus punya sumber. Kolom sumber harus ada di header file saat dipakai.
3. Saat mapping dibuat, tampilkan header file contoh dan dropdown kolom baku. Ini bisa dibuat dengan Alpine.js.
4. Kolom baku tetap berasal dari T52. Mapping tidak boleh menambah kolom baku baru tanpa perubahan kode.
5. Route `GET /impor/mapping`, `POST /impor/mapping`. Akses: `admin_tu`.

## File yang disentuh
- `db/migrations/0000XX_import_mappings.up.sql` dan `.down.sql`
- `internal/importer/mapping.go`, `mapping_test.go`
- `internal/importer/mapping_handler.go`
- `web/templates/importer/mapping.html`

## Kriteria selesai
- [ ] Mapping dengan kolom wajib yang belum dipetakan ditolak
- [ ] Mapping yang merujuk kolom tidak ada di file ditolak saat impor, dengan pesan yang jelas
- [ ] Mapping sekolah lain tidak terlihat
- [ ] Uji dengan dua format file berbeda menghasilkan baris yang sama setelah dipetakan

## Catatan
- Mapping hanya mengubah nama kolom. Aturan validasi tetap di T54.
