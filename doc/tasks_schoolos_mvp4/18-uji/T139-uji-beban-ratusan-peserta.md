# T139 — Uji beban ratusan peserta

**Fase:** 18 Uji
**Dependensi:** T130–T132 (lanjutan dari T61 uji beban ringan)
**Estimasi:** 2 hari

## Tujuan
Membuktikan AKRA di Railway sanggup melayani ratusan peserta ujian bersamaan, sebelum dipakai sekolah sungguhan.

## Skenario
1. **500 peserta** satu sekolah login dalam rentang 2 menit, lalu memuat soal (40 soal, 10 bergambar).
2. Setiap peserta menyimpan jawaban rata-rata tiap 20 detik selama 90 menit (≈ 25 request/detik), dengan lonjakan di awal dan menit akhir.
3. Semua peserta mengumpulkan dalam 1 menit terakhir; job kumpul otomatis berjalan bersamaan.
4. Pengawas di 15 sesi membuka halaman awas dengan polling tiap 10 detik.
5. Diulang dengan **2 sekolah** bersamaan (1.000 peserta) untuk melihat batas.

## Langkah
1. Skrip uji beban (k6 atau Go) di `scripts/loadtest/`, menjalankan alur peserta yang sebenarnya (login, mulai, simpan, kumpulkan).
2. Data uji dibuat oleh seed khusus. Uji dijalankan di lingkungan staging, **tidak** di database produksi.
3. Target:
   - p95 simpan jawaban < 300 ms;
   - p95 muat soal < 1,5 detik;
   - 0 jawaban hilang (dibandingkan dengan log klien);
   - CPU/memori layanan dan koneksi Postgres di bawah batas paket Railway.
4. Bila tidak tercapai, perbaiki dulu:
   - indeks;
   - ukuran pool `pgx`;
   - gabung simpan jawaban;
   - cache payload soal per attempt;
   - gambar soal disajikan dari R2/CDN, bukan lewat server aplikasi.
5. Catat hasil dan rekomendasi kapasitas (peserta per sekolah per paket Railway) di `doc/uji-mvp4.md`.

## Kriteria selesai
- [ ] Skenario 500 peserta lulus target di staging
- [ ] Tidak ada jawaban hilang di semua skenario
- [ ] Rekomendasi kapasitas tertulis
