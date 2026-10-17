# T04 — Migrasi database

**Fase:** 01 Fondasi
**Dependensi:** T03
**Estimasi:** 2 jam

## Tujuan
Perubahan skema database dikelola dengan migrasi yang bisa naik dan turun, dijalankan lewat perintah yang sama di semua lingkungan.

## Langkah
1. Pasang CLI `golang-migrate`. Catat cara pemasangannya di `README.md` (bukan dependensi `go.mod`).
2. Buat `db/migrations/` dengan format penamaan `000001_nama.up.sql` dan `000001_nama.down.sql`.
3. Buat `Makefile` dengan target:
   - `migrate-up`: `migrate -path db/migrations -database "$(DATABASE_URL)" up`
   - `migrate-down`: `... down 1`
   - `migrate-create name=...`: membuat pasangan file baru
4. Buat migrasi `000001_init` yang hanya berisi `CREATE EXTENSION IF NOT EXISTS pgcrypto;` (untuk `gen_random_uuid()`), dengan `down` yang sesuai.
5. Jalankan `migrate-up`, lalu `migrate-down`, lalu `migrate-up` lagi.
6. Pastikan `DATABASE_URL` diambil dari environment, tidak tertulis di Makefile.

## File yang disentuh
- `Makefile`
- `db/migrations/000001_init.up.sql`
- `db/migrations/000001_init.down.sql`
- `README.md`

## Kriteria selesai
- [ ] `make migrate-up` berhasil di database kosong
- [ ] `make migrate-down` berhasil dan mengembalikan kondisi awal
- [ ] Urutan up-down-up tidak menghasilkan error
- [ ] Target `migrate-create` membuat dua file dengan nomor berikutnya

## Catatan
- Aturan tim: setiap perubahan skema harus punya file `down`. Tambahkan ke `README.md`.
- Migrasi tidak boleh diedit setelah sudah dijalankan di staging atau production. Buat migrasi baru.
