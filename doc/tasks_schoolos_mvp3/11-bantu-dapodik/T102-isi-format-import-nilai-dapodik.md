# T102 — Isi format import nilai Dapodik

**Fase:** 11 Bantu Dapodik
**Dependensi:** T71, T73, T86 (NISN)
**Estimasi:** 2,5 hari

## Tujuan
Nilai rapor di SchoolOS bisa dimasukkan ke Dapodik lewat fitur "Import dari Excel" milik Dapodik, tanpa mengetik nilai satu per satu.

## Latar belakang
Format import nilai Dapodik 2027 (`template-format-import-nilai.xlsx`, sheet "Format Import Nilai"):
- Baris 8–14: token mata evaluasi, mata evaluasi, tingkat, rombel, KKM pengetahuan (E12), KKM keterampilan (E13), rapor ke-.
- Header baris 16–17, data mulai baris 18: A No, B Token Nilai (tersembunyi), C Token Anggota Rombel, D Nama, E NIS, F NISN, G/H Pengetahuan Nilai/Predikat, I/J Keterampilan Nilai/Predikat, K/L Sikap Spiritual Predikat/Deskripsi, M/N Sosial Predikat/Deskripsi.
- Validasi: nilai 0–100, predikat A/B/C/D, sikap SB/B/C/K, deskripsi dibatasi panjangnya. Sheet diproteksi password.
- Token dibuat Dapodik per rombel, mapel, dan siswa, jadi file **tidak bisa dibuat dari nol**. Alurnya: unduh dari Dapodik → isi → impor ke Dapodik.

## Langkah
1. Halaman `GET/POST /nilai/dapodik`: TU atau guru mapel mengunggah file "Unduh Format Excel untuk Import" dari Dapodik.
2. Baca kepala file (mata evaluasi, rombel, rapor ke-) dan cocokkan ke mapel dan kelas di SchoolOS. Jika tidak yakin, minta pengguna memilih mapel dan kelas.
3. Cocokkan baris siswa lewat NISN, lalu NIS. Baris yang tidak cocok dilaporkan dan dibiarkan kosong.
4. Isi hanya sel G–N dan E12/E13:
   - Nilai pengetahuan dan keterampilan dari nilai akhir (T71), dibulatkan ke bilangan bulat.
   - Predikat dari aturan rapor (T73).
   - Sikap spiritual dan sosial hanya diisi untuk Bahasa Indonesia (aturan Dapodik).
   - Deskripsi dipotong sesuai batas validasi.
5. Ubah sel langsung di file asli, jangan membuat ulang workbook. Token, kolom tersembunyi, proteksi, dan validasi data harus tetap utuh.
6. Kembalikan file hasil untuk diunduh, plus ringkasan: berapa siswa terisi, siswa yang tidak cocok, nilai yang kosong.
7. Akses: `admin_tu` untuk semua mapel; `guru` hanya untuk mapel dan kelas yang diampunya (T65).

## File yang disentuh
- `internal/dapodik/grade_import_fill.go`, `grade_import_fill_test.go`
- `internal/dapodik/handler.go`
- `web/templates/dapodik/grade_fill.html`

## Kriteria selesai
- [ ] Token di kolom B dan C tidak berubah satu karakter pun (uji membandingkan sebelum dan sesudah)
- [ ] Proteksi sheet dan validasi data tetap ada setelah file diisi
- [ ] Sikap tidak terisi untuk mapel selain Bahasa Indonesia
- [ ] Guru tidak bisa mengisi file untuk mapel yang tidak diampunya
- [ ] File hasil berhasil diimpor ke Dapodik asli tanpa error (diuji di T105)

## Catatan
- Kolom Pengetahuan/Keterampilan mengikuti format Dapodik 2027. Untuk sekolah Kurikulum Merdeka, pastikan di T105 bagaimana Dapodik memetakan nilai dan deskripsi capaian (T74b) ke kolom ini.

## Catatan implementasi
Hanya nilai dan predikat pengetahuan (G/H) dan KKM (E12, jika kosong) yang diisi; keterampilan dan sikap dibiarkan karena AKRA belum menyimpannya terpisah. Predikat hanya ditulis jika huruf A–D. Diuji dengan template asli dari installer lewat `DAPODIK_GRADE_TEMPLATE` (file tidak di-commit): proteksi, validasi, dan kolom tersembunyi tetap utuh.
