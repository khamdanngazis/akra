# T120 — "Jadikan siswa" langsung dari alur penerimaan; format NIS lebih sederhana

**Fase:** 13 Perbaikan
**Dependensi:** T96, T116
**Estimasi:** 1 hari
**Status:** catatan dan analisa, belum dikerjakan

## Masukan pengguna (2026-11-15)
- Kenapa "Jadikan siswa" harus menu terpisah?
- Kenapa selalu harus mengatur format NIS?
- Penjelasan format dan cara membuatnya kurang.
- Ketika tidak ada format, membingungkan.

## Analisa kondisi sekarang
- **Kenapa terpisah (alasan T96):**
  - Menerima belum tentu jadi siswa. Orang tua bisa mundur sebelum daftar ulang.
  - Sekolah biasanya membuat NIS dan membagi kelas **massal** setelah daftar ulang.

  Alasannya masuk akal, tapi tidak dijelaskan di UI. Halaman `/pendaftaran/terima` hanya ditautkan dari tombol di atas daftar pendaftar, tidak dari detail pendaftar yang diterima.
- **Format NIS:**
  - Sebenarnya **tidak wajib diatur**. Jika kosong dipakai bawaan `{yyyy}{seq:3}`, dan NIS juga bisa diketik manual per baris.
  - Tapi kartu "Format NIS otomatis" selalu tampil **di atas** daftar dengan input berisi token (`{yyyy}{seq:3}`). Kesannya harus diisi dulu.
  - Penjelasan token hanya satu baris kecil.
  - Contoh NIS memakai tahun berjalan + 1, bukan tahun ajaran pendaftar.
  - Jika sekolah sudah punya pola NIS lama (mis. 2324001), tidak ada cara "lanjutkan dari NIS terakhir".
- **"Tidak ada format":** jika belum ada pendaftar diterima, halaman hanya menampilkan kartu format dan "Tidak ada pendaftar diterima…". Pengguna tidak tahu harus apa.

## Usulan
1. **Tombol "Jadikan siswa" di detail pendaftar** yang berstatus Diterima, sebagai langkah terakhir di kartu Langkah berikutnya (T116). Dialog kecil berisi NIS (terisi otomatis, bisa diubah) dan kelas (opsional).
2. **Halaman massal tetap ada,** tapi menjadi tab/langkah terakhir "Jadikan siswa" di alur seleksi gelombang (T118). Kalimat pembuka menjelaskan kenapa dipisah dari "Diterima" (menunggu daftar ulang).
3. **Format NIS disembunyikan sebagai pengaturan:**
   - Di halaman hanya tertulis "NIS otomatis: 2027001, 2027002, … · Ubah pola".
   - Pengaturan dibuka di dialog atau halaman pengaturan sekolah.
4. **Pembuat pola tanpa token,** dengan pratinjau langsung. Pilihan:
   - **Tahun + nomor urut**, dengan pilihan 2 atau 4 digit tahun dan jumlah digit nomor;
   - **Awalan + nomor urut**, misalnya "S-" + 0001;
   - **Lanjutkan dari NIS terakhir di sekolah**, terdeteksi dari data siswa yang ada.

   Token `{seq:N}` hanya untuk mode lanjutan.
5. **Contoh NIS memakai tahun ajaran gelombang** pendaftar yang akan dijadikan siswa.
6. **Keadaan kosong yang jelas:** "Belum ada pendaftar diterima. Terima pendaftar di Peringkat dan hasil atau di detail pendaftar", dengan tautan.

## Keputusan (2026-11-15)
- **Dua cara didukung, dipilih sekali di tingkat sekolah,** bukan per gelombang. Pengaturan di form gelombang dinilai menambah bingung.
  - **Dengan daftar ulang (bawaan):** Diterima → daftar ulang → Jadikan siswa (per pendaftar dari detail, atau massal).
  - **Tanpa daftar ulang:** saat pendaftar ditetapkan Diterima (dari detail atau Peringkat dan hasil), ia langsung dijadikan siswa dengan NIS otomatis; kelas diatur belakangan.
- Tempatnya di halaman **Pengaturan pendaftaran** (satu halaman sekolah), bersama pola NIS (usulan 3–4). Halaman massal dan tombol di detail menyebut cara yang sedang dipakai, dengan tautan "Ubah di pengaturan".
- Mode dengan daftar ulang: tambah tanda **"Sudah daftar ulang"** dan batas waktu opsional, supaya TU tahu siapa yang belum datang. Yang tidak daftar ulang bisa ditandai Mundur, lalu kursinya diberikan ke cadangan.
- Mode tanpa daftar ulang: jika siswa ternyata tidak masuk, TU memakai alur keluar/mutasi siswa yang sudah ada.

## Kriteria tambahan
- [ ] Pengaturan sekolah "Pakai daftar ulang" (ya/tidak) bekerja untuk penetapan per pendaftar dan massal
- [ ] Mode tanpa daftar ulang: Diterima langsung membuat siswa dengan NIS otomatis, dalam satu transaksi

## Kriteria selesai
- [ ] Pendaftar diterima bisa dijadikan siswa dari halaman detailnya
- [ ] Format NIS tidak perlu disentuh untuk memakai bawaan, dan bisa diatur tanpa mengetik token
- [ ] Halaman kosong menjelaskan langkah berikutnya
