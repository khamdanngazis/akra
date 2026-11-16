# T136 — Cek kesiapan, cek perangkat, dan ujian latihan

**Fase:** 16 Persiapan Ujian
**Dependensi:** T133, T138
**Estimasi:** 2,5 hari

## Tujuan
Masalah ditemukan **sebelum** hari ujian, bukan saat 300 siswa sudah duduk di ruang.

## Langkah
1. **Cek kesiapan ujian** (pola checklist T117), menjadi langkah terakhir wizard dan tampil di detail ujian:
   - semua soal punya kunci; rumus valid; gambar terunggah;
   - jumlah soal sesuai aturan acak untuk setiap varian;
   - peserta ada di sesi; setiap peserta punya kartu atau kode;
   - pengawas ada di setiap sesi `di_ruang`;
   - durasi muat di jendela sesi; zona waktu benar;
   - (PTS/PAS) soal sudah ditelaah bila telaah aktif.

   Item merah memblokir **Jadwalkan**; item kuning hanya peringatan.
2. **Cek perangkat untuk peserta** `/u/cek` (tanpa login, bisa dibagikan ke siswa H-7):
   - versi browser dan dukungan fitur (service worker, layar penuh, audio);
   - kecepatan koneksi dan waktu muat contoh soal bergambar;
   - **tes suara** (T141);
   - rumus tampil benar.

   Hasil: ✓ siap / ⚠ saran ("Gunakan Chrome, bukan browser bawaan") / ✗ tidak didukung.
3. **Ujian latihan** (`purpose = latihan`):
   - sekolah otomatis mendapat paket latihan 5 soal (semua jenis);
   - guru/koordinator membuka untuk kelas tertentu tanpa token;
   - siswa mencoba antarmuka (ragu-ragu, grid nomor, offline, kumpulkan) sebelum ujian sungguhan.
   - **Disarankan wajib sekali** sebelum ujian online pertama; ditampilkan di cek kesiapan sebagai peringatan bila kelas belum pernah latihan.
4. **Gladi untuk pengawas:** mode latihan layar pengawas (T143) dengan peserta simulasi, agar guru mencoba token, reset, dan tambah waktu.

## File yang disentuh
- `internal/exam/readiness.go`, `readiness_test.go`, `device_check_handler.go`
- `web/templates/exam/readiness.html`, `device_check.html`
- `internal/exam/seed_practice.go`

## Kriteria selesai
- [ ] Setiap item cek kesiapan diuji (lolos dan gagal)
- [ ] Item merah memblokir penjadwalan
- [ ] Cek perangkat mendeteksi browser yang tidak didukung (uji UA) dan mengukur koneksi
- [ ] Ujian latihan bisa dibuka satu kelas dalam ≤ 3 klik
