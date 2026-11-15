# T114 — Rapikan halaman staf Pendaftaran dan Dapodik; lembar salin dengan accordion

**Fase:** 13 Perbaikan
**Dependensi:** T93, T97, T98, T103, T113
**Estimasi:** 1,5–2 hari

## Latar belakang
Dicek dengan data demo (±300 siswa), lebar desktop dan HP (2026-11-15):
- **Lembar salin:** ±4.000 px (±4.900 px di HP). Sepuluh bagian terbuka semua, banyak baris "Kosong", dan halaman sedikit melebar ke samping di HP. Operator harus terus menggulir sambil menyalin.
- **Daftar Dapodik:** ±15.000 px, semua siswa dalam satu daftar tanpa halaman. Tombol alat (impor, nilai, F-PD, ekspor) berjajar tanpa pengelompokan.
- **Detail pendaftar:** dua kolom sempit dengan separuh kanan layar kosong, NIK keluar dari kotak, data terpecah ke banyak kartu kecil.
- **Rekap pendaftaran:** melebar ke samping di HP.

## Keputusan (2026-11-15)
Langsung dikerjakan tanpa mockup.

## Langkah
1. **Lembar salin**
   - Setiap bagian menjadi **accordion**; hanya satu yang terbuka. Judul memuat jumlah terisi dan jumlah yang sudah disalin.
   - **Salin berikutnya** (tombol `n`) melompat antar-bagian dan membuka bagian berikutnya otomatis.
   - **Sembunyikan yang kosong** (diingat browser).
   - **Bar bawah yang menempel:** Salin berikutnya, status salinan, Tandai sudah masuk Dapodik, dan Tandai lalu ke siswa berikutnya.
   - Tidak melebar di HP.
2. **Daftar Dapodik**
   - Kartu ringkasan (semua / belum lengkap / siap dimasukkan / sudah masuk Dapodik) yang sekaligus menjadi filter.
   - Pencarian nama/NIS.
   - Halaman 50 siswa.
   - Tombol "Lanjutkan input" ke siswa siap pertama yang belum masuk Dapodik.
   - Alat lain dikelompokkan dalam menu.
3. **Detail pendaftar**
   - Kolom utama lebar berisi data pendaftar dalam accordion per bagian (Identitas, Data pribadi, Alamat, Orang tua/wali, Sekolah asal), dengan tombol buka/tutup semua.
   - Kolom kanan di desktop: tindak lanjut, dokumen, biaya, data pribadi (T113), riwayat.
   - Nilai panjang (NIK) dibungkus rapi.
4. **Rekap pendaftaran:** tidak melebar di HP.
5. **Uji:** handler halaman (filter, halaman, pencarian, tanpa data sekolah lain) dan cek tampilan di 390 px dan 1280 px tanpa gulir horizontal.

## Kriteria selesai
- [x] Lembar salin: satu bagian terbuka, Salin berikutnya pindah bagian otomatis, sembunyikan kosong, bar bawah selalu terlihat
- [x] Daftar Dapodik dipecah per halaman dengan ringkasan, filter, dan pencarian
- [x] Detail pendaftar memakai lebar layar; NIK tidak keluar kotak
- [x] Tidak ada gulir horizontal di 390 px untuk halaman yang disebut di atas
- [x] Uji handler lulus; route tidak berubah

## Catatan implementasi
- **Lembar salin** (`dapodik/sheet.html`)
  - Accordion per bagian (Alpine; tanpa JavaScript semua bagian terbuka). Judul menampilkan "x/y disalin" atau "kosong".
  - **Salin berikutnya** membuka bagian field berikutnya lalu menggulir ke field itu. Field yang sudah disalin diberi warna hijau, yang sedang disalin biru.
  - **Sembunyikan yang kosong** menyembunyikan baris "Kosong" dan field yang tidak disimpan AKRA; pilihan ini disimpan di `localStorage`.
  - **Bar bawah yang menempel** berisi Salin berikutnya, Ulang, status, dan tombol tanda. Di HP bar berada tepat di atas menu navigasi bawah.
  - Kekurangan data ditampilkan sebagai ringkasan yang bisa dibuka.
  - Hasil di desktop: ±4.000 px → ±2.450 px (satu bagian terbuka) → ±1.080 px dengan "sembunyikan yang kosong".
- **Daftar Dapodik**
  - Satu CTE dipakai untuk ringkasan, hitungan, dan baris.
  - Filter status `kurang` / `siap` / `sudah` (dan `belum` lama tetap jalan), pencarian nama/NIS (karakter LIKE di-escape), dan halaman 50 siswa (`?hal=`). Halaman di luar jangkauan jatuh ke halaman terakhir.
  - Tombol "Lanjutkan input" menuju siswa siap pertama yang belum masuk Dapodik.
  - Alat tambahan ada di menu "Alat lain".
  - Dengan 240 siswa demo: ±15.000 px → ±3.560 px per halaman.
- **Detail pendaftar**
  - Data pendaftar dalam satu kartu dengan `<details>` per bagian (dua bagian pertama terbuka, plus tombol buka/tutup semua).
  - Kolom samping 22 rem di desktop.
  - Kolom kanan **tidak dibuat menempel**: isinya bisa lebih tinggi dari layar, dan sticky akan memotong bagian bawahnya.
- **CSS bersama**
  - `.data-list` membatasi kolom label 45% dan membungkus nilai panjang; NIK tidak lagi keluar kartu.
  - `.detail-grid` memakai `grid-cols-1` di HP agar tabel lebar di dalam kartu tidak melebarkan halaman (memperbaiki rekap pendaftaran dan halaman lain yang memakai kelas ini).
- **Pengecekan:** Chrome 390 px dan 1280 px, tanpa gulir horizontal di rekap, daftar Dapodik, lembar salin, dan detail pendaftar.

