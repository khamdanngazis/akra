# T145 — Keandalan, observabilitas, dan jendela deploy

**Fase:** 18 Hari-H
**Dependensi:** T139
**Estimasi:** 3 hari

## Tujuan
Tim AKRA tahu ada masalah **sebelum** sekolah menelepon, dan tidak pernah mengganggu ujian yang sedang berlangsung karena deploy.

## Langkah
1. **Metrik** (endpoint `/metrics` yang dilindungi, atau kirim ke layanan pemantau yang dipilih di ADR):
   - latensi dan error per endpoint peserta (login, buka ujian, simpan, kumpul);
   - jumlah attempt aktif per sekolah;
   - antrian job kumpul otomatis;
   - pool koneksi database.
2. **Alert** ke kanal tim (email/Telegram/Slack, dipilih di ADR):
   - error simpan > 1% selama 2 menit;
   - p95 simpan > 1 detik;
   - job kumpul otomatis tertunda > 3 menit;
   - koneksi database > 80%.
3. **Jendela deploy:**
   - endpoint internal `/internal/active-exams` mengembalikan jumlah sesi aktif dan sesi yang mulai dalam 30 menit;
   - skrip deploy/CI menolak deploy bila ada, kecuali deploy darurat dengan tanda eksplisit;
   - jadwal musim ujian sekolah-sekolah tampil di dasbor internal agar tim merencanakan rilis.
4. **Siap replika:**
   - tidak ada state di memori yang diperlukan antar-request (rate limit dan session sudah di database);
   - job River aman berjalan di beberapa replika;
   - diuji dengan 2 replika lokal di belakang load balancer.
5. **Graceful shutdown:**
   - saat restart, request simpan yang sedang berjalan diselesaikan (`http.Server.Shutdown` dengan batas waktu);
   - klien mengulang otomatis bila terputus (T139).
6. **Runbook insiden** di `doc/runbook-ujian.md`: server lambat, database penuh, R2 gangguan, deploy gagal saat ujian, sekolah melapor "tidak bisa login". Berisi langkah, pemilik, dan pesan untuk sekolah.
7. **Halaman status** sederhana yang bisa dibuka sekolah: normal / gangguan / pemeliharaan terjadwal.

## File yang disentuh
- `internal/platform/metrics/`, `internal/platform/httpserver` (shutdown), `cmd/server`
- `scripts/deploy-guard.sh`, workflow CI
- `doc/runbook-ujian.md`, `doc/adr/0001-modul-ujian.md` (pilihan pemantau)

## Kriteria selesai
- [ ] Alert terkirim pada simulasi error simpan (uji di staging)
- [ ] Deploy ditolak saat ada sesi aktif di staging
- [ ] Restart server saat 200 klien simulasi menyimpan: 0 jawaban hilang
- [ ] Dua replika berjalan bersamaan tanpa job ganda atau session hilang
