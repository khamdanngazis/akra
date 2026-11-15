# T113 — Masa simpan data pendaftar yang tidak diterima (UU PDP)

**Fase:** 13 Perbaikan
**Dependensi:** T85b, T90, T91, T93, T97
**Estimasi:** 1 hari

## Latar belakang
Data pendaftar yang ditolak, mengundurkan diri, atau tidak pernah mengirim formulir saat ini disimpan tanpa batas waktu, termasuk NIK/No KK (terenkripsi), alamat, data orang tua, dan dokumen KK/akta. UU No. 27 Tahun 2022 tentang Pelindungan Data Pribadi (UU PDP):
- menyebut penyimpanan dan penghapusan/pemusnahan sebagai bagian dari pemrosesan data pribadi;
- mewajibkan pengendali menghapus data pribadi yang tidak lagi diperlukan untuk tujuan pemrosesannya (Pasal 43);
- mengatur pemusnahan data pribadi (Pasal 44).

UU PDP tidak menetapkan jumlah hari. Masa simpan harus ditetapkan pengendali (sekolah) sesuai tujuannya dan diberitahukan kepada subjek data.

## Keputusan (2026-11-15)
Tujuan data pendaftar yang tidak diterima adalah seleksi, pengumuman, sanggahan, dan rekap. Setelah itu data tidak diperlukan lagi.

| Status pendaftar | Data pribadi dihapus otomatis |
|---|---|
| Draf (tidak pernah dikirim), Ditolak, Mundur | **90 hari setelah tanggal tutup periode** |
| Cadangan | 90 hari setelah tanggal tutup periode, tetapi **paling cepat 30 hari setelah tahun ajaran dimulai** (cadangan masih mungkin dipanggil sampai awal tahun ajaran) |
| Diterima, Dikirim, Diverifikasi, Perlu perbaikan | Tidak dihapus otomatis: masih diproses, atau datanya dipakai sebagai data siswa |

- **Penghapusan atas permintaan:** orang tua berhak meminta datanya dihapus. TU (admin_tu) bisa menghapus data pribadi pendaftar berstatus Draf/Ditolak/Mundur/Cadangan kapan saja, dengan konfirmasi dan tercatat di audit.
- **Dianonimkan, bukan dihapus barisnya,** agar rekap pendaftaran (jumlah per jalur dan status) tetap benar.
- **Data yang dihapus:**
  - nama, NIK, No KK, no akta, KIP/KKS;
  - alamat dan data rinci, data orang tua/wali, data pindahan;
  - nomor HP dan kode lanjut;
  - catatan perubahan status;
  - semua dokumen beserta file di storage.

  Tanggal lahir dipotong menjadi tahun saja.
- **Data yang dipertahankan:** status, jalur, periode, jenis kelamin, tahun lahir, nomor pendaftaran, nilai tes (tanpa identitas), dan catatan pembayaran biaya formulir (bukti keuangan sekolah yang wajib disimpan untuk pembukuan).
- **Riwayat percobaan** formulir publik (`admission_attempts`, hash nomor HP dan IP) untuk pembatas percobaan dihapus setelah 30 hari.
- **Pemberitahuan:** teks persetujuan di halaman awal pendaftaran menyebut masa simpan ini.

## Langkah
1. Migrasi: `applicants.anonymized_at`, `anonymized_reason` (`masa_simpan` / `permintaan`).
2. Service `Anonymize` (satu transaksi, cek status) dan daftar pendaftar yang sudah lewat masa simpan.
3. Job River harian (jalan juga saat start): anonimkan yang lewat masa simpan, hapus file dokumennya, bersihkan `admission_attempts` lama.
4. Detail pendaftar:
   - tanggal penghapusan otomatis;
   - tombol "Hapus data pribadi sekarang" (admin_tu);
   - banner bila sudah dihapus.

   Pendaftar yang sudah dianonimkan tidak bisa diubah statusnya atau diisi lagi, dan tidak bisa dicek di halaman cek status publik.
5. Teks persetujuan di halaman awal pendaftaran.
6. Uji: aturan tanggal per status, isi yang dihapus/dipertahankan, file storage terhapus, job idempoten, tombol manual (peran, status, lintas sekolah), dan rekap tetap menghitung.

## Kriteria selesai
- [x] Pendaftar Draf/Ditolak/Mundur dianonimkan 90 hari setelah periode tutup; Cadangan sesuai aturan di atas
- [x] Dokumen dan file storage terhapus; NIK dan data rinci tidak tersisa di database
- [x] Rekap pendaftaran tetap menghitung pendaftar yang sudah dianonimkan
- [x] TU bisa menghapus atas permintaan; tercatat di audit; peran lain dan sekolah lain ditolak
- [x] Teks persetujuan menyebut masa simpan
- [x] Route baru masuk daftar uji lintas sekolah

## Catatan implementasi
- **Kode:** `internal/admission/retention.go`.
  - `RetentionDue` berisi aturan tanggal.
  - `Anonymize` berjalan dalam satu transaksi: cek status tanpa mendekripsi, hapus baris dokumen, kosongkan catatan status, anonimkan baris pendaftar, catat audit. Setelah transaksi, file dokumen di storage dihapus.
  - `RetentionWorker` adalah job River harian (`admission_retention`, juga jalan saat start), per batch 200, aman diulang.
- **Migrasi 000056:** `applicants.anonymized_at`, `anonymized_reason` dan indeks parsial untuk pencarian pendaftar yang belum dianonimkan.
- **Hasil anonimisasi:**
  - nama → "Data dihapus";
  - tanggal lahir → 1 Januari tahun lahirnya;
  - profil, orang tua, dan data pindahan → `{}`;
  - semua kolom terenkripsi, blind index NIK, kode lanjut → kosong;
  - nomor HP → `+00000000` (kolom wajib berformat).
- **Tetap tersimpan:** catatan pembayaran biaya formulir dan nilai tes. Riwayat percobaan formulir publik lebih dari 30 hari dihapus oleh job yang sama.
- **Tampilan TU:**
  - Detail pendaftar menampilkan "akan dihapus otomatis pada …" dan tombol **Hapus data pribadi sekarang** (admin_tu, wajib centang konfirmasi) untuk status draf/ditolak/mundur/cadangan.
  - Setelah dihapus muncul banner dengan tanggal dan alasan; data rinci dan tombol tindak lanjut disembunyikan.
  - Audit `admission.applicant_anonymize` mencatat aktor (kosong untuk job), alasan, status, dan jumlah dokumen; tanpa data pribadi.
- **Efek lain:**
  - Pendaftar yang sudah dianonimkan tidak bisa diubah statusnya atau diisi lagi.
  - Tidak ditemukan di cek status publik.
  - Tetap dihitung di rekap.
- **Pemberitahuan:** teks persetujuan di halaman awal: "Jika tidak diterima atau mengundurkan diri, data pribadi dihapus 90 hari setelah pendaftaran ditutup."
- **Belum dicakup:** data pendaftar yang **diterima** tetap tersimpan di baris pendaftar selain di data siswa. Ini perlu keputusan terpisah: apakah dokumen KK/akta dipindah ke berkas siswa, lalu data pendaftar dianonimkan setelah menjadi siswa.

