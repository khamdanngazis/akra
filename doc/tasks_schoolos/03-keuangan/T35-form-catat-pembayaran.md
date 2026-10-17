# T35 — Form catat pembayaran

**Fase:** 03 Keuangan
**Dependensi:** T34, T31
**Estimasi:** 1–2 hari

## Tujuan
Bendahara bisa mencari siswa, memilih tagihan, dan mencatat pembayaran. Status tagihan diperbarui otomatis lewat fungsi di T31.

## Langkah
1. Buat `internal/payments/handler.go`:
   - `GET /keuangan/pembayaran/baru?siswa={id}`: form pencatatan.
   - `GET /keuangan/pembayaran/cari?q=`: pencarian siswa (dipakai Alpine.js untuk autocomplete, hasil berupa HTML partial).
   - `POST /keuangan/pembayaran`
2. Form berisi:
   - Siswa (pencarian)
   - Tagihan yang belum lunas milik siswa (daftar, dipilih satu atau lebih)
   - Nominal per tagihan, default sisa tagihan
   - Metode: tunai atau transfer
   - Jika transfer: rekening tujuan (dari daftar rekening yang diatur sekolah) dan nomor referensi
   - Tanggal bayar, default hari ini (Asia/Jakarta)
   - Catatan opsional
3. Validasi di service:
   - Jumlah nominal per tagihan tidak melebihi sisa.
   - Metode transfer wajib rekening.
4. Setelah `POST` berhasil:
   - Hitung ulang status tagihan dengan `ComputeStatus` (T31).
   - Simpan `payments` dan update `invoices.status` dalam satu transaksi.
   - Audit: `payment.create`.
5. Redirect ke halaman detail tagihan dengan pesan sukses dan tautan "Cetak kuitansi" (T41).
6. Pembayaran lebih dari satu tagihan dalam satu form diproses per tagihan dalam satu transaksi. Jika satu gagal, semua dibatalkan.
7. Pastikan token CSRF ada di form (T09).

## File yang disentuh
- `internal/payments/handler.go`, `service.go`
- `web/templates/payments/form.html`, `search_results.html`

## Kriteria selesai
- [ ] Pembayaran penuh mengubah status tagihan menjadi `lunas`
- [ ] Pembayaran sebagian mengubah status menjadi `sebagian`
- [ ] Dua pembayaran bertotal lebih dari sisa, pembayaran kedua ditolak dan pembayaran pertama tidak tersimpan (transaksi)
- [ ] Guru dan orang tua mendapat `403`
- [ ] Pencarian siswa dari sekolah lain tidak mengembalikan hasil
- [ ] Form berfungsi di layar 360px

## Catatan
- Rekening tujuan diatur di pengaturan sekolah. Tabel dan halamannya belum ada di MVP 1 dan perlu task tambahan kecil bila belum tersedia. Untuk sementara, pilihan bisa berupa teks bebas.
- Aturan selisih transfer (kurang atau lebih kecil dari tagihan) belum ditetapkan. Task ini hanya menolak nominal melebihi sisa.
