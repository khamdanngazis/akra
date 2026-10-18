# T44 — Portal: detail siswa

**Fase:** 04 Portal dan Laporan
**Dependensi:** T43, T27
**Estimasi:** 3 jam

## Tujuan
Orang tua melihat profil anaknya, kelas aktif, dan rekap absensi. Akses ke siswa lain ditolak.

## Langkah
1. Buat handler `GET /portal/siswa/{id}`.
2. Di service `GetChildDetail(ctx, guardianID, studentID)`:
   - Cek `student_guardians` untuk pasangan `guardianID` dan `studentID`. Jika tidak ada, kembalikan `ErrNotFound`.
   - Jangan membedakan "siswa tidak ada" dengan "siswa bukan anak saya". Keduanya `404`.
3. Data yang ditampilkan:
   - Nama lengkap, NIS, tanggal lahir, jenis kelamin
   - Kelas dan wali kelas pada tahun aktif
   - Rekap absensi (memakai fungsi `SummaryByStudent` dari T27) untuk tahun ajaran aktif
4. Data yang tidak ditampilkan: status internal sekolah, catatan staf, dan data wali lain.
5. Template `web/templates/portal/child_detail.html`. Tautan di atas untuk kembali ke daftar anak.

## File yang disentuh
- `internal/portal/child_service.go`, `handler.go`
- `web/templates/portal/child_detail.html`

## Kriteria selesai
- [x] Orang tua membuka anaknya dan melihat data benar
- [x] Mengubah `studentID` di URL ke siswa lain menghasilkan `404`
- [x] Mengubah `studentID` ke siswa yang berasal dari sekolah lain menghasilkan `404`
- [x] Rekap absensi di portal sama dengan rekap di T27 untuk siswa yang sama
- [x] Tidak ada tombol ubah data di halaman ini

## Catatan
- Respons `404` yang sama untuk semua kasus mencegah orang tua menebak id siswa lain (enumerasi).
