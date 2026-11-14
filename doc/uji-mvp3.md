# Uji MVP 3 dengan Dapodik asli (T105)

Panduan ini untuk menguji fitur pendaftaran dan bantu Dapodik bersama operator Dapodik sekolah pilot. Bagian yang bisa diuji otomatis sudah tercakup di test (T104); yang di sini hanya bisa dibuktikan dengan aplikasi Dapodik 2027 sungguhan dan orang sungguhan.

Isi kolom **Hasil** dan **Catatan** saat menguji. Temuan dicatat sebagai task perbaikan.

## Persiapan

- [ ] Staging punya `PII_ENC_KEYS` dan `PII_INDEX_KEY` (lihat [restore.md](restore.md#kunci-enkripsi-data-pribadi)). Tanpa ini aplikasi menolak start di staging/production.
- [ ] Kunci disimpan juga di password manager tim.
- [ ] Laptop operator dengan Dapodik 2027 terpasang dan data sekolah pilot sudah tersinkron.
- [ ] Akun AKRA: admin_tu, kepala_sekolah, bendahara, satu guru mapel.

## 1. Import nilai ke Dapodik (T102)

| Langkah | Hasil | Catatan |
|---|---|---|
| Di Dapodik: menu nilai rapor → Export dan Import → "Unduh Format Excel untuk Import" untuk 2 mapel × 2 rombel | | |
| Di AKRA: Dapodik → Nilai ke Dapodik, unggah tiap file, pilih kelas dan mapel yang sesuai | | |
| Periksa ringkasan: jumlah terisi, siswa tidak ditemukan, nilai belum lengkap | | |
| Di Dapodik: "Import dari Excel" dengan file hasil. Catat semua pesan error Dapodik | | |
| Cek di Dapodik bahwa nilai pengetahuan dan predikat masuk sesuai | | |
| Sekolah Kurikulum Merdeka: catat bagaimana Dapodik memetakan nilai dan deskripsi capaian; apakah kolom pengetahuan saja cukup | | |

Catatan: AKRA hanya mengisi nilai dan predikat **pengetahuan** (dan KKM jika kosong). Keterampilan dan sikap belum disimpan terpisah di AKRA.

## 2. Lembar salin (T98)

| Langkah | Hasil | Catatan |
|---|---|---|
| Operator memasukkan minimal 10 siswa dari Dapodik → lembar salin, memakai "Salin berikutnya" (tombol pintas `n`) | | |
| Catat waktu rata-rata per siswa, bandingkan dengan input dari formulir kertas | | |
| Catat field yang urutan atau labelnya tidak sama dengan form Dapodik | | |
| Catat pilihan combobox yang tidak ditemukan saat label ditempel/diketik | | |
| Kode "Meninggal Dunia" (98) di pekerjaan orang tua: cocok dengan Dapodik? | | |

## 3. Formulir F-PD (T99)

| Langkah | Hasil | Catatan |
|---|---|---|
| Unduh F-PD terisi satu siswa dan satu kelas; operator dan wali kelas membandingkan dengan F-PD resmi | | |
| Cetak F-PD kosong; cek bisa diisi tangan | | |

## 4. Impor ekspor Dapodik (T101)

| Langkah | Hasil | Catatan |
|---|---|---|
| Di Dapodik: ekspor Daftar Peserta Didik sekolah pilot (file asli, tanpa diedit) | | |
| Di AKRA: Dapodik → Impor dari ekspor Dapodik; periksa pratinjau (cocok/baru/bermasalah) | | |
| Catat label pilihan yang ditolak "tidak dikenal" (berarti label di AKRA berbeda dengan Dapodik) | | |
| Terapkan; cek beberapa siswa di data rinci | | |
| Ekspor lagi dari AKRA (Ekspor format Daftar PD) dan bandingkan header dengan ekspor Dapodik | | |

## 5. Cek kesiapan (T103)

| Langkah | Hasil | Catatan |
|---|---|---|
| Bandingkan temuan Dapodik → Cek kesiapan dengan hasil validasi Dapodik untuk siswa yang sama | | |
| Catat aturan Dapodik yang belum dicek AKRA, dan peringatan AKRA yang tidak relevan | | |

## 6. Pilot pendaftaran (T89–T97)

| Langkah | Hasil | Catatan |
|---|---|---|
| Buat periode, jalur (satu dengan tes), tes seleksi, biaya formulir | | |
| Minimal 20 pendaftar (online lewat `/s/{kode}/daftar` dan input TU), termasuk unggah dokumen | | |
| Verifikasi, minta perbaikan satu pendaftar, orang tua memperbaiki dengan kode lanjut | | |
| Bendahara mencatat biaya formulir dan mencetak kuitansi | | |
| Isi nilai tes, tetapkan hasil lewat peringkat | | |
| Orang tua cek status dan unduh bukti pendaftaran | | |
| Jadikan siswa: cek NIS, data rinci, orang tua (kakak-adik berbagi wali), kelas | | |
| Rekap dan ekspor daftar pendaftar | | |

## Kriteria selesai

- [ ] File hasil T102 diterima Dapodik tanpa error
- [ ] Input 10 siswa dengan lembar salin lebih cepat daripada dari formulir kertas
- [ ] Semua perbedaan urutan atau label sudah dicatat
- [ ] Pilot pendaftaran selesai sampai siswa punya NIS dan profil lengkap

## Hasil

_Diisi setelah uji._
