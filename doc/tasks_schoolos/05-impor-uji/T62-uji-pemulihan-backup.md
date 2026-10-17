# T62 — Uji pemulihan backup

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T14
**Estimasi:** 3–4 jam

## Tujuan
Backup database benar-benar bisa dipulihkan, dan waktu pemulihannya diketahui sebelum pilot. Ini mitigasi R13.

## Langkah
1. Buat skrip `scripts/backup.sh`:
   - Jalankan `pg_dump` dengan format custom (`-Fc`).
   - Unggah ke R2 dengan kunci `backups/{tanggal}/schoolos.dump`.
   - Gunakan variabel `DATABASE_URL` dan kredensial R2 dari environment.
   - Jangan mencetak DSN ke log.
2. Jadwalkan backup harian sekali sehari di luar jam kerja (misalnya 01.00 WIB). Pakai scheduler yang tersedia di Railway atau job periodik River (T33).
3. Retensi: simpan 14 backup harian terakhir. Hapus yang lebih lama lewat skrip yang sama.
4. Uji pemulihan:
   - Ambil backup terbaru dari R2.
   - Pulihkan ke database kosong di Railway (instance terpisah).
   - Jalankan `GET /healthz/db`, dan kueri sederhana (jumlah siswa, jumlah pembayaran) untuk membandingkan dengan produksi.
   - Catat waktu mulai dan selesai.
5. Tulis langkah pemulihan di `docs/restore.md` yang bisa diikuti orang lain.

## File yang disentuh
- `scripts/backup.sh`
- `scripts/restore.sh`
- `docs/restore.md`

## Kriteria selesai
- [ ] Backup harian berjalan tiga hari berturut-turut tanpa gagal
- [ ] Backup bisa diunduh dari R2 dan dipulihkan ke database kosong
- [ ] Data hasil pemulihan sama dengan sumber untuk jumlah baris tabel utama
- [ ] Waktu pemulihan tercatat di `docs/restore.md`
- [ ] Tidak ada DSN atau kredensial di log

## Catatan
- Pemulihan yang belum pernah diuji dianggap belum ada. Ulangi uji ini setiap bulan selama pilot.
