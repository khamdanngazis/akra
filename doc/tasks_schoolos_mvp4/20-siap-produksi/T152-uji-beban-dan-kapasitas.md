# T152 — Uji beban dan perencanaan kapasitas

**Fase:** 20 Siap Produksi
**Dependensi:** T139, T140, T143, T145 (lanjutan dari T61)
**Estimasi:** 3 hari

## Tujuan
Membuktikan AKRA sanggup melayani ujian satu sekolah besar **dan** musim PTS/PAS serentak di banyak sekolah, lalu menetapkan kapasitas dan biaya infrastruktur.

## Skenario
| # | Skenario | Beban |
|---|---|---|
| S1 | Satu sekolah: login serentak setelah token dibacakan | 500 peserta login dalam 2 menit, lalu memuat 40 soal (10 bergambar) |
| S2 | Satu sekolah: mengerjakan | 500 peserta menyimpan tiap ±20 detik selama 90 menit; 15 pengawas polling tiap 10 detik |
| S3 | Kumpul serentak | 500 peserta kumpul dalam 1 menit terakhir, ditambah job kumpul otomatis |
| S4 | **Musim PAS** | 10 sekolah × 500 = **5.000 peserta** bersamaan, dengan jam mulai yang sama |
| S5 | Gangguan | Restart server dan 1 replika mati di tengah S2; jaringan klien 10% gagal acak |

## Langkah
1. Skrip uji beban (k6 atau Go) di `scripts/loadtest/`, menjalankan alur peserta sebenarnya: login, mulai, simpan (dengan `seq`), ping, kumpul. Klien mencatat setiap jawaban untuk dibandingkan dengan database.
2. Dijalankan di **lingkungan staging terpisah** dengan data seed. Tidak pernah di database produksi.
3. **Target:**
   - p95 simpan < 300 ms;
   - p95 buka ujian < 2 detik;
   - error < 0,1%;
   - **0 jawaban hilang**;
   - CPU < 70% dan koneksi database < 80% di S4.
4. Bila target tidak tercapai, terapkan perbaikan sesuai temuan:
   - indeks;
   - pool `pgx`;
   - batch simpan;
   - cache payload ujian per varian;
   - gambar langsung dari R2/CDN;
   - jumlah replika;
   - ukuran paket Postgres.
5. Tulis **rencana kapasitas** di `doc/uji-mvp4.md`: peserta bersamaan per konfigurasi Railway, biaya per bulan, kapan menambah replika, dan prosedur menaikkan kapasitas menjelang musim PAS.

## Kriteria selesai
- [ ] S1–S3 lulus target untuk satu sekolah
- [ ] S4 lulus dengan konfigurasi yang dicatat
- [ ] S5: 0 jawaban hilang, klien pulih otomatis
- [ ] Rencana kapasitas dan biaya tertulis
