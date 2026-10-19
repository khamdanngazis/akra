# T80 — Rekap pembayaran harian dan bulanan

**Fase:** 08 Laporan dan Impor
**Dependensi:** T34, T38, T39
**Estimasi:** 1–2 hari

## Tujuan
Bendahara dan kepala sekolah bisa melihat total pembayaran per hari atau per bulan, dibagi metode, dengan pembayaran yang dibatalkan dikecualikan dari total.

## Syarat mulai
Definisi "harian" sudah diputuskan: tanggal bayar (`paid_on`) atau tanggal rekening masuk. Default di task ini adalah `paid_on`.

## Langkah
1. Buat `internal/payments/daily_report.go`:
   - `DailyTotals(ctx, schoolID, from, to)`: satu baris per tanggal dengan jumlah tunai, transfer, total, dan jumlah transaksi.
   - `MonthlyTotals(ctx, schoolID, year)`: satu baris per bulan dengan kolom yang sama.
2. Pembayaran yang sudah disetujui pembatalannya tidak dihitung (T39). Pembayaran yang masih menunggu persetujuan tetap dihitung, dan kolom terpisah menampilkan jumlahnya.
3. Total harus cocok dengan jumlah `ActivePaid` untuk setiap bulan.
4. Route `GET /keuangan/rekap-pembayaran?mode=harian|bulanan&dari=&sampai=`. Akses: `bendahara`, `kepala_sekolah`.
5. Ekspor Excel dengan pola T47.

## File yang disentuh
- `internal/payments/daily_report.go`, `daily_report_test.go`
- `internal/payments/report_handler.go`
- `web/templates/payments/rekap.html`

## Kriteria selesai
- [ ] Total bulanan sama dengan jumlah seluruh total harian bulan itu
- [ ] Pembayaran yang dibatalkan tidak masuk total
- [ ] Pembayaran menunggu persetujuan terlihat di kolom terpisah
- [ ] Guru mendapat `403`
- [ ] Rekap satu tahun untuk 300 siswa selesai di bawah 2 detik

## Catatan
- Angka ini harus sama dengan laporan keuangan untuk yayasan. Jika ada perbedaan, cari penyebabnya sebelum rilis.
