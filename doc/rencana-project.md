# Rencana MVP SchoolOS Indonesia

**Segmen awal:** sekolah swasta
**Pembayaran:** dicatat manual, termasuk transfer dengan bukti foto (tanpa payment gateway)
**Laporan:** beberapa tipe yang bisa difilter dan diekspor

---

## 1. Ringkasan

Dokumen ini membagi pengembangan menjadi dua tahap. MVP 1 berisi fitur yang hampir pasti dibutuhkan sekolah swasta dan dibangun lebih dulu. MVP 2 berisi fitur yang bergantung pada format dan kebiasaan sekolah, dan ruang lingkupnya ditentukan setelah wawancara dengan calon pengguna.

## 2. Peran Pengguna

| Peran | Akses |
|---|---|
| Admin/TU | Kelola tahun ajaran, kelas, data siswa, dan data orang tua |
| Guru | Input absensi untuk kelas yang diampu |
| Bendahara | Atur jenis tagihan, buat tagihan, catat pembayaran, lihat tunggakan dan kuitansi |
| Kepala sekolah | Lihat seluruh data dan laporan (hanya baca) |
| Orang tua | Lihat data anak, absensi, tagihan, dan riwayat pembayaran (hanya baca) |

---

## 3. MVP 1: Dibangun Dulu

### 3.1 Fitur

| Area | Isi |
|---|---|
| Master data | Tahun ajaran dan periode aktif, kelas dan wali kelas, data siswa (impor Excel dan input manual), data orang tua/wali yang terhubung ke siswa |
| Akun | Login email dan password, hak akses per peran, reset password |
| Absensi | Input harian per kelas (hadir, sakit, izin, alpa), rekap per siswa dan per kelas |
| Keuangan | Jenis tagihan (SPP, uang pangkal, kegiatan) dengan nominal dan jatuh tempo, tagihan massal per kelas, catat pembayaran (tunai atau transfer dengan bukti foto), status tagihan (belum bayar, sebagian, lunas, terlambat), kuitansi PDF, daftar tunggakan |
| Portal orang tua | Login terpisah, lihat profil anak, absensi, tagihan, dan riwayat pembayaran (hanya baca) |
| Laporan | Rekap kehadiran dan daftar tunggakan dengan filter dasar, ekspor Excel dan PDF |
| Impor dan ekspor | Impor siswa dan orang tua dari Excel dengan template baku, laporan kesalahan per baris, ekspor data siswa ke Excel |

### 3.2 Kriteria Selesai

- Admin mengimpor 200 siswa dari Excel tanpa kesalahan yang tidak tertangani.
- Guru mengisi absensi satu kelas dalam kurang dari 2 menit.
- Bendahara membuat tagihan massal untuk satu kelas dan mencatat pembayaran dengan bukti transfer.
- Kuitansi bisa dicetak sebagai PDF.
- Orang tua hanya bisa melihat data anaknya sendiri.
- Pembayaran tidak bisa dihapus, hanya dibatalkan dengan catatan (untuk jejak audit).

### 3.3 Estimasi

Sekitar 7–8 minggu.

---

## 4. Wawancara (Sela Setelah MVP 1)

Wawancara dilakukan setelah MVP 1 siap, dengan target 10–15 calon pengguna dari sekolah swasta. Tujuannya menentukan fitur MVP 2 berdasarkan pemakaian nyata, bukan asumsi.

### 4.1 Panduan per Peran

- **Kepala sekolah:** prioritas laporan, cara memantau kondisi sekolah.
- **TU:** proses pendaftaran siswa dan impor data yang sudah ada.
- **Guru:** format nilai, cara input absensi, kebutuhan jadwal.
- **Bendahara:** jenis tagihan, aturan denda, cara rekonsiliasi transfer.
- **Orang tua:** informasi yang paling dicari dan kanal komunikasi yang dipakai.

### 4.2 Pertanyaan Wajib

- Aplikasi apa yang sudah dipakai sekolah sekarang?
- Format rapor apa yang dipakai?
- Apakah sekolah bersedia membayar, dan berapa kisaran yang wajar?

---

## 5. MVP 2: Setelah Wawancara

Isi final ditentukan dari hasil wawancara. Kandidat awal:

- Nilai dan rekap nilai per mata pelajaran, sesuai format rapor sekolah
- Jadwal pelajaran
- Laporan yang bisa disesuaikan: pilih kolom, filter, urutkan, dan simpan konfigurasi
- Rekap pembayaran harian dan bulanan
- Pengumuman ke kelas tertentu atau semua orang tua
- Dukungan impor dari format yang dipakai sekolah

---

## 6. Di Luar MVP 1 dan MVP 2

- Payment gateway, virtual account, dan QRIS
- Notifikasi WhatsApp dan email
- Rapor Kurikulum Merdeka dan penilaian kompetensi
- Penerimaan murid baru
- Aplikasi mobile native
- Pembelajaran digital, tugas online, dan ujian online
- Laporan lintas sekolah
- Integrasi dengan sistem pemerintah

---

## 7. Alur Kerja Utama

1. **Awal tahun ajaran:** admin membuat tahun ajaran dan kelas, lalu mengimpor siswa dan orang tua.
2. **Tagihan bulanan:** bendahara membuat tagihan SPP untuk satu kelas atau semua kelas.
3. **Pembayaran:** bendahara mencatat pembayaran dan melampirkan bukti transfer, lalu mencetak kuitansi.
4. **Harian:** guru mengisi absensi kelasnya.
5. **Orang tua:** masuk ke portal untuk melihat data anaknya.

---

## 8. Tahapan

| Tahap | Estimasi | Hasil |
|---|---|---|
| 1. Fondasi dan master data | 2 minggu | Admin bisa login dan mengelola kelas dan siswa |
| 2. Absensi dan keuangan | 3–4 minggu | Guru dan bendahara bisa bekerja, kuitansi tercetak |
| 3. Portal dan laporan dasar | 2 minggu | Orang tua bisa login, laporan bisa diekspor |
| 4. Uji internal MVP 1 | 1 minggu | Daftar perbaikan sebelum dipakai sekolah |
| 5. Wawancara | 2–3 minggu | Daftar fitur MVP 2 yang sudah diprioritaskan |
| 6. MVP 2 dan pilot | 6–8 minggu | Pilot di 2–3 sekolah |

---

## 9. Catatan Risiko

Karena wawancara dilakukan setelah MVP 1, sebagian fitur MVP 1 mungkin perlu diubah, terutama bagian keuangan dan impor data. Untuk mengurangi risiko ini, sebaiknya 2–3 calon pengguna dari sekolah swasta melihat alur tagihan dan absensi (berupa prototipe atau demo) sebelum tahap 2 selesai.

---

## 10. Keputusan yang Masih Terbuka

- Teknologi yang akan dipakai
- Hosting dan kebijakan backup data
- Apakah pilot gratis atau berbayar