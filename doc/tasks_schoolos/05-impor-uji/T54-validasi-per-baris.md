# T54 — Validasi per baris

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T53
**Estimasi:** 1 hari

## Tujuan
Setiap baris divalidasi sendiri. Baris salah tidak menghentikan baris lain, dan setiap kesalahan punya pesan yang bisa dipahami admin.

## Langkah
1. Buat `internal/importer/validate.go`:
   - Fungsi `ValidateRow(row, ctx) (ValidRow, []RowError)`.
   - Aturan:
     - NIS wajib, dinormalisasi (trim). Tidak boleh duplikat di file yang sama.
     - Nama wajib, maksimal 150 karakter.
     - Tanggal lahir, jika diisi, harus format `YYYY-MM-DD` dan tidak di masa depan.
     - Jenis kelamin, jika diisi, `L` atau `P`.
     - Kelas wajib dan harus ada di tahun ajaran aktif sekolah.
     - Email wali, jika diisi, harus format email.
     - Telepon wali dinormalisasi dengan aturan T23.
2. Cek NIS yang sudah ada di database sekolah (T20). Tandai sebagai "akan diperbarui", bukan error.
3. Simpan hasil ke `import_rows`: nomor baris asli, data mentah (JSON), status (`valid`, `error`, `dilewati`), dan daftar pesan error.
4. Ringkasan: jumlah valid, error, dan diperbarui.
5. Validasi tidak menulis ke `students`, `guardians`, atau `enrollments`.

## File yang disentuh
- `internal/importer/validate.go`, `validate_test.go`
- `db/migrations/0000XX_import_rows.up.sql` dan `.down.sql`

## Kriteria selesai
- [ ] File 200 baris dengan 3 baris salah menghasilkan 197 valid dan 3 error
- [ ] Setiap error menyebut nomor baris Excel asli (bukan indeks internal)
- [ ] NIS duplikat dalam file yang sama: baris kedua error
- [ ] NIS yang sudah ada di database: baris ditandai "diperbarui"
- [ ] Kelas tidak ditemukan: error dengan nama kelas yang dicari
- [ ] Setelah validasi, tabel `students` tidak berubah

## Catatan
- Angka "197 dan 3" di kriteria adalah contoh uji. Gunakan file uji yang disimpan di repositori.
