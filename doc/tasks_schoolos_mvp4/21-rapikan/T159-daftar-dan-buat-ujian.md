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

## Kriteria selesai
- [ ] Daftar ujian langsung terlihat di layar pertama (desktop 1280×800 dan HP 360 px)
- [ ] Saringan dan pencarian bekerja dan diuji; daftar terpaginasi
- [ ] Tidak ada lagi dua hal berbeda bernama "Ujian latihan" di antarmuka staf
- [ ] Panduan dan PDF memakai nama baru
