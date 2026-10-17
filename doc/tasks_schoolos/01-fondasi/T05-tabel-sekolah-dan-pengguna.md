# T05 — Tabel sekolah dan pengguna

**Fase:** 01 Fondasi
**Dependensi:** T04
**Estimasi:** 2–3 jam

## Tujuan
Tabel dasar untuk sekolah, pengguna, dan peran tersedia, dengan `school_id` sebagai dasar isolasi data.

## Langkah
1. Buat migrasi `000002_schools_users_roles`:
   - `schools`: `id uuid PK`, `name text NOT NULL`, `npsn text UNIQUE NULL`, `address text`, `created_at timestamptz`, `updated_at timestamptz`.
   - `users`: `id uuid PK`, `school_id uuid NOT NULL REFERENCES schools(id)`, `email text NOT NULL`, `password_hash text NOT NULL`, `full_name text NOT NULL`, `is_active boolean NOT NULL DEFAULT true`, `created_at`, `updated_at`.
   - Unique `(school_id, email)` pada `users`. Email unik per sekolah, bukan global.
   - `roles`: `id smallint PK`, `code text UNIQUE` (`admin_tu`, `bendahara`, `guru`, `kepala_sekolah`, `orang_tua`), `name text`.
   - `user_roles`: `user_id`, `role_id`, `PRIMARY KEY (user_id, role_id)`.
2. Isi tabel `roles` dengan lima peran di atas di migrasi yang sama (`INSERT`).
3. Tambahkan index `users(school_id)` dan `user_roles(role_id)`.
4. Buat `down` yang menghapus tabel dalam urutan terbalik.
5. Jalankan migrasi dan periksa dengan `\d users` di `psql`.

## File yang disentuh
- `db/migrations/000002_schools_users_roles.up.sql`
- `db/migrations/000002_schools_users_roles.down.sql`

## Kriteria selesai
- [x] Migrasi naik dan turun tanpa error
- [x] Dua pengguna dengan email sama di sekolah berbeda diizinkan
- [x] Dua pengguna dengan email sama di sekolah yang sama ditolak database
- [x] Tabel `roles` berisi lima baris
- [x] Semua kolom `school_id` tidak boleh NULL

## Catatan
- Kolom `email` disimpan dalam huruf kecil. Normalisasi dilakukan di aplikasi (T06).
- Peran orang tua tetap ada di tabel ini, walaupun akun orang tua dibuat di T24.
