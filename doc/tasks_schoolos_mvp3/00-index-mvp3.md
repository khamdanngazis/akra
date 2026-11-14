# Daftar Task MVP 3 SchoolOS — Pendaftaran dan Bantu Dapodik

Acuan: hasil pemeriksaan installer Dapodik 2027 (formulir F-PD, template import nilai, template ekspor daftar PD, dan `classic/app.js`), serta model data siswa MVP 1–2.

## Latar Belakang
- Data siswa SchoolOS saat ini hanya NIS, nama, tanggal lahir, dan jenis kelamin. Dapodik butuh sekitar 70 field per siswa (F-PD).
- Dapodik 2027 **tidak menyediakan import Excel untuk siswa**. Satu-satunya import Excel yang aktif adalah import nilai (`template-format-import-nilai.xlsx`). Siswa masuk lewat input manual, tombol "Registrasi", atau "Tarik Data" dari pusat.
- Karena itu, bantuan ke Dapodik dibagi tiga:
  1. **Import:** nilai bisa diisi otomatis ke file format import Dapodik (T102). Data siswa hanya bisa dari Dapodik ke SchoolOS (T101).
  2. **Salin-tempel:** lembar salin dengan urutan field seperti form Dapodik (T98).
  3. **Formulir:** F-PD terisi, ekspor format daftar PD, dan bukti pendaftaran (T99, T100, T95).
- Data dikumpulkan sejak pendaftaran, sehingga operator Dapodik tidak perlu mengetik ulang dari kertas.

## Gerbang Masuk
Task MVP 3 **tidak dimulai** sebelum:
1. MVP 2 lulus T84 (uji internal dan pilot).
2. Keputusan di bagian "Keputusan yang Harus Diambil" sudah ada untuk task yang ditandai **[keputusan]**.

## Aturan Task
- Sama dengan MVP 1–2: kecil, punya dependensi, punya kriteria selesai yang bisa dicek.
- Setiap route baru wajib masuk daftar uji lintas sekolah (T58) dan uji orang tua (T59).
- Route publik (tanpa login) wajib punya rate limit dan tidak boleh membuka data sekolah lain.
- **Data pribadi sensitif** (NIK, No KK, no akta, NIK orang tua, nomor KIP/KKS, dokumen pendaftar):
  - **Dienkripsi di database** dengan AES-256-GCM di level aplikasi (T85b). Kunci di environment variable, terpisah dari database dan backup. Pencarian dan cek unik memakai blind index (HMAC).
  - Tidak boleh disimpan dalam bentuk teks di tempat lain: jsonb, tabel batch impor, payload job River, atau file sementara.
  - Hanya bisa dilihat `admin_tu` dan `kepala_sekolah`. Peran lain melihat versi ter-mask (`3201********0001`).
  - Tidak boleh muncul di log aplikasi. Di audit log, nilainya diredaksi (seperti migrasi 000025).
  - Persetujuan pengolahan data pribadi dicatat saat pendaftaran (UU PDP).
- Kode dan label referensi Dapodik hanya berasal dari T85. Jangan menyalin daftar kode di tempat lain.

## Daftar Fase

| Folder | Task | Isi | Estimasi |
|---|---|---|---|
| `09-data-induk/` | T85–T88 (+T85b) | Referensi kode Dapodik, enkripsi data pribadi, data rinci siswa dan orang tua, form dan kelengkapan data | 2 minggu |
| `10-pendaftaran/` | T89–T97 (+T93a, T93b) | Periode dan jalur, formulir publik, dokumen, verifikasi, tes seleksi, perankingan, biaya, status, penerimaan jadi siswa, rekap | 3,5 minggu |
| `11-bantu-dapodik/` | T98–T103 | Lembar salin, F-PD PDF, ekspor daftar PD, impor ekspor Dapodik, isi format import nilai, cek kesiapan | 2 minggu |
| `12-uji/` | T104–T105 | Uji lintas sekolah dan privasi, uji dengan Dapodik asli | 0,5–1 minggu |
| `13-perbaikan/` | T106– | Perbaikan dari hasil pengecekan setelah MVP 3 (T106: tombol ambil lokasi untuk lintang/bujur; T107: halaman periksa dan kirim yang jelas) | sesuai kebutuhan |

Total perkiraan: 8,5–9,5 minggu.

## Urutan Kritis
1. T85 dan T85b → T86 → T87 → T88 (fondasi, wajib lebih dulu)
2. T89 → T90 → T91 → T93 → T93a → T93b → T96 (pendaftaran inti). Didahulukan karena pendaftaran sekolah swasta umumnya dibuka Desember–Februari.
3. T98, T99, T101 (bantu Dapodik dengan manfaat terbesar)
4. T92, T94, T95, T97, T100, T102, T103
5. T104 dan T105 terakhir

## Keputusan (diambil 2026-11-14)
- Orang tua mengisi **data lengkap F-PD** saat mendaftar; bagian non-wajib boleh dilewati (T90).
- **Ada jalur dan tes/seleksi** (T89). Ditambah task T93a (tes dan nilai) dan T93b (perankingan dan penetapan hasil).
- Biaya formulir **diatur per periode**, termasuk apakah wajib lunas sebelum verifikasi (T89, T94).
- Format NIS **bisa diatur sekolah**, bawaan `{tahun}{urut:3}` (T96).

## Keputusan yang Masih Terbuka
- Masa simpan data pendaftar yang ditolak atau mundur (T93).

## Di Luar Cakupan
- Pengisian Dapodik otomatis (extension browser, autofill, atau robot). Dapodik tidak punya API dan cara ini rawan rusak setiap versi.
- Integrasi dengan SPMB pemerintah atau server Dapodik pusat.
- Pembayaran online (payment gateway).
- Seleksi berbasis tes online.

## Catatan Versi Dapodik
Referensi kode (T85), urutan field (T98), dan format file (T100–T102) mengikuti Dapodik 2027. Saat versi Dapodik baru keluar, periksa ulang installer-nya: `template_excel/`, `files/formulir/F-PESERTA_DIDIK.pdf`, dan label form di `classic/app.js`. Kode PHP Dapodik dienkripsi ionCube dan tidak dipakai sebagai acuan.
