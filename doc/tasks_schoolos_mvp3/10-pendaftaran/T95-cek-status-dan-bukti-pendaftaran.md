# T95 — Cek status dan bukti pendaftaran

**Fase:** 10 Pendaftaran
**Dependensi:** T90, T93
**Estimasi:** 1 hari

## Tujuan
Orang tua bisa melihat status pendaftaran dan mengunduh bukti pendaftaran tanpa akun.

## Langkah
1. Route publik `GET/POST /s/{kode-sekolah}/status`: masuk dengan nomor pendaftaran + tanggal lahir calon siswa. Rate limit seperti login.
2. Tampilkan status, jalur, jadwal tes (T93a), catatan perbaikan (jika `perlu_perbaikan`), dokumen yang kurang, dan status bayar. Jangan tampilkan NIK atau data sensitif lain.
3. Bukti pendaftaran PDF (fpdf, seperti kuitansi T41): kop sekolah, nomor, nama, tanggal lahir, periode, tanggal kirim, daftar dokumen terunggah, QR atau kode verifikasi.
4. TU bisa mencetak bukti yang sama dari halaman detail.

## File yang disentuh
- `internal/admission/status_public_handler.go`
- `internal/admission/proof_pdf.go`, test

## Kriteria selesai
- [x] Nomor benar dengan tanggal lahir salah menampilkan pesan umum yang sama dengan nomor salah
- [x] PDF tidak memuat NIK atau No KK
- [x] 10 percobaan gagal dari IP yang sama dibatasi sementara
