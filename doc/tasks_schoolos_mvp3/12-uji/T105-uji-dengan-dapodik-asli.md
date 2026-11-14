# T105 — Uji dengan Dapodik asli dan pilot pendaftaran

**Fase:** 12 Uji
**Dependensi:** T98–T103, T104
**Estimasi:** 1,5 hari (di luar waktu pilot)

## Tujuan
Membuktikan bahwa bantuan Dapodik benar-benar bekerja di aplikasi Dapodik 2027, bersama operator sekolah pilot.

## Langkah
1. **Import nilai (T102):** unduh format dari Dapodik untuk minimal 2 mapel dan 2 rombel, isi lewat SchoolOS, impor kembali ke Dapodik. Catat semua pesan error Dapodik.
2. **Lembar salin (T98):** operator memasukkan minimal 10 siswa dari lembar salin. Catat waktu per siswa dibandingkan input dari kertas, dan field yang urutan atau labelnya tidak sesuai.
3. **F-PD (T99):** operator dan wali kelas memeriksa PDF terhadap formulir resmi.
4. **Impor ekspor Dapodik (T101):** impor file ekspor Daftar PD asli dari sekolah pilot.
5. **Cek kesiapan (T103):** bandingkan temuan T103 dengan hasil validasi Dapodik untuk siswa yang sama.
6. **Pilot pendaftaran:** satu periode pendaftaran nyata atau simulasi dengan minimal 20 pendaftar, sampai konversi menjadi siswa (T96).
7. Kurikulum Merdeka: pastikan pemetaan nilai di T102 untuk sekolah Merdeka.
8. Temuan dicatat sebagai task perbaikan; hasil uji ditulis di `doc/uji-internal.md`.

## Kriteria selesai
- [ ] File hasil T102 diterima Dapodik tanpa error
- [ ] Input 10 siswa dengan lembar salin lebih cepat daripada dari formulir kertas
- [ ] Tidak ada perbedaan urutan atau label yang belum dicatat
- [ ] Pilot pendaftaran selesai sampai siswa memiliki NIS dan profil lengkap

## Catatan implementasi
Panduan dan lembar hasil ada di `doc/uji-mvp3.md`.
