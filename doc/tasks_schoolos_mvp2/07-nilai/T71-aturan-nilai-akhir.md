# T71 — Aturan nilai akhir (unit test)

**Fase:** 07 Nilai
**Dependensi:** T70
**Estimasi:** 3 jam

## Tujuan
Nilai akhir per mapel dihitung dengan satu fungsi murni yang mengikuti aturan sekolah. Semua tampilan dan rekap memakai fungsi ini.

## Syarat mulai
Rumus pembulatan dan skala huruf harus dikonfirmasi dari wawancara. Dokumen ini memakai aturan contoh, bukan keputusan final.

## Langkah
1. Buat `internal/grades/final.go`:
   ```go
   func FinalScore(components []ComponentScore) (Result, error)
   ```
   - Input: daftar `{weight_percent, score (nullable)}`.
   - Jika ada komponen aktif yang `score` kosong, hasil `Incomplete` dan tidak ada nilai akhir.
   - Hitung dengan bilangan bulat dalam satuan 1/100 untuk menghindari float: `sum(score*100 * weight) / 100`, lalu bulatkan dengan aturan tetap.
2. Pembulatan: setengah ke atas (`x.5` menjadi bilangan bulat berikutnya). Simpan ke satu tempat agar mudah diubah.
3. Skala huruf: fungsi terpisah `Letter(score, scale)` dengan tabel batas dari `school_settings`.
4. Uji (`final_test.go`):
   - Tiga komponen dengan bobot 30, 30, 40 dan nilai 80, 90, 70 menghasilkan 79.
   - Satu komponen kosong menghasilkan `Incomplete`.
   - Nilai di batas skala (misalnya tepat 80) masuk huruf yang benar.
   - Total bobot tidak 100 menghasilkan error.
5. Uji bahwa paket `grades` tidak mengimpor `float64` untuk perhitungan.

## File yang disentuh
- `internal/grades/final.go`, `letter.go`
- `internal/grades/final_test.go`, `letter_test.go`

## Kriteria selesai
- [ ] Semua kasus uji lulus
- [ ] Hasil identik untuk input yang sama, dijalankan 1000 kali
- [ ] Nilai tidak lengkap tidak menghasilkan angka parsial
- [ ] Tidak ada float dalam jalur perhitungan (dicek dengan pencarian kode di CI)

## Catatan
- Pembulatan ini sensitif. Jika sekolah berbeda pendapat, cukup ubah satu fungsi dan uji ulang.
