# T11 — Layout dan halaman dasar SSR

**Fase:** 01 Fondasi
**Dependensi:** T09
**Estimasi:** 1 hari

## Tujuan
Ada layout HTML yang dipakai semua halaman, dengan CSS variables, Alpine.js dari file lokal, dan halaman login dan beranda yang tampil baik di mobile dan desktop.

## Langkah
1. Buat `web/templates/layout.html`:
   - `<head>` berisi meta viewport, meta CSRF (dari T09), judul halaman, dan link CSS.
   - Blok `{{ block "content" . }}` untuk isi halaman.
   - Script Alpine.js dimuat dengan `defer` dari `/static/js/alpine.min.js`.
2. Buat `web/static/css/app.css`:
   - CSS variables di `:root` untuk warna, jarak, dan ukuran font.
   - Gaya dasar: body, tombol, form, tabel sederhana, flash message.
   - Pastikan tidak ada lebar tetap yang membuat scroll horizontal di layar 360px.
3. Unduh Alpine.js versi yang dipilih ke `web/static/js/alpine.min.js`. Simpan versi di komentar `README.md`.
4. Buat `web/templates/auth/login.html`:
   - Form email dan password, `csrfField`, tombol masuk.
   - Pesan error dari parameter flash, bukan dari query string.
5. Buat `web/templates/home.html`:
   - Sapaan dengan nama pengguna dan nama sekolah.
   - Belum ada menu peran (dikerjakan di T51).
6. Buat helper render di `internal/platform/render`:
   - Parse semua template saat start.
   - Fungsi `Render(w, name, data)` dengan error handling yang benar (status 500 dan log jika gagal).
7. Pasang route `GET /login` dan `GET /` dengan handler yang memakai helper render.

## File yang disentuh
- `web/templates/layout.html`, `web/templates/home.html`, `web/templates/auth/login.html`
- `web/static/css/app.css`, `web/static/js/alpine.min.js`
- `internal/platform/render/render.go`

## Kriteria selesai
- [ ] Halaman login tampil di Chrome desktop dan di emulasi mobile 360px tanpa scroll horizontal
- [ ] Alpine.js termuat (dicek dengan `window.Alpine` di console)
- [ ] Tidak ada request ke CDN eksternal (dicek di tab Network)
- [ ] Template yang gagal di-parse membuat server gagal start dengan pesan jelas

## Catatan
- Warna dan font cukup satu set sederhana. Tidak perlu desain final di tahap ini.
- Tailwind tidak dipakai (sesuai keputusan di rancangan bagian 11).
