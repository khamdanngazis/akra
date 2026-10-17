# T12 — Embed aset

**Fase:** 01 Fondasi
**Dependensi:** T11
**Estimasi:** 2 jam

## Tujuan
Template dan aset statis masuk ke dalam binary, sehingga deploy cukup satu file.

## Langkah
1. Buat `web/embed.go`:
   ```go
   //go:embed templates static
   var FS embed.FS
   ```
2. Ubah `internal/platform/render` agar membaca template dari `web.FS`, bukan dari path disk.
3. Ubah `httpserver` agar melayani `/static/` dari `fs.Sub(web.FS, "static")`.
4. Tambahkan header cache untuk `/static/`: `Cache-Control: public, max-age=86400`.
5. Tambahkan variabel `APP_ENV=development` yang membaca dari disk jika diperlukan, sehingga perubahan template tidak perlu rebuild. Opsional, bisa ditunda jika merepotkan.
6. Build binary, pindahkan ke folder lain, lalu jalankan.

## File yang disentuh
- `web/embed.go`
- `internal/platform/render/render.go`
- `internal/platform/httpserver/server.go`

## Kriteria selesai
- [x] `go build -o schoolos ./cmd/server` menghasilkan satu file
- [x] Binary dijalankan dari folder lain tanpa folder `web/` di sekitarnya dan halaman login tampil
- [x] `/static/css/app.css` mengembalikan `200` dengan header cache
- [x] Ukuran binary dicatat di `README.md` sebagai referensi

## Catatan
- Setiap perubahan template atau CSS sekarang memerlukan rebuild. Itu diterima untuk MVP.
