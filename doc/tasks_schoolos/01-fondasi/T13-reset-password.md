# T13 — Reset password

**Fase:** 01 Fondasi
**Dependensi:** T06
**Estimasi:** 1 hari

## Tujuan
Pengguna yang lupa password bisa meminta tautan reset. Tautan hanya berlaku 30 menit dan hanya sekali pakai.

## Langkah
1. Buat migrasi `000005_password_reset_tokens`:
   - `id uuid PK`, `user_id uuid NOT NULL REFERENCES users(id)`, `token_hash bytea NOT NULL UNIQUE`, `expires_at timestamptz NOT NULL`, `used_at timestamptz NULL`, `created_at`.
   - Index `(user_id)`.
2. Buat `internal/auth/reset.go`:
   - `RequestReset(ctx, email)`:
     - Jika email ada dan aktif, buat token acak 32 byte, simpan SHA-256 dari token-nya, dan kirim tautan.
     - Jika email tidak ada, tetap kembalikan pesan sukses yang sama (jangan membocorkan email terdaftar).
   - `ResetPassword(ctx, token, newPassword)`:
     - Cari berdasarkan hash, pastikan belum kedaluwarsa dan `used_at` masih NULL, lalu update password dan set `used_at` dalam satu transaksi.
3. Pengiriman tautan:
   - Di `development` dan `staging`, tulis tautan ke log (dengan peringatan jelas di log).
   - Di `production`, panggil interface `Mailer` yang dibuat di task ini dengan implementasi kosong yang mengembalikan error sampai layanan email dipilih.
4. Buat halaman `GET /lupa-password`, `POST /lupa-password`, `GET /reset-password?token=...`, dan `POST /reset-password`.
5. Setelah reset berhasil, hapus semua sesi aktif milik pengguna tersebut.
6. Validasi password baru: minimal 8 karakter.

## File yang disentuh
- `db/migrations/000005_password_reset_tokens.up.sql` dan `.down.sql`
- `internal/auth/reset.go`, `internal/auth/mailer.go`, `internal/auth/handler.go`
- `web/templates/auth/lupa_password.html`, `web/templates/auth/reset_password.html`

## Kriteria selesai
- [ ] Email tidak terdaftar dan terdaftar menghasilkan pesan sukses yang sama
- [ ] Tautan yang sudah dipakai ditolak
- [ ] Tautan setelah 30 menit ditolak
- [ ] Token di database tersimpan dalam bentuk hash, bukan plaintext
- [ ] Setelah reset, login dengan password lama gagal dan password baru berhasil
- [ ] Sesi aktif pengguna dihapus setelah reset

## Catatan
- Gunakan `crypto/rand` untuk token. Jangan `math/rand`.
- Layanan email yang dipakai belum dipilih. Keputusan ini masuk daftar keputusan terbuka.
