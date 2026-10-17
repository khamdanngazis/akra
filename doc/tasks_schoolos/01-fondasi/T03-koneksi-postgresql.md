# T03 — Koneksi PostgreSQL

**Fase:** 01 Fondasi
**Dependensi:** T02
**Estimasi:** 2–3 jam

## Tujuan
Aplikasi tersambung ke PostgreSQL dengan connection pool, dan gagal dengan pesan jelas jika database tidak tersedia.

## Langkah
1. Pasang `github.com/jackc/pgx/v5` dan `github.com/jackc/pgx/v5/pgxpool`.
2. Buat `internal/platform/database/postgres.go`:
   - Fungsi `Connect(ctx, dsn)` membuat `pgxpool.Pool`.
   - Atur `MaxConns` (default 10) dan `MinConns` (default 2) dari environment `DB_MAX_CONNS`.
   - Lakukan `Ping` dengan timeout 5 detik saat start.
3. Di `main.go`, panggil `Connect` sebelum server dijalankan. Jika gagal, log error dan keluar dengan kode 1.
4. Tambahkan `docker-compose.yml` untuk Postgres lokal:
   - Image `postgres:16`
   - Port `5432`
   - Volume untuk data
   - Variabel `POSTGRES_DB=schoolos`, user dan password untuk lokal saja
5. Buat `.env.example` berisi contoh `DATABASE_URL` untuk lokal (tanpa nilai rahasia).
6. Tambahkan endpoint `GET /healthz/db` yang melakukan `Ping` dan mengembalikan `200` atau `503`.
7. Perbarui `README.md`: cara menjalankan `docker compose up -d`.

## File yang disentuh
- `internal/platform/database/postgres.go`
- `cmd/server/main.go`
- `internal/platform/health/handler.go`
- `docker-compose.yml`
- `.env.example`
- `README.md`

## Kriteria selesai
- [x] Server tersambung ke Postgres lokal saat dijalankan
- [x] Dengan Postgres dimatikan, server tidak start dan pesan error menyebut host/port database
- [x] `GET /healthz/db` mengembalikan `200` saat database hidup, `503` saat mati
- [x] Tidak ada DSN atau password nyata di repositori (dicek dengan pencarian sederhana)

## Catatan
- Jangan menulis SQL di task ini. Skema dibuat di T04 dan T05.
