# T161 — Bank soal: aksi dikelompokkan dan bank bawaan rapi

**Fase:** 21 Rapikan
**Dependensi:** T157, T159
**Estimasi:** 1 hari

## Latar belakang
- **Detail bank berisi 10 tombol berjejer** dengan gaya yang sama:
  - TP, Ubah, Tambah stimulus, Impor, Ekspor Excel, Ekspor Moodle, Tambah soal;
  - di bawah filter: Kisi-kisi, Kartu soal, Cek mutu;
  - setiap soal masih punya Duplikat dan Arsipkan.

  Akibatnya, aksi utama (Tambah soal) sulit dibedakan.
- **Bank paket latihan bawaan** ("Latihan Antarmuka (contoh)") tampil sebagai "Tes umum" tanpa label Contoh. Bank contoh lain sudah berlabel (T154).
- **Daftar bank** belum bisa disaring per mapel. Saringannya hanya per tingkat.

## Keputusan
1. **Header detail bank:**
   - **Tambah soal** sebagai tombol utama, **Impor** sebagai tombol kedua;
   - menu **"Lainnya"**: Tambah stimulus, Tujuan pembelajaran, Ekspor Excel, Ekspor Moodle, Ubah bank, Arsipkan bank.
2. **Aksi untuk soal terpilih:**
   - Kisi-kisi, Kartu soal, dan Cek mutu digabung dalam satu tombol menu **"Cetak & periksa"** di bar daftar soal;
   - bar itu menampilkan jumlah soal terpilih ("3 soal terpilih", atau "semua 42 soal sesuai filter").
3. **Aksi per soal:** Duplikat dan Arsipkan dipindah ke menu titik tiga per baris. Klik pada baris membuka soal.
4. **Bank paket bawaan:**
   - ditandai `sample = true` (migrasi data untuk sekolah yang sudah punya) dan diberi label Contoh;
   - namanya mengikuti T159 ("Contoh — Coba Halaman Ujian").
5. **Daftar bank:** saringan mapel ditambahkan di samping tingkat. Bank contoh diurutkan paling akhir.

## Langkah
1. **Partial `action_menu`** (`<details>` dengan panel), dipakai di sini dan di T160. Bisa ditutup dengan Esc dan klik di luar, dan dapat diakses keyboard.
2. **Perbarui `bank_detail.html`** dan daftar soal di `partials/exam.html`.
3. **Migrasi data:** `UPDATE exam_banks SET sample = true WHERE id IN (bank paket bawaan)`. `seed_practice.go` membuat bank dengan `sample = true`.
4. **Saringan mapel** di `ListBanks`.

## File yang disentuh
- `web/templates/exam/bank_detail.html`, `banks.html`, `partials/exam.html`, `partials/action_menu.html` (baru)
- `internal/exam/bank.go`, `bank_handler.go`, `seed_practice.go`, migrasi data

## Catatan implementasi
- **Menu aksi tanpa partial baru.** Isi menu berbeda-beda (tautan, form POST dengan konfirmasi), sehingga yang dibagikan adalah pola `<details class="menu-details">` + `.menu-panel` + `.menu-link` (tautan atau tombol). Perilakunya ada di `js/nav.js` untuk semua halaman, termasuk menu "Lainnya" T160:
  - Esc menutup menu dan fokus kembali ke tombolnya;
  - klik di luar menutup menu;
  - membuka satu menu menutup menu lain.
- **Header detail bank:** Tambah soal (utama), Impor (kedua), "Lainnya" berisi Tambah stimulus, Tujuan pembelajaran, Ekspor Excel, Ekspor Moodle, Ubah bank, Arsipkan bank. Tombol "Arsipkan bank" di bawah halaman dihapus. Kepala sekolah hanya melihat Tujuan pembelajaran di "Lainnya".
- **"Cetak & periksa"** ada di bar jumlah soal ("Menampilkan 1–20 dari 42 soal"). Panelnya menulis "3 soal terpilih" atau "Semua 42 soal sesuai filter". Cek mutu kini juga mengikuti soal terpilih/filter, sama seperti kisi-kisi dan kartu soal.
- **Per soal:** Duplikat dan Arsipkan di menu "⋯". Klik di mana pun pada kartu soal membuka soal; kotak pilih dan menu tetap bisa diklik.
- **Bank paket bawaan:** migrasi 088 menandai `sample = true` dan mengganti nama "Latihan Antarmuka (contoh)" menjadi "Contoh — Coba Halaman Ujian" (bila belum diubah sekolah). Bank dikenali dari soal yang dipakai ujian bawaan.
- **Saringan mapel:** pilihan diambil dari bank yang boleh dilihat pengguna (`ListBankSubjects`). Bila saringan tidak menemukan bank, tampil "Tidak ada bank yang cocok" dengan tautan hapus saringan.

## Kriteria selesai
- [x] Header detail bank: satu tombol utama, satu tombol kedua, dan menu "Lainnya"; semua aksi lama tetap tercapai (uji template)
- [x] Menu "Cetak & periksa" menampilkan jumlah soal yang akan diproses
- [x] Bank paket bawaan berlabel Contoh, termasuk di sekolah yang sudah ada (uji migrasi)
- [x] Saringan mapel bekerja; bank contoh di urutan akhir
