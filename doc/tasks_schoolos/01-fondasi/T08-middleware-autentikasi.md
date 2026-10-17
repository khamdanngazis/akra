# T08 — Middleware autentikasi

**Fase:** 01 Fondasi
**Dependensi:** T07
**Estimasi:** 2–3 jam

## Tujuan
Route yang membutuhkan login otomatis redirect ke `/login`. Identitas sekolah dan pengguna selalu diambil dari sesi, bukan dari URL.

## Langkah
1. Buat `internal/auth/middleware.go` dengan fungsi `RequireLogin(next http.Handler) http.Handler`.
2. Di dalam middleware:
   - Cek `user_id` di sesi. Jika kosong, redirect `302` ke `/login?next=<path>`.
   - Parameter `next` hanya menerima path relatif yang dimulai dengan `/`, untuk mencegah open redirect.
3. Buat helper `auth.CurrentUser(ctx) (Identity, bool)` yang membaca `user_id`, `school_id`, dan peran dari sesi.
4. Simpan `Identity` ke `context` di middleware, sehingga handler tidak membaca sesi secara langsung.
5. Pasang middleware di grup route yang membutuhkan login (semua kecuali `/login`, `/healthz`, `/healthz/db`, dan `/static/`).
6. Tambahkan larangan di code review: handler tidak boleh membaca `school_id` dari `r.URL.Query()` atau `chi.URLParam` untuk data sekolah.

## File yang disentuh
- `internal/auth/middleware.go`
- `internal/auth/context.go`
- `cmd/server/main.go` atau `internal/platform/httpserver/routes.go` (pemasangan grup route)

## Kriteria selesai
- [x] Akses `/` tanpa login redirect ke `/login?next=%2F`
- [x] Parameter `next=https://contoh.com` diabaikan dan redirect ke `/`
- [x] Setelah login, pengguna kembali ke halaman `next`
- [x] Uji unit: middleware tanpa sesi menghasilkan `302`, dengan sesi menghasilkan `200` dan `Identity` tersedia di context
- [x] Route `/login`, `/healthz`, dan `/static/*` tidak redirect

## Catatan
- Pemeriksaan peran dikerjakan di T10, bukan di task ini.
