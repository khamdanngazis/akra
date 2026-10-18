# R01 — Redesain tampilan (Tailwind CSS)

**Fase:** Sebelum 05 Impor dan Uji
**Dependensi:** T11, T51
**Branch:** `feat/redesign`

## Permintaan awal
> buat design lebih modern dan bagus
>
> bisa di buka di destkop, mobile dan table

Lanjutan: styling dipindah ke Tailwind CSS agar tampilan lebih bagus.

## Tujuan
Tampilan aplikasi lebih modern dan konsisten dengan [brand identity](../../brand-identity.md), dan nyaman dipakai di desktop, tablet, dan HP. Semua halaman yang sudah ada tetap berfungsi tanpa perubahan alur.

## Keputusan
- **Tailwind CSS v4 standalone CLI** (binary, tanpa Node), sesuai opsi di rancangan bagian 11 (`software-architecture.md`). Catatan "Tailwind tidak dipakai" di T11 tidak berlaku lagi.
- Hasil build `web/static/css/app.css` **di-commit**, sehingga `go build`, Docker, dan deploy Railway tidak butuh Tailwind.
- Pendekatan campuran:
  - komponen yang dipakai banyak halaman (`btn`, `card`, `badge`, `field`, `stat-grid`, `pay-item`, dan lain-lain) ditulis dengan `@apply` di `@layer components`, dengan nama class yang sama seperti sebelumnya;
  - tata letak shell, beranda, dan login memakai utility class langsung di template.
- Sidebar **navy** (opsi "Sidebar navy atau putih" di brand identity), ikon outline dari **Lucide** (lisensi ISC) sebagai sprite SVG lokal. Tidak ada aset dari CDN.

## Sistem desain
| Elemen | Nilai |
|---|---|
| Primary / sidebar | Navy `#0B3D91` (`navy-900`, palet `navy` sendiri) |
| Tombol utama, tautan | Blue `#2563EB` (`blue-600`) |
| Sukses / aksen | Teal `#10B981` (`emerald-500`) |
| Latar | `#F8FAFC` (`slate-50`) |
| Card | Putih, `rounded-2xl`, ring halus `slate-900/5`, bayangan tipis |
| Font | Plus Jakarta Sans (lokal), dasar 15px |
| Target sentuh | Tombol, input, dan item menu minimal 44px |

## Tata letak per ukuran layar
- **Desktop (>= 1024px):** sidebar navy tetap di kiri, berisi:
  - logo dan nama sekolah;
  - menu ber-ikon sesuai peran (item aktif berupa kartu putih);
  - kartu pengguna (inisial, nama, peran) dan tombol keluar.
- **Tablet dan HP (< 1024px):** top bar dengan tombol menu. Sidebar terbuka sebagai drawer, dan bisa ditutup dengan tombol ×, klik latar, atau Esc. Tanpa JavaScript, menu tampil biasa di atas konten.
- **Login (>= 56rem):** dua kolom, yaitu panel brand navy (tagline "Satu Sistem, Banyak Cerita." dan poin fitur) lalu form. Di HP hanya form.

## Langkah yang dikerjakan
1. Sistem desain dan kerangka baru (`baa446d`):
   - shell sidebar dan drawer;
   - sprite ikon Lucide (26 ikon) dan field `Icon` di `nav.Items`;
   - fungsi template `initials` dan `roleLabel`;
   - beranda dengan kartu ber-ikon;
   - login dua kolom.
2. Pindah ke Tailwind CSS v4.3.3 (`9a71dc2`):
   - sumber `web/tailwind/app.css` dengan tema brand di `@theme`, base (pengganti gaya bawaan yang dihapus Preflight), dan komponen;
   - sidebar navy bergradien;
   - banner sapaan di beranda;
   - select dengan ikon panah sendiri;
   - target Makefile `css`, `css-watch`, dan `css-check`.
3. Pengecekan visual 25 halaman dengan data demo untuk kelima peran, di lebar 390, 768, dan 1280, termasuk drawer terbuka dan tertutup.

## File yang disentuh
- `web/tailwind/app.css` (sumber), `web/static/css/app.css` (hasil build)
- `web/templates/layout.html`, `home.html`
- `web/templates/partials/app_header.html`, `icons.html`, `auth_aside.html`, `logo.html`
- `web/templates/auth/*.html` (pembungkus dua kolom)
- `internal/platform/nav/menu.go` (ikon, `RoleLabel`, `Initials`), `internal/platform/render/render.go`
- `Makefile`, `README.md`

## Cara kerja sehari-hari
- Setiap mengubah template atau `web/tailwind/app.css`, jalankan `make css` lalu commit `web/static/css/app.css`. Tailwind hanya menghasilkan class yang dipakai di `web/templates`.
- `make css-watch` saat mengedit tampilan.
- `make css-check` gagal jika CSS yang di-commit tidak sesuai sumbernya (bisa dipasang di CI).
- Binary Tailwind diunduh otomatis ke `bin/` (di-ignore git) dengan versi dan checksum terkunci di Makefile.

## Kriteria selesai
- [x] Tampilan mengikuti brand identity (warna, font, card membulat, ikon outline)
- [x] Desktop memakai sidebar; tablet dan HP memakai top bar dan drawer
- [x] Tidak ada halaman yang scroll horizontal di lebar 360–390px
- [x] Menu tetap bisa dipakai tanpa JavaScript
- [x] Tidak ada aset dari CDN (`TestTemplates_NoExternalResources` lulus)
- [x] Seluruh `go test ./...` lulus
- [x] `make css-check` lulus (CSS yang di-commit sesuai sumber)
- [ ] Dicoba di HP dan tablet sungguhan setelah deploy ke staging

## Catatan
- Halaman selain shell, beranda, dan login masih memakai class komponen (hasil `@apply`). Halaman-halaman itu bisa dipindah bertahap ke utility class murni jika diperlukan.
- Logo masih konsep awal (lihat brand identity bagian 4), belum aset final.
