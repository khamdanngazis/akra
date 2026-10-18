# Uji internal MVP 1 (T63)

Catatan uji alur kerja utama dari awal sampai akhir di staging, sebelum pilot dengan sekolah.

## Lingkungan dan data

| | |
|---|---|
| Tanggal | 18 Oktober 2026 |
| Lingkungan | Staging Railway (`https://akra-production.up.railway.app`, `APP_ENV=staging`), commit `8ea5953` |
| Sekolah | **SD Uji Internal** (kode `uji-internal`). Fiktif, bukan data sekolah nyata |
| Akun | `admin@`, `bendahara@`, `kepala@`, `guru1@` (wali 1A), `guru2@` (wali 1B) `uji-internal.test`; domain `.test` tidak pernah menerima email. Password acak, disimpan di luar repo |
| Data | 200 baris siswa dari `internal/importer/testdata/impor-200.xlsx` (3 baris sengaja salah), kelas 1A dan 1B |
| Cara uji | Skrip HTTP yang meniru browser (login, token CSRF, unggah file), dijalankan berurutan per peran. Setiap langkah dicek dari isi halaman atau berkas. Hasil yang meragukan diperiksa ulang dengan membuka halamannya |

Uji ini dijalankan oleh pembuat fitur. Catatan T63 menyarankan penguji lain, jadi [langkah manual](#yang-masih-perlu-dicoba-manusia) di bawah sebaiknya dicoba orang lain sebelum pilot.

## Hasil per skenario

### 1. Awal tahun ajaran (admin/TU)

| Langkah | Hasil |
|---|---|
| Buat tahun ajaran 2026/2027, lalu aktifkan | ✓ |
| Buat kelas 1A dan 1B dengan wali kelas | ✓ |
| Unggah 200 siswa dari Excel | ✓ 197 siswa baru, 3 perlu diperbaiki |
| Halaman batch menampilkan baris yang salah | ✓ Baris 51 (kelas 1X tidak ada), 121 (tanggal 2019-13-01), 181 (NIS ganda) |
| Unduh laporan kesalahan (.xlsx) | ✓ 3 baris, kolom sama dengan template |
| Simpan (job latar belakang, karena lebih dari 50 baris) | ✓ 197 siswa baru, selesai dalam 3 detik |
| Daftar siswa kelas 1A terisi | ✓ 97 siswa |

### 2. Tagihan bulanan (bendahara)

| Langkah | Hasil |
|---|---|
| Buat jenis tagihan SPP Rp300.000, jatuh tempo tanggal 10 | ✓ |
| Tagihan massal Oktober 2026 kelas 1A (lewat pratinjau) | ✓ 97 tagihan dibuat (job latar belakang) |
| Tagihan massal Oktober 2026 semua kelas | ✓ 100 dibuat (1B), 97 dilewati (1A sudah ditagih) |
| Tidak ada tagihan ganda | ✓ |

### 3. Pembayaran (bendahara, kepala sekolah)

| Langkah | Hasil |
|---|---|
| Catat pembayaran tunai Rp100.000 | ✓ status tagihan *Sebagian* |
| Catat transfer Rp300.000 dengan rekening dan referensi | ✓ status *Lunas* |
| Unggah bukti transfer (PNG) | ✓ tersimpan di R2; dibuka lewat URL bertanda tangan |
| Cetak kuitansi PDF | ✓ |
| Ajukan pembatalan pembayaran tunai dengan alasan | ✓ menunggu persetujuan (ambang bawaan Rp0); status tagihan belum berubah |
| Kepala sekolah menyetujui pembatalan | ✓ tagihan kembali *Belum bayar*; pembayaran tetap tercatat dengan tanda *Dibatalkan* dan alasannya |
| Pembayaran tidak bisa dihapus | ✓ tidak ada aksi hapus. `DELETE`/`PUT` ditolak (403); tabel `payments` append-only di database (T60) |
| Jejak audit | ✓ pembayaran, pengajuan dan persetujuan pembatalan, unggah dan buka bukti, lengkap dengan pelaku, waktu, dan alasan |

### 4. Absensi (wali kelas)

| Langkah | Hasil |
|---|---|
| Wali kelas 1A login; `/absensi` langsung membuka kelasnya | ✓ |
| Isi absensi 97 siswa (3 tidak hadir), simpan | ✓ 94 hadir, 1 sakit, 1 izin, 1 alpa; buka + simpan 160 ms di server |
| Wali kelas 1B mencoba mengisi absensi 1A | ✓ ditolak (403) |
| Rekap kelas 1A (admin) | ✓ 96,9% kehadiran, angka sama dengan isian |

### 5. Portal orang tua

| Langkah | Hasil |
|---|---|
| Pasang email wali Siswa 002 (1A) lewat impor ulang satu baris | ✓ baris *diperbarui*; email wali terisi tanpa membuat wali ganda |
| Admin membuat akun orang tua dari halaman siswa | ✓ status *Menunggu aktivasi*; email aktivasi terkirim ke alamat penguji |
| Aktivasi dari email, login, lihat anak, absensi, tagihan, unduh kuitansi | menunggu aktivasi oleh penguji |

Kepemilikan data portal sudah diuji otomatis di T59 (setiap push di CI): anak sendiri 200; anak lain, sekolah lain, dan ID acak 404 dengan respons identik; route staf 403.

### 6. Laporan (admin/TU, bendahara)

| Langkah | Hasil |
|---|---|
| Rekap kehadiran 1A: Excel dan PDF | ✓ Excel: 94 / 1 / 1 / 1, total 96,9%, **sama dengan layar** |
| Tunggakan: Excel dan PDF | ✓ Rp58.800.000, 196 siswa, 196 tagihan, **sama dengan layar** (197 tagihan − 1 lunas; pembayaran tunai yang dibatalkan tidak dihitung) |

## Kriteria selesai MVP 1 (rencana bagian 3.2)

| Kriteria | Status | Catatan |
|---|---|---|
| Admin mengimpor 200 siswa dari Excel tanpa kesalahan yang tidak tertangani | ✓ | 3 baris salah ditampilkan per baris, bisa diunduh, dan diunggah ulang setelah diperbaiki (T56) |
| Guru mengisi absensi satu kelas dalam kurang dari 2 menit | ✓ sebagian | Server: 160 ms. Satu halaman, semua siswa bawaan *Hadir*, ada tombol *Tandai semua hadir*; guru cukup mengubah yang tidak hadir. **Waktu nyata oleh guru belum diukur** (lihat langkah manual) |
| Bendahara membuat tagihan massal untuk satu kelas dan mencatat pembayaran dengan bukti transfer | ✓ | |
| Kuitansi bisa dicetak sebagai PDF | ✓ | |
| Orang tua hanya bisa melihat data anaknya sendiri | ✓ otomatis | Diuji otomatis di T59. Uji manual di staging menunggu alamat email |
| Pembayaran tidak bisa dihapus, hanya dibatalkan dengan catatan | ✓ | Pembatalan wajib alasan; tercatat di audit |

Fitur MVP 1 (rencana 3.1) yang diperiksa terhadap aplikasi:

| Area | Status |
|---|---|
| Master data, akun dan reset password, absensi, keuangan, portal, laporan | ✓ ada |
| Impor siswa dan orang tua dari Excel dengan laporan kesalahan per baris | ✓ |
| **Ekspor data siswa ke Excel** | ✗ **belum ada, dan tidak ada task-nya** (temuan K-2) |

## Kendala

| No | Skenario | Langkah | Hasil | Prioritas |
|---|---|---|---|---|
| K-1 | Absensi | Rekap Absensi, tombol *Unduh Excel* | Tombol nonaktif bertuliskan "Unduh Excel (segera)" dengan keterangan "dikerjakan di T47". Ekspornya sudah ada di menu Laporan, tapi tombol ini tidak disambungkan. Pengguna bisa mengira fitur belum ada | **Sedang** |
| K-2 | Fitur 3.1 | Ekspor data siswa ke Excel | Tidak ada di aplikasi dan tidak ada task-nya. Tercantum di fitur MVP 1, tapi bukan kriteria selesai 3.2 | **Tinggi** |
| K-3 | Pembayaran | Halaman Audit Keuangan | Rincian memakai nama field teknis (`void_id`, `requires_approval`, `status pending`, UUID). Bisa dibaca, tapi tidak ramah untuk kepala sekolah | Rendah |
| K-4 | Portal | Aktivasi akun orang tua | Satu-satunya jalan aktivasi adalah email. Tidak ada cadangan (mis. tautan yang bisa disalin admin) jika email wali salah atau tidak masuk | Sedang |
| K-5 | Data | Data wali | Tidak ada halaman untuk mengubah data wali (nama, telepon, email). Satu-satunya cara adalah impor ulang (T57 melengkapi email yang kosong) | Sedang |
| K-6 | Awal tahun ajaran | Riwayat kelas siswa hasil impor | Tanggal masuk kelas tercatat sebagai tanggal impor (18 Oktober), bukan awal tahun ajaran (13 Juli). Tagihan tidak terpengaruh, karena tagihan massal memakai kelas aktif. Tapi untuk sekolah yang mulai memakai AKRA di tengah tahun, riwayat kelas tampak seolah siswa baru masuk | Rendah |

Tidak ada kendala **kritis**.

## Daftar perbaikan (backlog, urut prioritas)

1. **Tinggi.** Ekspor data siswa ke Excel (K-2): daftar siswa sesuai filter, kolom sama dengan template impor agar bisa dipakai ulang. Perlu task baru (mis. T64).
2. **Sedang.** Sambungkan tombol *Unduh Excel* di Rekap Absensi ke `/laporan/kehadiran.xlsx` dengan filter yang sama (K-1). Perubahan kecil.
3. **Sedang.** Cadangan aktivasi akun orang tua tanpa email (K-4).
4. **Sedang.** Halaman ubah data wali (K-5).
5. **Rendah.** Label ramah di halaman audit (K-3).
6. **Rendah.** Pilihan tanggal masuk kelas saat impor, dengan bawaan awal tahun ajaran jika impor di tengah tahun (K-6).

## Yang masih perlu dicoba manusia

1. **Portal orang tua di staging.** Akun wali Siswa 002 sudah dibuat dan email aktivasinya terkirim. Langkah berikutnya:
   - aktifkan dari email;
   - login dengan kode sekolah `uji-internal`;
   - pastikan hanya Siswa 002 yang tampil;
   - buka absensi (hari ini tercatat), tagihan Oktober (*Lunas*), dan unduh kuitansinya.
2. **Waktu isi absensi oleh guru sungguhan** di HP, untuk kelas berisi sekitar 30 siswa (target di bawah 2 menit).
3. **Tampilan di HP dan tablet sungguhan** (sisa kriteria redesain).
4. **Hasil uji ini dibahas dan disetujui** sebelum pilot (kriteria terakhir T63).

## Data uji di staging

Sekolah `uji-internal` dan isinya dibiarkan untuk uji manual di atas. Untuk menghapus, jalankan `go run ./cmd/loadseed -reset` (versi untuk kode `uji-internal` perlu disesuaikan), atau hapus lewat SQL dengan `session_replication_role = replica` seperti di `cmd/loadseed`.
