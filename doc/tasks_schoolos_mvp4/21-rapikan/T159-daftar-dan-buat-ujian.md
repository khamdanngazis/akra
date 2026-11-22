# T159 — Daftar ujian, alur buat ujian, dan penamaan latihan

**Fase:** 21 Rapikan
**Dependensi:** T157
**Estimasi:** 1,5 hari

## Latar belakang
- **Daftar ujian dimulai dengan kartu besar.** Lima kartu "Buat ujian" (Ulangan, Ujian latihan, PTS, PAS, Tes seleksi) mengisi setengah layar. Pilihan yang sama muncul lagi di langkah 1 wizard, sedangkan daftarnya sendiri terdorong ke bawah.
- **Daftar tanpa saringan.** Tidak ada saringan tujuan, status, mapel, atau kelas, dan tidak ada pencarian. Setelah satu semester, daftar ini akan panjang.
- **Dua hal bernama "ujian latihan":**
  1. jenis ujian **Ujian latihan** yang dibuat guru (hasil langsung, boleh mengulang);
  2. halaman **Ujian latihan** (`/ujian/latihan`) yang membuka paket bawaan "Latihan Antarmuka Ujian" untuk mengenalkan halaman ujian.

  Keduanya muncul berdampingan di beranda dan daftar ujian.

## Keputusan
1. **Tombol "Buat ujian"** di header daftar menuju wizard langkah 1, yang sudah berisi pilihan tujuan. Kartu "Buat ujian" di daftar dihapus. Tujuan yang tidak boleh dipakai pengguna tidak ditampilkan di wizard.
2. **Saringan daftar ujian:** cari nama; tujuan; status (draf, terjadwal, berlangsung, selesai); mapel; kelas; tahun ajaran (bawaan: tahun aktif). Saringan diingat per tab lewat pola T121.
3. **Penamaan** (usulan, dikonfirmasi saat review):
   - halaman `/ujian/latihan` → **"Coba halaman ujian"**, paket bawaan → **"Coba Halaman Ujian"**;
   - jenis ujian "Ujian latihan" tetap.

   Label lama di panduan (`doc/panduan-ujian/`), panduan PDF, dan halaman bantuan ikut diganti.
4. **Daftar:** kolom Ujian (nama, pemilik), Tujuan, Mapel/kelas, Jadwal terdekat, Status, dan aksi cepat sesuai status:

   | Status | Aksi cepat |
   |---|---|
   | Draf | Lanjutkan |
   | Berlangsung | Pantau |
   | Selesai | Hasil |

   Ujian bawaan "Coba halaman ujian" tidak tampil di daftar; ujian itu hanya diakses dari halamannya sendiri.

## Langkah
1. **`exams.html`:** hapus kartu buat ujian; tambah tombol header dan bar saringan (pola `filter-bar`).
2. **`ListExams`:** parameter saringan, paginasi 25 per halaman, indeks bila perlu (cek `EXPLAIN` dengan 2.000 ujian per sekolah).
3. **Ganti label "Ujian latihan" untuk halaman bawaan:**
   - template, nav, panduan, guide PDF, empty state, tour;
   - nama paket bawaan: migrasi data untuk sekolah yang sudah punya.
4. **Uji:** saringan, paginasi, ujian bawaan tidak tampil, dan tombol buat ujian sesuai hak akses.

## File yang disentuh
- `internal/exam/exam_handler.go`, `exam.go` (daftar), `seed_practice.go`
- `web/templates/exam/exams.html`, `practice.html`, `home.html`, `help.html`
- `internal/exam/guide_pdf.go`, `doc/panduan-ujian/*.md`
- migrasi data nama paket bawaan

## Catatan implementasi
- **Nama baru dipakai:** halaman `/ujian/latihan` dan paket bawaan menjadi **"Coba halaman ujian"** / **"Coba Halaman Ujian"**. Tombolnya ada di beranda modul dan header Daftar Ujian.
  - Paket bawaan sekolah yang sudah ada diganti namanya lewat migrasi 087 (hanya bila namanya belum diubah sekolah).
  - Teks pendaftaran (tes seleksi), cek kesiapan, PDF panduan peserta, dan `doc/panduan-ujian/` ikut diganti.
  - Nama bank paket bawaan menunggu T161.
- **Saringan:** cari nama, tujuan, status, mapel, kelas, tahun ajaran. Pilihan mapel dan kelas diambil dari ujian yang boleh dilihat pengguna. Tahun ajaran bawaan adalah tahun aktif; ujian tanpa tahun ajaran selalu ikut. Saringan berlaku otomatis saat diubah, dan "Hapus saringan" muncul bila ada saringan.
- **Paginasi** 25 per halaman. Diukur dengan 2.000 ujian di satu sekolah: query inti sekitar 3 ms, termasuk di halaman terakhir. Indeks `(school_id, created_at)` yang sudah ada cukup.
- **Kolom:** Ujian (pemilik, jumlah soal, jumlah sesi), Tujuan, Mapel/kelas, Jadwal terdekat, Status, dan aksi cepat (Lanjutkan / Pantau / Hasil).

## Kriteria selesai
- [x] Daftar ujian langsung terlihat di layar pertama (desktop 1280×800); HP 360 px perlu dicek sekali di perangkat sungguhan
- [x] Saringan dan pencarian bekerja dan diuji; daftar terpaginasi
- [x] Tidak ada lagi dua hal berbeda bernama "Ujian latihan" di antarmuka staf
- [x] Panduan dan PDF memakai nama baru
