# T01 — Inisialisasi repositori

**Fase:** 01 Fondasi
**Dependensi:** tidak ada
**Estimasi:** 1–2 jam

## Tujuan
Repositori Git berisi struktur folder dasar sesuai rancangan bagian 5, dan bisa di-build.

## Langkah
1. Buat repositori Git baru (`schoolos`), branch utama `main`.
2. Buat `go.mod` dengan module path internal, misalnya `schoolos`. Versi Go mengikuti yang terpasang di mesin.
3. Buat folder kosong dengan file `.gitkeep`:
   - `cmd/server/`
   - `internal/auth/`, `internal/school/`, `internal/classes/`, `internal/students/`, `internal/attendance/`, `internal/billing/`, `internal/payments/`, `internal/reports/`, `internal/importer/`, `internal/audit/`, `internal/platform/`
   - `web/templates/`, `web/static/`
   - `db/migrations/`, `db/queries/`
4. Buat `cmd/server/main.go` yang hanya mencetak "schoolos" (placeholder).
5. Buat `.gitignore`: binary, `.env`, `tmp/`, `*.log`, folder build.
6. Buat `README.md` berisi: nama proyek, versi Go, dan cara menjalankan (akan dilengkapi di T02 dan T03).
7. Commit pertama: `chore: init repository structure`.

## File yang disentuh
- `go.mod`, `.gitignore`, `README.md`, `cmd/server/main.go`, folder di atas

## Kriteria selesai
- [x] `go build ./cmd/server` berhasil tanpa error
- [x] `go vet ./...` tidak menampilkan error
- [x] Struktur folder sama dengan rancangan bagian 5
- [x] `.env` tidak ikut ter-commit (dicek dengan `git status`)

## Catatan
- Jangan menambahkan dependensi pihak ketiga di task ini.
