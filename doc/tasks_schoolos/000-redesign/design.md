# R01 — Redesain tampilan (Tailwind CSS)

**Fase:** Sebelum 05 Impor dan Uji
**Dependensi:** T11, T51
**Branch:** `feat/redesign`

## Permintaan awal
> buat design lebih modern dan bagus
>
> bisa di buka di destkop, mobile dan table

Lanjutan:
- styling dipindah ke Tailwind CSS agar tampilan lebih bagus;
- "masih kurang coba redesign berdasarkan brand guide ini": ikuti [AKRA_Brand_Design_Guide.md](AKRA_Brand_Design_Guide.md) dan [AKRA School Management UI Showcase.png](<AKRA School Management UI Showcase.png>), logo memakai [Blue and Teal Ribbon A Logo.png](<Blue and Teal Ribbon A Logo.png>).

## Tujuan
Tampilan aplikasi lebih modern dan konsisten dengan [brand guide AKRA](AKRA_Brand_Design_Guide.md), dan nyaman dipakai di desktop, tablet, dan HP. Semua halaman yang sudah ada tetap berfungsi tanpa perubahan alur.

## Keputusan
- **Tailwind CSS v4 standalone CLI** (binary, tanpa Node), sesuai opsi di rancangan bagian 11 (`software-architecture.md`). Catatan "Tailwind tidak dipakai" di T11 tidak berlaku lagi.
- Hasil build `web/static/css/app.css` **di-commit**, sehingga `go build`, Docker, dan deploy Railway tidak butuh Tailwind.
- Pendekatan campuran:
  - komponen yang dipakai banyak halaman (`btn`, `card`, `badge`, `field`, `stat-grid`, `pay-item`, dan lain-lain) ditulis dengan `@apply` di `@layer components`, dengan nama class yang sama seperti sebelumnya;
  - tata letak shell, beranda, dan login memakai utility class langsung di template.
- Sidebar **navy** dengan item aktif berupa bidang biru (brand guide bagian 5), ikon outline dari **Lucide** (lisensi ISC) sebagai sprite SVG lokal. Tidak ada aset dari CDN.
- Logo: simbol A diambil dari PNG resmi (latar transparan dibersihkan, dipotong persegi) menjadi `web/static/img/akra-mark.png` dan `favicon.png`. Wordmark "AKRA" dibuat ulang sebagai SVG garis dengan `currentColor`, supaya bisa navy di latar terang dan putih di sidebar tanpa dua file gambar.
- Hanya menampilkan data yang benar-benar ada. Tidak ada lonceng notifikasi, "Ingat saya", atau grafik contoh seperti di showcase, karena fiturnya belum ada.

## Sistem desain
| Elemen | Nilai |
|---|---|
| Primary / sidebar | Navy `#0B3D91` (`navy-900`, palet `navy` sendiri) |
| Tombol utama, tautan | Blue `#2563EB` (`blue-600`) |
| Sukses / aksen | Teal `#10B981` (`emerald-500`) |
| Latar | `#F8FAFC` (`slate-50`) |
| Card | Putih, `rounded-2xl`, border `slate-200` (`#E2E8F0`), bayangan lembut |
| Teks | `slate-900` (`#0F172A`) dan `slate-500` (`#64748B`), tanpa huruf kapital penuh |
| Font | Plus Jakarta Sans (lokal), dasar 15px |
| Target sentuh | Tombol, input, dan item menu minimal 44px |

## Tata letak per ukuran layar
- **Semua ukuran:** header putih berisi pencarian sesuai peran (admin/kepala: cari siswa; bendahara: cari siswa untuk pembayaran) dan menu profil (inisial, nama, peran, sekolah, Keluar).
- **Desktop (>= 1024px):** sidebar navy tetap di kiri, berisi logo, menu ber-ikon sesuai peran (item aktif biru), dan kartu sekolah dengan peran pengguna di bawah.
- **Tablet (768–1023px):** tombol menu di header membuka sidebar sebagai drawer, dan bisa ditutup dengan tombol ×, klik latar, atau Esc.
- **HP (< 768px):** bottom navigation dengan 4 menu pertama dan tombol "Menu" (membuka drawer) bila menu lebih dari 4.
- **Tanpa JavaScript:** menu tampil biasa di atas konten; bottom navigation dan tombol drawer disembunyikan.
- **Login dan halaman akun lain:** latar terang, logo dengan tagline "Satu Sistem, Banyak Cerita", satu card form dengan ikon di input, tombol lihat password, dan tombol biru.

