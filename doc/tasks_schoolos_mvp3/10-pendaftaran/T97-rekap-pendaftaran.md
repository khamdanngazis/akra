# T97 — Rekap pendaftaran

**Fase:** 10 Pendaftaran
**Dependensi:** T93
**Estimasi:** 1 hari

## Tujuan
Kepala sekolah dan TU bisa melihat perkembangan pendaftaran dan mengekspor datanya.

## Langkah
1. Halaman `GET /pendaftaran/rekap`: jumlah per status, per gelombang, per sekolah asal, per jenis kelamin, dan per hari (tren). Sisa kuota per periode.
2. Ekspor Excel daftar pendaftar memakai ekspor laporan yang sudah ada (`internal/reports/export`). NIK hanya ikut untuk `admin_tu`/`kepala_sekolah`.
3. Akses: `admin_tu`, `kepala_sekolah`.

## File yang disentuh
- `internal/admission/summary.go`, `summary_test.go`
- `web/templates/admission/summary.html`

## Kriteria selesai
- [ ] Angka rekap sama dengan jumlah baris di daftar pendaftar untuk filter yang sama
- [ ] Ekspor oleh peran tanpa hak NIK tidak memuat kolom NIK
