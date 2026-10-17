# T07 — Sesi dengan `scs`

**Fase:** 01 Fondasi
**Dependensi:** T06
**Estimasi:** 3–4 jam

## Tujuan
Login menghasilkan sesi yang disimpan di Postgres. Sesi bertahan setelah reload dan hilang setelah logout.

## Langkah
1. Pasang `github.com/alexedwards/scs/v2` dan `github.com/alexedwards/scs/pgxstore`.
2. Buat migrasi `000004_sessions` sesuai skema yang dibutuhkan `pgxstore` (tabel `sessions` dengan kolom `token`, `data`, `expiry`).
3. Buat `internal/auth/session.go`:
   - Inisialisasi `scs.SessionManager` dengan `Store` dari pgxstore.
   - `Lifetime`: 12 jam.
   - `IdleTimeout`: 2 jam.
   - `Cookie.Name`: `schoolos_session`.
   - `Cookie.HttpOnly`: true.
   - `Cookie.Secure`: true jika `APP_ENV` bukan `development`.
   - `Cookie.SameSite`: `http.SameSiteLaxMode`.
4. Pasang middleware `LoadAndSave` di router.
5. Setelah `Authenticate` berhasil di T06:
   - Panggil `RenewToken` untuk mencegah session fixation.
   - Simpan `user_id`, `school_id`, dan daftar peran ke sesi.
6. Buat handler `POST /logout`:
   - Panggil `Destroy`, lalu redirect ke `/login`.
   - Logout hanya lewat POST, bukan GET.
7. Pastikan sesi tidak menyimpan password atau data sensitif lain.

## File yang disentuh
- `internal/auth/session.go`
- `internal/auth/handler.go`
- `db/migrations/000004_sessions.up.sql` dan `.down.sql`
- `cmd/server/main.go` (pasang middleware)

## Kriteria selesai
- [x] Setelah login, refresh halaman tetap dalam keadaan login
- [x] Setelah logout, membuka halaman yang sebelumnya terlindungi kembali ke `/login`
- [x] Cookie `schoolos_session` memiliki atribut `HttpOnly` dan `SameSite=Lax` (dicek di DevTools)
- [x] Di staging atau production, cookie memiliki atribut `Secure`
- [x] Token sesi berubah setelah login (tidak sama dengan token sebelum login)

## Catatan
- Middleware autentikasi yang memeriksa sesi dikerjakan di T08.
- Pastikan ada pembersihan sesi kedaluwarsa. Cukup `pgxstore` dengan cleanup bawaan; catat interval-nya.
