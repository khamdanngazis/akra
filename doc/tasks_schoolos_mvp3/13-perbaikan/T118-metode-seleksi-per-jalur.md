# T118 — Metode seleksi per jalur, peringkat yang jelas, dan alur verifikasi/terima

**Fase:** 13 Perbaikan
**Dependensi:** T93a, T93b, T117
**Estimasi:** 2–3 hari (tergantung metode yang dipilih)
**Status:** catatan dan analisa, belum dikerjakan

## Masukan pengguna (2026-11-15)
- Jalur peringkat membingungkan: tidak jelas peringkat berdasarkan apa, dan di mana calon siswa memasukkan nilai peringkatnya.
- TU bingung saat akan memverifikasi atau menetapkan diterima.

## Analisa kondisi sekarang
- **Tidak ada "jalur peringkat" sebagai jenis.** Jalur hanya punya centang "memakai tes seleksi". Halaman **Peringkat dan hasil** mengurutkan dengan dua aturan (`RankTrack`):
  - **jalur dengan tes:** skor akhir dari nilai tes yang diisi TU;
  - **jalur tanpa tes:** **waktu kirim formulir paling awal** (siapa cepat dia dapat).

  Aturan kedua hanya tertulis kecil ("urut waktu kirim"). Jalur bernama "Prestasi" atau "Peringkat" tanpa tes diam-diam diurutkan berdasarkan waktu kirim, bukan nilai.
- **Calon siswa tidak bisa memasukkan nilai.** Formulir publik tidak punya isian nilai rapor, prestasi, atau sertifikat. Satu-satunya nilai adalah nilai tes yang diisi TU di grid nilai.
- **Jalur yang umum di SPMB** (prestasi/nilai rapor, domisili/jarak, afirmasi, mutasi orang tua) belum punya cara seleksi sendiri. Padahal titik lokasi rumah sudah ada (T106/T112), jadi jarak bisa dihitung.
- **Alur verifikasi → peringkat → terima tidak terlihat:**
  - Hanya pendaftar **Diverifikasi** dan **Cadangan** yang masuk peringkat. Pendaftar Dikirim tidak tampil, tanpa penjelasan.
  - Menerima bisa dari detail pendaftar (satu per satu, mengabaikan kuota) atau dari Peringkat dan hasil (massal, mengikuti kuota). Dua jalan ini tidak dijelaskan.
  - Usulan diterima/cadangan diisi otomatis di dropdown, lalu "Tetapkan hasil". TU tidak yakin apakah usulan sudah tersimpan.

## Usulan
1. **Metode seleksi per jalur** menggantikan centang "memakai tes":

   | Metode | Urutan | Sumber nilai |
   |---|---|---|
   | Urutan daftar | waktu kirim paling awal | otomatis |
   | Tes seleksi | skor akhir tes (T119) | TU mengisi nilai tes |
   | Nilai rapor/prestasi | rata-rata nilai atau poin prestasi | calon siswa mengisi di formulir + unggah bukti, TU memverifikasi |
   | Jarak rumah | jarak terdekat ke sekolah | dari titik lokasi rumah (T112) dan titik sekolah |
   | Penilaian manual | urutan diatur TU | TU |

   Metode dipilih saat membuat jalur (T117), dengan penjelasan satu kalimat di setiap pilihan.
2. **Isian nilai di formulir publik** hanya untuk jalur "Nilai rapor/prestasi". Contohnya rata-rata rapor semester 1–5 atau daftar prestasi dengan tingkat (kab/prov/nas), ditambah dokumen bukti wajib. TU bisa mengoreksi nilai saat verifikasi.
3. **Halaman Peringkat menjelaskan dasarnya** di kepala tiap jalur, misalnya "Diurutkan berdasarkan jarak rumah terdekat; 3 pendaftar belum diverifikasi tidak ikut".
4. **Alur seleksi dalam 3 langkah,** tampil di halaman gelombang:
   1. Verifikasi berkas (jumlah menunggu);
   2. Peringkat (nilai/tes lengkap?);
   3. Tetapkan hasil (diterima / cadangan / ditolak, dengan pratinjau sebelum disimpan).

   Dari detail pendaftar ada tautan ke langkah yang relevan (T116).
5. **Verifikasi massal** dari daftar pendaftar (centang beberapa → Verifikasi), untuk berkas yang sudah dicek.

## Keputusan (2026-11-15)
- Dikerjakan dulu: **Urutan daftar, Tes seleksi, Nilai rapor/prestasi**, plus Penilaian manual. **Jarak rumah menyusul** (butuh titik lokasi sekolah dan aturan zonasi per daerah).
- Bobot gabungan (rapor + tes) belum dikerjakan.

## Kriteria selesai
- [ ] Setiap jalur menyatakan metode seleksinya, dan halaman peringkat menjelaskan dasar urutan
- [ ] Jalur nilai rapor/prestasi bisa diisi calon siswa dan diverifikasi TU
- [ ] TU bisa melihat alur verifikasi → peringkat → hasil dan jumlah di tiap langkah
