# T101 — Impor dari ekspor Dapodik

**Fase:** 11 Bantu Dapodik
**Dependensi:** T82, T85b, T86, T87, T100
**Estimasi:** 2 hari

## Tujuan
Sekolah yang sudah punya data di Dapodik bisa memindahkan data siswa lengkap ke SchoolOS dari file ekspor Dapodik, tanpa mengedit file lebih dulu.

## Latar belakang
- `ReadHeader` (`internal/importer/parser.go`) memakai baris pertama yang berisi sebagai header. Di ekspor Dapodik baris itu adalah judul "Daftar Peserta Didik", jadi header asli (baris 5–6) tidak terbaca.
- Header bergrup ("Data Ibu" → "Nama") membuat kolom "Nama" muncul empat kali.
- Mapping demo "Ekspor Dapodik" (`internal/demo/mvp2.go`) merujuk kolom "Nama Ibu Kandung" yang tidak ada di ekspor Dapodik.

## Langkah
1. Deteksi header: cari baris di 20 baris pertama yang paling banyak cocok dengan kolom yang dikenal. Jika baris berikutnya berisi sub-header di bawah sel gabungan, gabungkan menjadi `Grup / Sub` (mis. `Data Ibu / Nama`).
2. Perubahan ini berlaku untuk semua impor (T53/T82). File lama dengan header di baris 1 harus tetap terbaca sama.
3. Preset mapping bawaan "Ekspor Dapodik" untuk target siswa dan profil, mencakup semua kolom T100. Perbaiki mapping demo agar memakai `Data Ibu / Nama`.
4. Pencocokan siswa: NIPD = NIS, lalu NISN, lalu NIK (lewat blind index T85b). Siswa baru dibuat; siswa yang sudah ada diperbarui. NIK dan No KK dari file langsung dienkripsi; nilai utuh tidak disimpan di tabel batch impor atau laporan error (tampilkan ter-mask).
5. Pratinjau perbedaan sebelum disimpan: field kosong di SchoolOS langsung diisi; field yang sudah terisi dan berbeda ditampilkan berdampingan dan hanya ditimpa jika dicentang.
6. Label pilihan dari Dapodik diubah ke kode T85. Label yang tidak dikenal menjadi error per baris, bukan diam-diam dikosongkan.
7. Berjalan sebagai job (T55) dan idempoten (T57).

## File yang disentuh
- `internal/importer/parser.go`, `parser_test.go` (deteksi header)
- `internal/importer/dapodik_preset.go`, test
- `internal/demo/mvp2.go`

## Kriteria selesai
- [ ] File ekspor Dapodik asli (tanpa diedit) terbaca dengan header baris 5–6
- [ ] Semua uji impor lama (T53, T82) tetap lulus
- [ ] Impor ulang file yang sama tidak mengubah apa pun
- [ ] Field yang sudah diisi TU tidak tertimpa tanpa konfirmasi
- [ ] File hasil T100 terimpor 100% tanpa error

## Catatan implementasi
Dua bagian: (1) deteksi header importer umum (`ReadHeader`, `scanSheet`, `ReadTable`) untuk baris judul dan header bergrup, plus perbaikan mapping demo menjadi `Data Ibu / Nama`; (2) impor data rinci khusus di `/dapodik/impor` dengan pratinjau isi/konflik. File disimpan terenkripsi sementara di storage antara pratinjau dan terapkan. Data orang tua yang sudah ada tidak ditimpa. Impor tidak berjalan sebagai job River.
