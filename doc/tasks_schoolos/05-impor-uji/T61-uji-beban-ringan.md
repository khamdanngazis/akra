# T61 — Uji beban ringan

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T51
**Estimasi:** 1 hari

## Tujuan
Memastikan aplikasi tetap responsif saat beberapa pengguna bekerja bersamaan, dengan data yang mendekati kondisi pilot.

## Langkah
1. Siapkan data uji: 3 sekolah, masing-masing 300 siswa, 12 kelas, 6 bulan tagihan (sekitar 5.400 tagihan per sekolah), 10.000 pembayaran.
2. Pilih alat uji sederhana (misalnya `k6` atau `vegeta`). Catat versi yang dipakai.
3. Skenario:
   - 50 pengguna virtual membuka beranda dan daftar siswa dengan pencarian.
   - 10 pengguna membuka daftar tunggakan dengan filter.
   - 5 pengguna membuat ekspor rekap kehadiran (di bawah 5.000 baris).
   - Durasi 5 menit.
4. Catat:
   - waktu respons median dan p95 per halaman
   - jumlah error (status 5xx)
   - pemakaian CPU dan memori di Railway
5. Ulangi dengan 2x beban untuk mengetahui batas kasar.

## File yang disentuh
- `loadtest/scenarios.js` (atau berkas sejenis)
- `loadtest/README.md` (hasil dan cara menjalankan)

## Kriteria selesai
- [ ] Waktu respons median halaman biasa di bawah 500 ms
- [ ] Tidak ada error 5xx pada skenario dasar
- [ ] Hasil dan versi alat tercatat di `loadtest/README.md`
- [ ] Batas kasar kapasitas dicatat untuk keputusan skala nanti

## Catatan
- Uji ini tidak menggantikan uji produksi. Tujuannya menangkap masalah yang jelas, seperti query tanpa index.
- Jika ada query lambat, catat dan perbaiki sebelum uji internal (T63).
