# T121 — Navigasi kembali (breadcrumb) yang jelas

**Fase:** 13 Perbaikan
**Dependensi:** —
**Estimasi:** 1 hari
**Status:** catatan dan analisa, belum dikerjakan

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
- [ ] Semua halaman Pendaftaran dan Dapodik memakai partial yang sama
- [ ] Di HP tombol kembali terlihat jelas dengan area sentuh ≥ 44 px
- [ ] Kembali dari detail pendaftar mempertahankan filter daftar asal
