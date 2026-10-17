# T53 — Parser Excel

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T52
**Estimasi:** 3 jam

## Tujuan
File Excel yang diunggah dibaca dengan aman. File yang salah format ditolak sebelum diproses lebih jauh.

## Langkah
1. Buat `internal/importer/parser.go`:
   - `ParseFile(r io.Reader) (Rows, error)` dengan `excelize`.
   - Baca sheet pertama saja.
2. Validasi file:
   - Ukuran maksimal 5 MB (`http.MaxBytesReader`).
   - Ekstensi `.xlsx` dan deteksi tipe ZIP di awal file (`PK\x03\x04`). Jangan hanya percaya ekstensi.
   - Jumlah baris data maksimal 5.000.
   - Header harus cocok dengan kolom baku (T52). Urutan boleh berbeda, tetapi nama kolom wajib ada.
3. Jika header tidak lengkap, kembalikan error yang menyebut kolom yang hilang, contoh: `Kolom "Telepon wali" tidak ditemukan`.
4. Baris kosong di tengah dilewati dan dicatat sebagai "dilewati" di laporan.
5. Setelah parsing, hasil disimpan sementara di `import_batches` (T55) dengan status `diunggah`.

## File yang disentuh
- `internal/importer/parser.go`, `parser_test.go`
- `internal/importer/handler.go` (unggahan)

## Kriteria selesai
- [ ] File dengan kolom `NIS` hilang ditolak dan pesan menyebut `NIS`
- [ ] File `.csv` yang diganti nama menjadi `.xlsx` ditolak
- [ ] File 6.000 baris ditolak dengan pesan batas
- [ ] File 4 MB diterima, file 6 MB ditolak
- [ ] Baris kosong di tengah dilewati, bukan dianggap error

## Catatan
- Parser tidak boleh menyimpan data ke `students` langsung. Validasi dan penyimpanan dipisah (T54 dan T55).
