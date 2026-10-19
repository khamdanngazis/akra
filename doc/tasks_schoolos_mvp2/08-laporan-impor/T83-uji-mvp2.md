# T83 — Uji otorisasi dan aturan MVP 2

**Fase:** 08 Laporan dan Impor
**Dependensi:** T58, T59, T60, T67, T75, T78, T80, T82
**Estimasi:** 2 hari

## Tujuan
Semua route dan aturan baru MVP 2 teruji dengan cara yang sama seperti MVP 1.

## Langkah
1. Tambah ke `internal/testutil/routes.go` semua route baru dari fase 06–08 dengan peran yang diizinkan.
2. Tambah fixture di `fixtures.go`: mapel, penugasan, jadwal, komponen, nilai, konfigurasi laporan (pribadi dan bersama), dan mapping impor, untuk dua sekolah.
3. Uji lintas sekolah (T58) untuk semua route baru.
4. Uji orang tua (T59) untuk `/portal/siswa/{id}/jadwal` dan `/portal/siswa/{id}/nilai`.
5. Uji injeksi report builder: kunci kolom, operator, dan nilai filter yang tidak sah.
6. Uji perhitungan: `FinalScore` (T71), rekap pembayaran harian (T80), dan konsistensi antara keduanya.
7. Pastikan uji gagal jika route baru tidak punya entri di `routes.go` (aturan dari T58).

## File yang disentuh
- `internal/testutil/routes.go`, `fixtures.go`
- `internal/testutil/mvp2_auth_test.go`
- `internal/reports/builder/injection_test.go`

## Kriteria selesai
- [ ] Semua uji lintas sekolah lulus untuk route MVP 2
- [ ] Uji orang tua untuk jadwal dan nilai lulus
- [ ] Uji injeksi lulus
- [ ] Uji perhitungan lulus dan dijalankan di CI
- [ ] Route baru tanpa entri uji menyebabkan CI gagal

## Catatan
- Uji ini tidak menggantikan uji internal. Itu dilakukan di T84.
