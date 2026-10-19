# T82 — Impor dengan mapping (siswa dan nilai)

**Fase:** 08 Laporan dan Impor
**Dependensi:** T81, T55, T70
**Estimasi:** 2 hari

## Tujuan
File dari format sekolah bisa diimpor lewat mapping, untuk data siswa dan untuk nilai.

## Langkah
1. Di alur impor T53 dan T54, tambah langkah "pilih mapping" sebelum validasi.
2. Setelah mapping diterapkan, baris diubah ke kolom baku, lalu masuk validasi T54 yang tidak berubah.
3. Untuk target `grades`:
   - Kolom: NIS, kode mapel, nama komponen, nilai.
   - Baris diverifikasi terhadap penugasan (T65) dan komponen aktif (T69). Baris di luar itu menjadi error.
   - Nilai di luar 0–100 menjadi error. Kosong disimpan sebagai `NULL`.
4. Impor nilai memakai batch dan idempotensi seperti T57.
5. Batas file tetap 5.000 baris.

## File yang disentuh
- `internal/importer/apply_mapping.go`, `apply_mapping_test.go`
- `internal/importer/service.go` (langkah tambahan)
- `internal/importer/grades_import.go`

## Kriteria selesai
- [ ] File dengan header berbeda dan mapping yang benar menghasilkan data identik dengan file baku
- [ ] Impor nilai yang melewati penugasan guru menghasilkan error per baris
- [ ] Impor ulang nilai yang sama tidak menduplikasi nilai (`UNIQUE`)
- [ ] Impor 1.000 baris nilai selesai lewat job tanpa membuat halaman macet

## Catatan
- Impor nilai harus mengikuti aturan pengisian nilai yang sama dengan T70. Jangan membuat jalur lain yang melewatinya.
