# T46 — Portal: unduh kuitansi

**Fase:** 04 Portal dan Laporan
**Dependensi:** T41, T45
**Estimasi:** 3 jam

## Tujuan
Orang tua bisa mengunduh kuitansi untuk pembayaran anaknya sendiri, dan hanya itu.

## Langkah
1. Buat handler `GET /portal/pembayaran/{paymentID}/kuitansi`.
2. Alur pengecekan:
   - Ambil pembayaran dengan join ke `invoices` dan `enrollments`.
   - Cek bahwa `enrollment.student_id` ada di `student_guardians` untuk `guardianID` yang login.
   - Jika tidak lolos, kembalikan `404`.
3. Panggil fungsi PDF dari T41 (`receipt_pdf.go`). Fungsi yang sama dipakai bendahara dan orang tua, jadi isinya identik.
4. Header: `Content-Disposition: attachment; filename="kuitansi-{nomor}.pdf"`.
5. Catat unduhan ke audit log dengan `action = receipt.download_parent`, `user_id` orang tua.
6. Tambahkan tautan "Unduh kuitansi" di tabel riwayat pembayaran (T45).

## File yang disentuh
- `internal/portal/receipt_handler.go`
- `web/templates/portal/billing.html` (tautan)

## Kriteria selesai
- [ ] Orang tua mengunduh kuitansi anaknya dan PDF-nya sama dengan yang dicetak bendahara
- [ ] Mengubah `paymentID` ke pembayaran siswa lain menghasilkan `404`
- [ ] Mengubah `paymentID` ke pembayaran sekolah lain menghasilkan `404`
- [ ] Unduhan tercatat di audit log
- [ ] Kuitansi pembayaran yang dibatalkan tetap bisa diunduh dengan watermark (sesuai T41)

## Catatan
- Pengecekan kepemilikan dilakukan di handler dan di query. Jangan hanya di salah satunya.
