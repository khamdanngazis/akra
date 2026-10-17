# T57 — Impor idempoten

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T55
**Estimasi:** 3 jam

## Tujuan
Mengimpor file yang sama dua kali tidak membuat siswa ganda. Impor yang terputus bisa dilanjutkan tanpa mengulang baris yang sudah tersimpan.

## Langkah
1. Pastikan `students` punya constraint `UNIQUE (school_id, nis)` (T20). Upsert memakai `ON CONFLICT (school_id, nis) DO UPDATE`.
2. Untuk `guardians`, cari dulu dengan kombinasi nama dan telepon di sekolah yang sama. Jika cocok, pakai yang ada. Jika tidak, buat baru.
3. Untuk `enrollments`, gunakan `ON CONFLICT` pada unique partial index (T21). Jika enrollment aktif sudah ada di kelas yang sama, lewati. Jika di kelas berbeda, pindahkan lewat fungsi `Place` (T21).
4. Tandai di `import_rows` apakah baris ini membuat data baru atau memperbarui yang ada.
5. Batch yang sudah `selesai` tidak bisa dijalankan ulang. Pengguna harus membuat batch baru, yang tetap aman karena upsert.
6. Tampilkan di hasil: jumlah baru, diperbarui, dilewati.

## File yang disentuh
- `internal/importer/upsert.go`, `upsert_test.go`
- `internal/students/guardian_repo.go` (pencarian wali)
- `internal/importer/service.go`

## Kriteria selesai
- [ ] Impor file yang sama dua kali: impor kedua menghasilkan 0 siswa baru
- [ ] Impor kedua mencatat baris sebagai diperbarui, bukan gagal
- [ ] Wali yang sama tidak terduplikasi antar-impor
- [ ] Impor yang dihentikan di tengah dan dilanjutkan tidak membuat siswa ganda (diuji dengan menghentikan job di batch ke-2)
- [ ] Siswa yang pindah kelas lewat impor memiliki satu enrollment aktif

## Catatan
- Pencocokan wali dengan nama dan telepon bisa salah jika dua wali punya nama sama. Ini risiko yang diterima untuk MVP; sekolah diberi tahu bahwa wali duplikat bisa dirapikan manual.
