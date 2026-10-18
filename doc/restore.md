# Backup dan pemulihan database AKRA

Panduan ini untuk siapa pun yang perlu memulihkan database AKRA dari backup, atau menguji bahwa backup bisa dipulihkan. Ini mitigasi risiko R13.

**Pemulihan yang belum pernah diuji dianggap belum ada. Ulangi uji pemulihan setiap bulan selama pilot** dan catat hasilnya di bagian [Riwayat uji](#riwayat-uji).

## Ringkasan

| | |
|---|---|
| Alat | `akra-backup` ([cmd/backup](../cmd/backup/main.go)), dibungkus [scripts/backup.sh](../scripts/backup.sh) dan [scripts/restore.sh](../scripts/restore.sh) |
| Image | [Dockerfile.backup](../Dockerfile.backup): `postgres:18-alpine` + `akra-backup` |
| Format | `pg_dump --format=custom --no-owner --no-privileges` (terkompresi) |
| Lokasi | R2, bucket privat aplikasi, kunci `backups/{tanggal WIB}/schoolos.dump` |
| Jadwal | setiap hari 01.00 WIB (service cron Railway `akra-backup`, `0 18 * * *` UTC) |
| Retensi | 14 backup harian terbaru (`BACKUP_RETENTION`); yang lebih lama dihapus oleh `run` |
| Isi | seluruh database: data sekolah, keuangan, audit, sesi login, dan antrean job River |

`pg_dump`/`pg_restore` harus **sama atau lebih baru** dari versi server. Railway memakai Postgres 18, jadi image memakai `postgres:18-alpine`. Jika Postgres dinaikkan, naikkan juga tag di `Dockerfile.backup`.

DSN dan kredensial hanya dibaca dari environment, tidak pernah dari argumen. `akra-backup` meneruskannya ke `pg_dump`/`pg_restore` lewat variabel `PG*`, dan proses anak tidak mewarisi `DATABASE_URL` maupun `R2_*`. Log hanya berisi kunci, ukuran, dan durasi. Jangan menjalankan perintah dengan DSN sebagai argumen (misalnya `psql postgres://...`), karena tersimpan di riwayat shell.

### Variabel environment

| Variabel | Dipakai | Isi |
|---|---|---|
| `DATABASE_URL` | `run`, `compare` | database sumber (produksi/staging) |
| `RESTORE_DATABASE_URL` | `restore`, `compare` | database tujuan, **harus kosong** |
| `R2_ACCOUNT_ID`, `R2_BUCKET`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` | semua | sama dengan service `akra` |
| `BACKUP_RETENTION` | `run` | jumlah backup harian (bawaan 14) |
| `R2_ENDPOINT` | uji saja | server S3 lain (mis. uji lokal); kosongkan di Railway |

### Perintah

```sh
akra-backup run                      # backup sekarang + retensi (dipakai cron)
akra-backup list                     # daftar backup di R2
akra-backup restore                  # backup terbaru -> RESTORE_DATABASE_URL (harus kosong)
akra-backup restore -key backups/2026-10-18/schoolos.dump
akra-backup compare                  # jumlah baris tabel utama: DATABASE_URL vs RESTORE_DATABASE_URL
restore.sh [-key ...]                # restore lalu compare (jika DATABASE_URL diisi)
```

`restore` menolak database tujuan yang sudah berisi tabel, dan memakai `--single-transaction`. Jika gagal di tengah, database tujuan tetap kosong.

## Menyiapkan backup harian di Railway

Satu kali, di project `natural-compassion`:

1. **Buat service.** Klik New → GitHub Repo → repositori AKRA, lalu beri nama service `akra-backup`.
2. **Atur Settings:**
   - Dockerfile path: `Dockerfile.backup`;
   - Cron Schedule: `0 18 * * *` (UTC = 01.00 WIB);
   - Restart policy: Never.
3. **Isi Variables** (pakai referensi agar tidak menyalin rahasia):

   ```
   DATABASE_URL=${{Postgres.DATABASE_URL}}
   R2_ACCOUNT_ID=${{akra.R2_ACCOUNT_ID}}
   R2_BUCKET=${{akra.R2_BUCKET}}
   R2_ACCESS_KEY_ID=${{akra.R2_ACCESS_KEY_ID}}
   R2_SECRET_ACCESS_KEY=${{akra.R2_SECRET_ACCESS_KEY}}
   BACKUP_RETENTION=14
   ```

4. **Pastikan tidak ada domain publik.** Service ini tidak butuh port.
5. **Jalankan sekali secara manual** untuk memastikan cron jalan: deploy pertama langsung menjalankan perintahnya; setelah itu ikuti jadwal.
6. **Cek log.** Harus muncul `"msg":"backup finished","key":"backups/<tanggal>/schoolos.dump","bytes":...`.
7. **Pantau tiga hari pertama:** pastikan ada tiga folder tanggal berturut-turut di `akra-backup list` atau dashboard R2.

Jika backup gagal, cron Railway menandai run itu gagal dan log berisi pesan `pg_dump`/R2 (password disamarkan `***`). Backup gagal tidak menghapus backup lama, karena retensi hanya berjalan setelah unggahan berhasil.

## Uji pemulihan (bulanan)

Tujuannya membuktikan backup terbaru bisa dipulihkan ke database **terpisah**, lalu mencatat waktunya. Produksi tidak disentuh.

1. **Siapkan database kosong.**
   - **Di Railway:** New → Database → PostgreSQL, beri nama mis. `Postgres-restore-uji`. Versinya harus sama dengan produksi (18).
   - **Atau di lokal:**

     ```sh
     docker run -d --name akra-restore -e POSTGRES_PASSWORD=<acak> -e POSTGRES_DB=akra -p 5434:5432 postgres:18-alpine
     ```

2. **Jalankan pemulihan dan pembandingan.**
   - **Di Railway:** jalankan sebagai one-off di service `akra-backup`. Tambahkan variabel sementara `RESTORE_DATABASE_URL=${{Postgres-restore-uji.DATABASE_URL}}`, ubah start command menjadi `restore.sh`, lalu deploy. Setelah selesai, kembalikan start command ke default (`backup.sh`) dan hapus `RESTORE_DATABASE_URL`.
   - **Atau dari laptop dengan Docker**, dengan variabel diisi dari `railway variables`. Untuk database Railway, aktifkan sementara TCP proxy publik di database tujuan:

     ```sh
     docker build -f Dockerfile.backup -t akra-backup .
     docker run --rm --env-file restore.env akra-backup restore.sh
     ```

     `restore.env` berisi `DATABASE_URL`, `RESTORE_DATABASE_URL`, dan `R2_*`. Jangan di-commit, dan hapus setelah selesai.
3. **Periksa hasilnya.** Log `restore finished` mencatat `started`, `finished`, dan `duration`. `compare` mencetak tabel jumlah baris dan diakhiri `Semua jumlah baris sama.`

   Jika sumbernya produksi yang sedang dipakai, selisih kecil di `audit_logs`, `payments`, atau `attendance_records` bisa wajar: itu data yang masuk setelah backup dibuat. Bandingkan dengan waktu backup.
4. **Cek aplikasi di atas database hasil pemulihan.**
   - Jalankan server AKRA dengan `DATABASE_URL` = database hasil pemulihan (`APP_ENV=development` di lokal).
   - Buka `GET /healthz/db` (harus 200), login sebagai bendahara, lalu buka Tunggakan.
5. **Catat hasilnya** di [Riwayat uji](#riwayat-uji): tanggal, kunci backup, ukuran, waktu mulai dan selesai, serta hasil `compare`.
6. **Bersihkan.**
   - Hapus database uji (`docker rm -f akra-restore`, atau hapus service Postgres uji di Railway).
   - Matikan TCP proxy jika sempat diaktifkan.

## Pemulihan sungguhan (database produksi rusak atau hilang)

Perkiraan waktu total: sekitar 30 menit, sebagian besar untuk langkah manual di dashboard. Pemulihan datanya sendiri butuh beberapa detik untuk ukuran pilot.

1. **Hentikan penulisan.** Scale service `akra` ke 0 replika, atau pasang halaman perawatan, supaya tidak ada data baru yang hilang di tengah proses.
2. **Tentukan backup yang dipakai.**
   - Jalankan `akra-backup list`. Biasanya dipakai yang terbaru.
   - Jika kerusakan sudah terjadi sebelum backup terbaru, pilih tanggal sebelum kerusakan dengan `-key`.
3. **Buat Postgres baru** di Railway dengan versi yang sama, lalu pulihkan ke sana (langkah 2 uji pemulihan, tanpa `compare` jika sumber sudah tidak ada).
   - Jangan memulihkan ke database lama yang rusak; `restore` menolak database yang tidak kosong.
4. **Sambungkan aplikasi.**
   - Ubah `DATABASE_URL` service `akra` ke database baru: `${{<nama Postgres baru>.DATABASE_URL}}`.
   - Ubah juga `DATABASE_URL` service `akra-backup`.
   - Deploy. Pre-deploy `akra-migrate up` akan menjalankan migrasi yang belum ada di backup.
5. **Verifikasi.**
   - `/healthz/db` harus 200.
   - Login sebagai admin dan bendahara.
   - Cek jumlah siswa dan pembayaran terakhir.
   - Cek nomor kuitansi berikutnya melanjutkan (`receipt_counters` ikut dipulihkan).
6. **Beri tahu sekolah.** Data setelah waktu backup (paling lama sekitar 24 jam) harus dimasukkan ulang, misalnya pembayaran dan absensi hari itu. Daftar pembayaran yang hilang bisa dicocokkan dari kuitansi kertas atau bukti transfer.
7. **Simpan database lama** sampai penyebab kerusakan diketahui, lalu hapus.

Sesi login ikut dipulihkan sesuai isi backup. Untuk memaksa semua pengguna login ulang setelah pemulihan, jalankan `DELETE FROM sessions;` di database baru.

## Riwayat uji

| Tanggal | Lingkungan | Data | Backup | Pemulihan | Hasil |
|---|---|---|---|---|---|
| 18 Okt 2026 | Lokal: Docker Postgres 18.6 sumber dan tujuan terpisah, image `Dockerfile.backup`, server S3 lokal | 37 MB (3 sekolah ukuran pilot dari T61: 900 siswa, 16.200 tagihan, 10.002 pembayaran, 63.000 absensi) | 2,9 MB dalam 1,4 dtk | mulai 08.16.39 UTC, selesai 08.16.41 UTC (**2,0 dtk**) | Semua 18 tabel utama dan versi migrasi (30) sama; `/healthz/db` 200; login dan halaman tunggakan berjalan; log tanpa DSN/kredensial |
| | Railway (`akra-backup` cron + Postgres terpisah) | | | | belum dijalankan |
