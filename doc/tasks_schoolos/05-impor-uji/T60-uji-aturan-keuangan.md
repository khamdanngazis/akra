# T60 — Uji aturan keuangan

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T39, T38
**Estimasi:** 1–2 hari

## Tujuan
Aturan keuangan teruji dari sisi unit maupun database, termasuk kondisi bersamaan (concurrency).

## Langkah
1. Uji unit (sudah sebagian dibuat di T31 dan T39), lengkapi:
   - Pembulatan: semua nominal adalah `int64`. Uji bahwa tidak ada konversi ke float di jalur keuangan (cek dengan pencarian kode sederhana di CI).
   - Pembatalan di bulan berbeda ditolak.
   - Pembatalan di atas ambang tanpa persetujuan tidak mengubah status.
   - Pengaju dan penyetuju pembatalan tidak boleh orang yang sama.
2. Uji integrasi transaksi:
   - Dua pembayaran bersamaan untuk tagihan yang sama dengan total melebihi sisa: hanya satu yang berhasil. Gunakan `SELECT ... FOR UPDATE` pada baris tagihan, lalu verifikasi.
   - Pembayaran dan pembatalan bersamaan: hasil akhir status konsisten dengan urutan yang berhasil.
3. Uji nomor kuitansi bersamaan (dari T41): 20 pembayaran paralel menghasilkan 20 nomor berbeda dan berurutan tanpa celah.
4. Uji tagihan massal dua kali (dari T32): hasilnya tidak ada tagihan ganda.
5. Pastikan setiap uji integrasi berjalan di database kosong dengan migrasi terbaru.

## File yang disentuh
- `internal/billing/*_test.go`
- `internal/payments/*_test.go`
- `internal/payments/concurrency_test.go`

## Kriteria selesai
- [ ] Semua uji unit keuangan lulus
- [ ] Uji concurrency dua pembayaran hanya meloloskan satu yang melebihi sisa, dijalankan 50 kali tanpa gagal
- [ ] Uji nomor kuitansi paralel tidak menghasilkan duplikat
- [ ] Uji tidak ada float di jalur keuangan lulus
- [ ] Uji berjalan di CI

## Catatan
- Uji concurrency bisa sesekali gagal jika tidak deterministik. Jalankan dengan `-count=50` sebelum dianggap stabil.
