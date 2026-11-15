# T121 — Navigasi kembali (breadcrumb) yang jelas

**Fase:** 13 Perbaikan
**Dependensi:** —
**Estimasi:** 1 hari
**Status:** selesai (2026-11-15)

## Masukan pengguna (2026-11-15)
Desain navigasi kembali kurang bagus dan tidak jelas.

## Analisa kondisi sekarang
- **Bentuknya:** 38 template memakai `<p class="breadcrumb"><a>← Nama halaman</a></p>`. Ini satu tautan teks kecil (14 px, abu-abu `slate-500`) di atas judul.
  - Di HP mudah terlewat, dan area sentuhnya kecil (tinggi sekitar 20 px, di bawah anjuran 44 px).
  - Tidak terlihat seperti tombol.
- **Hanya satu tingkat:** contohnya "Isi nilai" → "← Gelombang 1", padahal halaman sebelumnya adalah "Tes seleksi". Pengguna kehilangan konteks posisinya (Pendaftaran › Gelombang 1 › Tes seleksi › Nilai).
- **Tujuan tidak konsisten:** detail pendaftar selalu kembali ke "/pendaftaran", walaupun dibuka dari Peringkat, Biaya, atau daftar yang sudah difilter. Filter dan halaman daftar hilang.
- **Halaman tanpa kembali:** beberapa halaman tingkat atas mengandalkan menu samping, yang di HP tersembunyi di menu bawah.

## Usulan
1. **Partial `back_nav` bersama** untuk semua halaman staf:
   - **Desktop:** breadcrumb lengkap, misalnya `Pendaftaran › Gelombang 1 › Tes seleksi`, dengan tingkat terakhir tidak bisa diklik.
   - **HP/tablet:** tombol kembali yang jelas (ikon panah + nama halaman induk, tinggi ≥ 44 px), dan breadcrumb dipotong ke induk langsung.
2. **Kembali ke daftar yang sama:** daftar pendaftar, siswa, dan Dapodik menyimpan filter dan halaman di parameter `kembali` (atau sessionStorage). Detail pendaftar kembali ke daftar dengan filter yang sama, atau ke Peringkat jika dibuka dari sana.
3. **Data breadcrumb dari handler** (`[]Crumb{Label, URL}`), bukan ditulis tangan di tiap template, supaya konsisten. Diganti bertahap mulai dari modul Pendaftaran dan Dapodik.
4. **Judul halaman di header HP** (opsional): saat menggulir, judul dan tombol kembali menempel di atas.

## Perlu keputusan
- Diterapkan ke semua modul sekaligus, atau mulai dari Pendaftaran dan Dapodik dulu? Saran: Pendaftaran dan Dapodik dulu, modul lain menyusul dengan partial yang sama.

## Kriteria selesai
- [x] Semua halaman Pendaftaran dan Dapodik memakai partial yang sama
- [x] Di HP tombol kembali terlihat jelas dengan area sentuh ≥ 44 px
- [x] Kembali dari detail pendaftar mempertahankan filter daftar asal

## Keputusan (2026-11-15)
Mulai dari Pendaftaran dan Dapodik; modul lain menyusul dengan partial yang sama.

## Catatan implementasi
- **Fungsi template `crumbs`** (`internal/platform/render`): `crumbs "Label" "/url" … "Halaman ini" ""` menghasilkan `Trail{Items, Parent}`. URL kosong = halaman sekarang. Jejak ditulis di template (bukan handler), karena labelnya hanya memakai data yang sudah ada di view.
- **Partial `back_nav`:**
  - Di bawah `lg` (HP dan tablet): tombol kembali ke induk terdekat, tinggi 44 px, bergaris tepi.
  - Di desktop: jejak lengkap, tingkat terakhir tidak bisa diklik (`aria-current="page"`).
- **`web/static/js/nav.js`** (dimuat di layout) mencatat query terakhir per path di `sessionStorage`. Tautan bertanda `data-restore` tanpa query diarahkan ke daftar dengan filter dan halaman terakhir di tab yang sama. Tanpa JavaScript, tautan tetap ke daftar tanpa filter.
- **Dipakai di:**
  - Pendaftaran: detail pendaftar, ubah isian staf, input pendaftar, rekap, jadikan siswa, pengaturan, daftar/detail/form periode, tes, nilai, peringkat.
  - Dapodik: cek kesiapan, lembar salin, impor.
- **Modul lain** masih memakai `.breadcrumb` lama, tetapi area sentuhnya kini 44 px.
- **Tidak dibuat:**
  - kembali ke Peringkat jika detail dibuka dari sana (tombol kembali selalu ke induk di jejak);
  - judul menempel saat menggulir (usulan 4).