## Beranda
Mengikuti bagian "Dashboard administrator" di brand guide:
- sapaan "Selamat datang kembali, {nama}!" dengan nama sekolah, tanggal, dan tahun ajaran;
- kartu ringkasan per peran, masing-masing satu angka utama, label, dan keterangan:
  - admin/TU dan kepala sekolah: siswa aktif, guru, kelas, dan pengumuman di tahun ajaran aktif;
  - bendahara: sisa tunggakan, siswa menunggak, pembayaran hari ini, dan uang diterima hari ini (ditambah tombol "Catat pembayaran");
  - guru: kelas wali dan absensi yang belum diisi;
- panel utama: kehadiran minggu ini dengan bar persentase (admin dan kepala), pembayaran terbaru (bendahara), status absensi kelas wali (guru);
- panel samping "Pengumuman terbaru" (4 terbaru) dengan tautan "Lihat semua";
- "Akses cepat": maksimal 6 menu sesuai peran beserta keterangannya.

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
3. Redesain sesuai brand guide:
   - logo resmi (simbol PNG dan wordmark SVG), favicon PNG;
   - header putih dengan pencarian dan menu profil, sidebar dengan item aktif biru dan kartu sekolah;
   - bottom navigation di HP (`nav.Bottom`, label pendek `Short`);
   - login satu kolom terang (partial `auth_logo`, `password_field`, fungsi template `dict`);
   - beranda dengan kartu ringkasan, aktivitas terbaru, dan akses cepat (`home.KPI`, `SchoolSummaryFor`, `RecentPaymentsFor`, `nav.Shortcuts`);
   - tombol secondary bergaris biru, badge berbentuk pil, header tabel tanpa huruf kapital.
4. Pengecekan visual 25 halaman dengan data demo untuk kelima peran, di lebar 390, 768, dan 1280, termasuk drawer terbuka dan tertutup.

## File yang disentuh
- `web/tailwind/app.css` (sumber), `web/static/css/app.css` (hasil build)
- `web/static/img/akra-mark.png`, `favicon.png` (menggantikan `favicon.svg`)
- `web/templates/layout.html`, `home.html`
- `web/templates/partials/app_header.html`, `icons.html`, `logo.html`, `auth_logo.html` (menggantikan `auth_aside.html`)
- `web/templates/auth/*.html`
- `internal/home/handler.go` (kartu ringkasan, pembayaran dan pengumuman terbaru)
- `internal/platform/nav/menu.go` (ikon, `Short`, `Desc`, `Bottom`, `Shortcuts`, `RoleLabel`, `Initials`), `internal/platform/render/render.go`
- `Makefile`, `README.md`

## Cara kerja sehari-hari
- Setiap mengubah template atau `web/tailwind/app.css`, jalankan `make css` lalu commit `web/static/css/app.css`. Tailwind hanya menghasilkan class yang dipakai di `web/templates`.
- `make css-watch` saat mengedit tampilan.
- `make css-check` gagal jika CSS yang di-commit tidak sesuai sumbernya (bisa dipasang di CI).
- Binary Tailwind diunduh otomatis ke `bin/` (di-ignore git) dengan versi dan checksum terkunci di Makefile.

## Kriteria selesai
- [x] Tampilan mengikuti brand guide (warna, font, card membulat, ikon outline, logo resmi)
- [x] Desktop memakai sidebar; tablet memakai drawer; HP memakai bottom navigation dan drawer
- [x] Tidak ada halaman yang scroll horizontal di lebar 360–390px
- [x] Menu tetap bisa dipakai tanpa JavaScript
- [x] Tidak ada aset dari CDN (`TestTemplates_NoExternalResources` lulus)
- [x] Seluruh `go test ./...` lulus
- [x] `make css-check` lulus (CSS yang di-commit sesuai sumber)
- [ ] Dicoba di HP dan tablet sungguhan setelah deploy ke staging

## Catatan
- Halaman selain shell, beranda, dan login masih memakai class komponen (hasil `@apply`). Halaman-halaman itu bisa dipindah bertahap ke utility class murni jika diperlukan.
- Logo memakai PNG resmi; file master vektor belum ada, jadi simbol disimpan sebagai PNG 512px. Ganti dengan SVG resmi bila sudah tersedia.
