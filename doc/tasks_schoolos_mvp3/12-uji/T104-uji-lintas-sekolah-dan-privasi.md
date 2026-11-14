# T104 — Uji lintas sekolah dan privasi MVP 3

**Fase:** 12 Uji
**Dependensi:** T85–T103
**Estimasi:** 1,5 hari

## Tujuan
Fitur pendaftaran dan Dapodik tidak membocorkan data antarsekolah, antarpendaftar, atau data sensitif ke peran yang tidak berhak.

## Langkah
1. Tambahkan semua route MVP 3 ke daftar uji T58 (lintas sekolah) dan T59 (orang tua).
2. Uji route publik:
   - Kode sekolah A tidak bisa membuka periode, pendaftar, atau status sekolah B.
   - Nomor pendaftaran sekolah B dengan tanggal lahir benar tetap ditolak di halaman sekolah A.
   - Rate limit form, lanjut draf, dan cek status bekerja.
3. Uji data sensitif:
   - NIK, No KK, dan no akta tidak muncul di log aplikasi saat simpan, impor, dan error validasi.
   - Audit log hanya memuat versi teredaksi.
   - `pg_dump` database uji tidak memuat NIK, No KK, atau no akta contoh dalam bentuk teks, termasuk di tabel pendaftar, batch impor, audit, dan job River.
   - Aplikasi tidak bisa membaca data jika kunci `PII_ENC_KEYS` diganti dengan kunci lain (gagal dengan error, bukan data salah).
   - `guru` dan `orang_tua` hanya melihat versi ter-mask atau mendapat 403.
   - Dokumen pendaftar tidak bisa dibuka tanpa login dengan peran yang benar, dan URL presign kedaluwarsa.
4. Uji beban ringan: 200 kiriman formulir dalam 5 menit tanpa nomor pendaftaran ganda.

## File yang disentuh
- `internal/testutil/` dan test lintas sekolah yang sudah ada

## Kriteria selesai
- [ ] Semua route MVP 3 ada di daftar T58/T59 dan lulus
- [ ] Pencarian `nik`/NIK contoh di output log uji tidak menemukan apa pun
- [ ] Uji beban formulir lulus tanpa duplikasi
