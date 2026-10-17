# T09 — CSRF dengan `nosurf`

**Fase:** 01 Fondasi
**Dependensi:** T07
**Estimasi:** 2–3 jam

## Tujuan
Semua request yang mengubah data (POST, PUT, PATCH, DELETE) wajib membawa token CSRF yang valid.

## Langkah
1. Pasang `github.com/justinas/nosurf`.
2. Buat `internal/platform/csrf/csrf.go`:
   - Bungkus router dengan `nosurf.New(handler)`.
   - Cookie CSRF: `HttpOnly`, `SameSite=Lax`, `Secure` di non-development, `Path=/`.
   - Pengecualian: hanya `/healthz` dan `/healthz/db` (GET, tidak mengubah data).
   - Gagal CSRF mengembalikan `403` dengan halaman sederhana.
3. Buat helper template `{{ csrfField }}` yang menghasilkan `<input type="hidden" name="csrf_token" value="...">`.
4. Tambahkan `{{ csrfField }}` ke form login (T06) dan form logout (T07).
5. Untuk AJAX dari Alpine.js, sediakan `<meta name="csrf-token" content="...">` di layout dan kirim lewat header `X-CSRF-Token`.
6. Pastikan `nosurf` dipasang sebelum `scs` agar token tersedia saat render.

## File yang disentuh
- `internal/platform/csrf/csrf.go`
- `web/templates/layout.html` (meta tag)
- `web/templates/auth/login.html`
- `cmd/server/main.go` atau `routes.go`

## Kriteria selesai
- [ ] POST ke `/login` tanpa token mengembalikan `403`
- [ ] POST dengan token valid diproses
- [ ] POST dengan token dari sesi lain ditolak
- [ ] `GET /healthz` tetap `200`
- [ ] Cookie CSRF memiliki atribut `HttpOnly` dan `SameSite=Lax`

## Catatan
- Token harus diperiksa di setiap route yang mengubah data. Tambahkan uji otomatis yang memeriksa ini (ikut dikerjakan di T58 dan T59 nanti).
