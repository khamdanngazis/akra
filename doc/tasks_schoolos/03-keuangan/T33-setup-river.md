# T33 — Setup River

**Fase:** 03 Keuangan
**Dependensi:** T04
**Estimasi:** 3 jam

## Tujuan
Antrean pekerjaan latar belakang berjalan di Postgres yang sama, tanpa Redis. Dipakai untuk tagihan massal (T32), impor (T55), dan ekspor besar (T49).

## Langkah
1. Pasang `github.com/riverqueue/river` dan `github.com/riverqueue/river/riverdriver/riverpgxv5`.
2. Jalankan migrasi River sesuai dokumentasi library. Migrasi ini dibuat oleh tool River, bukan ditulis manual. Simpan file-nya di `db/migrations` dengan nomor berikutnya agar `golang-migrate` tetap menjadi sumber kebenaran.
3. Buat `internal/platform/jobs/client.go`:
   - Inisialisasi `river.Client` dengan pool pgx yang sama dengan aplikasi.
   - Worker dan client berjalan dalam proses yang sama (sesuai rancangan).
   - Jumlah worker dibaca dari `RIVER_WORKERS` (default 4).
4. Buat satu job contoh `PingJob` dengan argumen `message string`. Job menulis log dan selesai.
5. Di `main.go`: start client setelah database siap, dan hentikan dengan graceful shutdown saat `SIGTERM`.
6. Tambahkan endpoint internal (hanya development) untuk memasukkan `PingJob` dan memeriksa hasilnya.
7. Catat di `README.md` cara menjalankan worker saat development.

## File yang disentuh
- `internal/platform/jobs/client.go`, `ping_job.go`
- `db/migrations/0000XX_river.up.sql` dan `.down.sql` (sesuai output tool River)
- `cmd/server/main.go`

## Kriteria selesai
- [ ] `PingJob` yang dimasukkan selesai dan tercatat di tabel River
- [ ] Shutdown tidak membiarkan job setengah jalan tanpa tanda (job yang dibatalkan dicoba ulang)
- [ ] Migrasi River bisa naik dan turun
- [ ] Jumlah worker mengikuti `RIVER_WORKERS`

## Catatan
- River menggunakan koneksi database tambahan. Pastikan `DB_MAX_CONNS` (T03) cukup untuk aplikasi dan worker bersama.
- Versi library dicek ulang saat task ini dikerjakan, karena API bisa berubah.
