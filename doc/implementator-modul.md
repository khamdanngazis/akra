# Panduan Tim Implementator: Modul dan Paket Sekolah

Panduan ini untuk tim implementator AKRA yang menyalakan dan mematikan modul sekolah. Sekolah tidak bisa mengubah modulnya sendiri; admin TU dan kepala sekolah hanya melihatnya di **Pengaturan → Modul & Paket**. Latar belakang dan keputusan: [ADR 0002](adr/0002-modul-per-sekolah.md).

Untuk sekarang semua perubahan lewat CLI `akra-modul`. UI implementator menyusul dan akan memakai layanan yang sama (`module.Admin`), jadi aturannya tidak berubah.

## 1. Modul

| Kode | Nama | Isi | Catatan |
|---|---|---|---|
| `core` | Inti | Profil sekolah, tahun ajaran, kelas, siswa & wali, mapel, penugasan, pengaturan, impor siswa | Selalu aktif, tidak bisa diubah |
| `attendance` | Absensi | Absensi harian dan per mapel, rekap, laporan kehadiran | Tanpa modul ini, rapor tidak memuat ketidakhadiran |
| `schedule` | Jadwal | Jadwal pelajaran dan jadwal mengajar | |
| `grades` | Nilai & Rapor | Komponen, input, rekap, rapor PDF, impor nilai | Dibutuhkan untuk "Kirim ke rapor" dari ujian dan "Nilai ke Dapodik" |
| `billing` | Keuangan | Jenis tagihan, tagihan massal, pembayaran, kuitansi, tunggakan, audit keuangan | Tanpa modul ini, biaya formulir pendaftaran dicatat admin TU |
| `admission` | Pendaftaran | Periode, gelombang, formulir publik, pendaftar, tes, daftar ulang | Peran bendahara butuh keuangan atau pendaftaran |
| `dapodik` | Bantu Dapodik | Lembar salin, cek kesiapan, F-PD, impor/ekspor | |
| `exam` | Ujian Online | Bank soal sampai hasil | Pilihan `release`: `r1` tes seleksi, `r2` ulangan guru, `r3` PTS/PAS |
| `announcements` | Pengumuman | Pengumuman staf dan orang tua | |
| `portal` | Portal Orang Tua | Akun wali, aktivasi, halaman anak | Tanpa modul ini, link aktivasi wali tidak berlaku |

**Ketergantungan wajib:** saat ini semua modul hanya bergantung pada Inti.

**Ketergantungan opsional:** modul lain tetap jalan, hanya fiturnya hilang. Contohnya, tanpa Nilai, tombol "Kirim ke rapor" di ujian tidak tampil. Daftar lengkapnya ada di ADR 0002 bagian 4.

## 2. Paket

Paket adalah templat. Setelah paket dipasang, modul tetap bisa diubah satu per satu.

| Kode | Nama | Modul (selain Inti) | Untuk |
|---|---|---|---|
| `dasar` | Paket Dasar | absensi, jadwal, pengumuman, portal | Sekolah yang mulai digital |
| `akademik` | Paket Akademik | Dasar + nilai, dapodik | Operator yang mengurus rapor dan Dapodik |
| `keuangan` | Paket Keuangan | keuangan, pengumuman, portal | Yayasan yang hanya perlu SPP dan tagihan |
| `lengkap` | Paket Lengkap | semua modul, termasuk pendaftaran dan ujian | Sekolah swasta dengan SPMB dan ujian online |

**Sekolah baru:**
- `akra-seed` memasang paket Dasar, kecuali diberi `-package`;
- `akra-demoseed -package <kode>` memasang paket di sekolah demo; tanpa `-package`, semua modul aktif kecuali ujian.

**Sekolah yang sudah ada sebelum T163** mendapat semua modul kecuali ujian. Label paketnya kosong ("tanpa paket").

## 3. Perintah umum

Semua perintah dijalankan di server aplikasi, mis. `railway ssh --service akra -- akra-modul ...`. Koneksi dari `DATABASE_URL`. **`-note` wajib untuk setiap perubahan.**

```sh
# Lihat status satu sekolah
akra-modul -school sd-harapan

# Nyalakan atau matikan satu modul (beberapa sekolah dipisah koma)
akra-modul -school sd-a,sd-b -module billing -on -note "kontrak 2027"
akra-modul -school sd-a -module dapodik -off -note "permintaan kepala sekolah, tiket #123"

# Modul dengan pilihan: ujian tahap 2 (ulangan guru)
akra-modul -school sd-a -module exam -on -option release=r2 -note "pilot ulangan"
# Hanya ubah pilihan, status modul tetap
akra-modul -school sd-a -module exam -option release=r3 -note "siap PTS"

# Pasang paket untuk satu gelombang sekolah
akra-modul -file gelombang-2.txt -package lengkap -note "gelombang 2"

# Daftar sekolah × modul dan paket, atau satu modul dengan pilihannya
akra-modul -list
akra-modul -list -module exam
```

**Aturan perintah:**
- Satu perintah adalah satu transaksi. Bila satu sekolah gagal (kode salah atau ditolak penjaga), tidak ada sekolah yang berubah.
- Berkas `-file` berisi satu kode sekolah per baris; baris kosong dan baris diawali `#` diabaikan.
- Bentuk lama `-exam on|off -release rN` masih diterima, tetapi tetap butuh `-note`.
- Pengguna sekolah melihat perubahan tanpa login ulang, paling lama 30 detik.

## 4. Penjaga

Saat mematikan modul, `akra-modul` memeriksa apakah modul sedang dipakai:

| Modul | Ditolak bila | Bisa dipaksa? |
|---|---|---|
| Ujian Online | ada sesi ujian berlangsung atau mulai dalam 24 jam | Tidak. Tunggu sesi selesai atau batalkan dulu |
| Pendaftaran | ada periode pendaftaran berstatus dibuka | Tidak. Tutup periodenya dulu |
| Keuangan | ada tagihan belum lunas | Ya, dengan `-force`. Tagihan tetap tersimpan, tetapi orang tua tidak melihatnya lagi |

**Ketentuan lain:**
- Paket juga melewati penjaga: memasang paket yang mematikan pendaftaran saat periodenya dibuka akan ditolak seluruhnya.
- **Data tidak pernah dihapus.** Mematikan modul hanya menyembunyikan menu dan menutup halamannya. Data muncul lagi saat modul dinyalakan kembali.
- **Peran dan tugas:** pengguna yang sudah ada tidak diubah. Peran bendahara baru hanya bisa diberikan (`akra-seed -role bendahara`) bila keuangan atau pendaftaran aktif. Tugas koordinator ujian hanya ditawarkan bila ujian aktif.

## 5. Membaca audit

Setiap perubahan tercatat di `audit_logs` sekolah itu sebagai aksi sistem:

| Aksi | Isi `after` |
|---|---|
| `module.<kode>`, mis. `module.billing` | `enabled`, `options`, `note`, `by` (mis. `akra-modul (budi)`) |
| `module.package` | `package`, `note`, `by` |

`before` berisi status sebelumnya. Contoh query:

```sql
SELECT created_at, action, before, after
FROM audit_logs
WHERE school_id = (SELECT id FROM schools WHERE code = 'sd-a') AND action LIKE 'module.%'
ORDER BY created_at DESC;
```

Status terakhir per modul juga ada di `school_modules` (`changed_at`, `changed_by`, `note`).
