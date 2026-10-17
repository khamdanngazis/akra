# T02 — Server HTTP dasar

**Fase:** 01 Fondasi
**Dependensi:** T01
**Estimasi:** 2–3 jam

## Tujuan
Server HTTP berjalan dengan router `chi`, membaca konfigurasi dari environment, dan punya endpoint health check.

## Langkah
1. Pasang `github.com/go-chi/chi/v5`.
2. Buat paket `internal/platform/config`:
   - Struct `Config` berisi `Port`, `DatabaseURL`, `AppEnv` (`development`, `staging`, `production`).
   - Fungsi `Load()` membaca environment variable: `PORT` (default 8080), `DATABASE_URL` (wajib), `APP_ENV` (default `development`).
   - Jika `DATABASE_URL` kosong, kembalikan error yang menyebut nama variabelnya.
3. Buat paket `internal/platform/httpserver`:
   - Fungsi `New(cfg)` yang mengembalikan `*http.Server` dengan `ReadTimeout`, `WriteTimeout`, dan `IdleTimeout` yang masuk akal (misalnya 10s, 30s, 60s).
4. Di `cmd/server/main.go`:
   - Muat config, buat server, jalankan.
   - Tangani `SIGINT` dan `SIGTERM` dan lakukan graceful shutdown dengan timeout 10 detik.
5. Tambahkan route `GET /healthz` yang mengembalikan `200` dengan body `ok`.
6. Untuk `/healthz`, tulis handler di `internal/platform/health`.
7. Perbarui `README.md`: cara menjalankan dengan `DATABASE_URL=... go run ./cmd/server`.

## File yang disentuh
- `internal/platform/config/config.go`
- `internal/platform/httpserver/server.go`
- `internal/platform/health/handler.go`
- `cmd/server/main.go`
- `README.md`

## Kriteria selesai
- [x] `curl -i localhost:8080/healthz` mengembalikan `200 OK` dengan body `ok`
- [x] Menjalankan tanpa `DATABASE_URL` berhenti dengan pesan error yang jelas
- [x] `Ctrl+C` menghentikan server tanpa panic
- [x] Ada uji unit untuk `config.Load()` pada kasus: variabel lengkap, variabel wajib kosong, nilai default

## Catatan
- Pada tahap ini koneksi database belum dibuat. Itu dikerjakan di T03.
