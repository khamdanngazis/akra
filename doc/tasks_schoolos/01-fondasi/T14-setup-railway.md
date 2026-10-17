# T14 — Setup Railway

**Fase:** 01 Fondasi
**Dependensi:** T12
**Estimasi:** 3–4 jam

## Tujuan
Aplikasi berjalan di Railway dengan database Postgres. Halaman login bisa dibuka dari URL publik.

## Langkah
1. Buat `Dockerfile` multi-stage:
   - Stage build: `golang:<versi>` (sesuai `go.mod`), jalankan `CGO_ENABLED=0 go build -o /out/schoolos ./cmd/server`.
   - Stage runtime: `gcr.io/distroless/static` atau `alpine` kecil. Jalankan sebagai user non-root.
   - `EXPOSE 8080`, `ENTRYPOINT ["/schoolos"]`.
2. Tambahkan `.dockerignore`: `.git`, `.env`, binary lokal, `tmp/`.
3. Buat project Railway dan tambahkan service PostgreSQL.
4. Buat service aplikasi dari repositori GitHub dan pilih region Singapura.
5. Set environment variable di Railway:
   - `APP_ENV=staging`
   - `DATABASE_URL` (dari referensi service Postgres)
   - `PORT` jika diperlukan
6. Jalankan migrasi ke database Railway. Opsi: perintah manual dari laptop dengan `DATABASE_URL` staging, atau tambahkan langkah pre-deploy. Pilih satu dan catat di `README.md`.
7. Aktifkan domain Railway dan catat URL-nya.
8. Buat satu pengguna admin awal untuk staging lewat perintah terpisah (`cmd/seed`), bukan lewat SQL manual yang tersimpan di riwayat.

## File yang disentuh
- `Dockerfile`, `.dockerignore`
- `cmd/seed/main.go` (opsional di task ini, bisa ditunda)
- `README.md` (bagian deploy)

## Kriteria selesai
- [x] `docker build .` berhasil di mesin lokal
- [x] Deploy Railway sukses dan status service hijau
- [x] `https://<url-railway>/healthz` mengembalikan `200`
- [x] `https://<url-railway>/healthz/db` mengembalikan `200`
- [x] Halaman login bisa dibuka dari browser
- [x] Tidak ada rahasia di repositori (dicek dengan `git log -p` secara sederhana)

## Catatan
- Gunakan lingkungan `staging` dulu. `production` dibuat setelah uji internal.
- Catat region dan alasannya di `README.md` sesuai keputusan Singapura.
