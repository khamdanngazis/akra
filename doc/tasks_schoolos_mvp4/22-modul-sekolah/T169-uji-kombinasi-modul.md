# T169 — Uji kombinasi modul dan dokumentasi

**Fase:** 22 Modul per Sekolah
**Dependensi:** T163–T168
**Estimasi:** 1,5 hari

## Latar belakang
Dengan 9 modul opsional, kesalahan biasanya muncul di sambungan antarmodul, bukan di modul itu sendiri. Contohnya tautan ke modul yang mati atau query modul mati di halaman lain.

## Langkah
1. **Uji kombinasi** di `cmd/server` (memakai registry route dan klien uji):
   - sekolah **inti saja**;
   - setiap **paket templat**;
   - **semua kecuali X** untuk setiap modul X.

   Untuk setiap kombinasi:
   - semua route GET yang boleh dibuka peran memberi 200 atau 404 "modul tidak aktif", tanpa 500;
   - tidak ada tautan di halaman yang menuju route modul mati (pemindai tautan);
   - menu dan beranda setiap peran sesuai.
2. **Harness visual** (T162): opsi `-modules` untuk memotret menu dan beranda paket Dasar dan Keuangan.
3. **Dokumentasi:**
   - `doc/panduan-ujian/README.md` dan panduan lain menyebut modul;
   - runbook: cara menangani "modul tidak aktif" dan permintaan aktivasi;
   - ADR 0002 menjadi "diterima" dengan keputusan final.

## Kriteria selesai
- [ ] Uji kombinasi lulus untuk semua kombinasi di atas (tanpa 500, tanpa tautan ke modul mati)
- [ ] Tangkapan layar menu dan beranda paket Dasar dan Keuangan diperiksa
- [ ] ADR 0002 diperbarui dengan keputusan final
