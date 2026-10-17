# T06 — Login dan hash password

**Fase:** 01 Fondasi
**Dependensi:** T05
**Estimasi:** 1 hari

## Tujuan
Pengguna bisa login dengan email dan password. Password disimpan dengan `argon2id`. Percobaan login berlebihan diblokir.

## Langkah
1. Pasang `github.com/alexedwards/argon2id`.
2. Buat `internal/auth/password.go`:
   - `HashPassword(plain) (string, error)`
   - `VerifyPassword(plain, hash) (bool, error)`
   - Parameter argon2id dibaca dari config, dengan default yang disepakati tim.
3. Buat `internal/auth/service.go`:
   - `Authenticate(ctx, email, password) (User, error)`
   - Normalisasi email: `strings.ToLower(strings.TrimSpace(email))`
   - Jika email tidak ditemukan, tetap jalankan `VerifyPassword` dengan hash dummy agar waktu respons sama.
   - Kembalikan satu error generik `ErrInvalidCredentials` untuk email salah maupun password salah.
4. Buat migrasi `000003_login_attempts`:
   - Tabel `login_attempts`: `id bigserial`, `email_normalized text`, `ip inet`, `success boolean`, `attempted_at timestamptz`.
   - Index `(email_normalized, attempted_at)` dan `(ip, attempted_at)`.
5. Buat fungsi `IsLocked(ctx, email, ip)`:
   - Lebih dari 5 kegagalan dalam 15 menit untuk email atau IP yang sama, kembalikan `ErrTooManyAttempts`.
6. Buat handler `GET /login` (form) dan `POST /login`:
   - Setelah sukses, panggil `scs` (T07) untuk membuat sesi. Untuk task ini cukup redirect ke `/` dan tandai sebagai TODO sampai T07 selesai.
   - Pesan error tampil sama untuk semua kegagalan kredensial.
7. Pesan error untuk `ErrTooManyAttempts` menyebut "coba lagi beberapa menit lagi", tanpa detail teknis.

## File yang disentuh
- `internal/auth/password.go`, `internal/auth/service.go`, `internal/auth/handler.go`
- `db/migrations/000003_login_attempts.up.sql` dan `.down.sql`

## Kriteria selesai
- [x] Login dengan password benar berhasil (validasi di service)
- [x] Email salah dan password salah menghasilkan error yang sama persis
- [x] Hash di database berformat argon2id (`$argon2id$...`), bukan plaintext
- [x] Setelah 5 kegagalan dalam 15 menit, percobaan ke-6 ditolak meski password benar
- [x] Uji unit: hash lalu verify berhasil, verify dengan password salah gagal, email dengan huruf besar tetap cocok

## Catatan
- Jangan pernah mencatat password, bahkan dalam log error.
- Email yang tidak terdaftar tidak boleh terlihat dari perbedaan waktu respons secara jelas. Cukup pastikan verifikasi hash dummy dijalankan.
