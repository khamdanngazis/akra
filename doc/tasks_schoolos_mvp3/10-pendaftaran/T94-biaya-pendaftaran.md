# T94 — Biaya pendaftaran

**Fase:** 10 Pendaftaran
**Dependensi:** T89, T93, T30, T34
**Estimasi:** 1 hari

## Tujuan
Jika periode punya biaya formulir, tagihan dibuat otomatis dan status bayarnya terlihat di daftar pendaftar.

## Keputusan
Diatur per periode lewat `fee_required_before_verify` (T89), 2026-11-14.

## Langkah
1. Tagihan dibuat saat status menjadi `dikirim` jika `fee_amount > 0`. Satu tagihan per pendaftar (idempoten).
2. Pendaftar belum menjadi siswa, jadi perlu cara menautkan tagihan ke pendaftar. Pilihan: kolom `applicant_id` pada tagihan dengan `student_id` boleh NULL, atau tabel tagihan pendaftaran terpisah. Pilih yang paling sedikit mengubah aturan billing yang sudah diuji (T60).
3. Pembayaran dicatat bendahara dengan alur yang sudah ada (bukti, kuitansi, pembatalan).
4. Jika periode memilih "wajib lunas", tombol `diverifikasi` nonaktif sampai lunas.
5. Saat pendaftar diterima (T96), tagihan dan pembayaran ikut tertaut ke siswa baru.

## File yang disentuh
- `internal/admission/fee.go`, `fee_test.go`
- `internal/billing/` (sesuai pilihan langkah 2)

## Kriteria selesai
- [ ] Kirim ulang setelah `perlu_perbaikan` tidak membuat tagihan kedua
- [ ] Semua uji aturan keuangan T60 tetap lulus
- [ ] Kuitansi biaya formulir bisa dicetak seperti kuitansi biasa
