# T24 — Akun orang tua

**Fase:** 02 Master Data dan Absensi
**Dependensi:** T23, T10
**Estimasi:** 3 jam

## Tujuan
Wali bisa punya akun login untuk portal. Akun ini hanya bisa mengakses portal, tidak bisa membuka halaman admin.

## Langkah
1. Tambahkan kolom `guardian_id uuid NULL REFERENCES guardians(id)` ke `users` lewat migrasi `000012_user_guardian_link`.
2. Constraint `UNIQUE (guardian_id)` agar satu wali hanya punya satu akun.
3. Buat `internal/students/guardian_account.go`:
   - `CreateAccount(ctx, schoolID, guardianID)`: buat baris `users` dengan `is_active = false` dan password kosong, lalu tandai peran `orang_tua`.
   - Kirim tautan aktivasi lewat `Mailer` (T13). Tautan memakai token sekali pakai dengan masa berlaku 7 hari.
   - Pengguna membuat password lewat halaman aktivasi, bukan dari admin.
4. Admin hanya bisa membuat akun untuk wali yang punya email.
5. Peran `orang_tua` dicek di middleware: route admin menolak dengan `403`, route portal (`/portal/*`) hanya menerima peran ini.
6. Audit untuk `guardian_account.create` dan `guardian_account.activate`.

## File yang disentuh
- `db/migrations/000012_user_guardian_link.up.sql` dan `.down.sql`
- `internal/students/guardian_account.go`
- `internal/auth/activation.go`
- `web/templates/auth/aktivasi.html`

## Kriteria selesai
- [ ] Membuat akun untuk wali yang sama dua kali ditolak
- [ ] Wali tanpa email tidak bisa dibuatkan akun
- [ ] Akun belum aktif tidak bisa login
- [ ] Setelah aktivasi, akun orang tua bisa login dan diarahkan ke `/portal`
- [ ] Akun orang tua mendapat `403` saat membuka `/siswa` atau `/pengaturan/tahun-ajaran`
- [ ] Tautan aktivasi yang sudah dipakai atau kedaluwarsa ditolak

## Catatan
- Orang tua tidak pernah menentukan `school_id` sendiri. Nilai ini diambil dari wali yang ditautkan.
- Di staging, tautan aktivasi ditulis ke log (sama dengan T13).
