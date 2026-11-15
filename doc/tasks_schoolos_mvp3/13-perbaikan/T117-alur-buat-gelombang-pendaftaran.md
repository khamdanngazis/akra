# T117 — Alur membuat gelombang pendaftaran yang jelas

**Fase:** 13 Perbaikan
**Dependensi:** T92, T109
**Estimasi:** 1–1,5 hari
**Status:** selesai (2026-11-15)

## Masukan pengguna (2026-11-15)
Pembuatan gelombang pendaftaran masih membingungkan.

## Analisa kondisi sekarang
- **Istilah tidak konsisten:**
  - Menu dan form memakai **"Periode"**, deskripsi halaman memakai "Gelombang", contoh nama "Gelombang 1".
  - Tombol di daftar pendaftar berbunyi "Periode dan jalur".
  - Pengguna tidak tahu bahwa satu gelombang = satu periode.
- **Langkah terpecah dan tersembunyi:**
  1. Form "Buat periode" (jenis, nama, tahun ajaran, tingkat, biaya, tanggal, dokumen) disimpan dengan status **draf**.
  2. Jalur ditambahkan terpisah di halaman detail periode.
  3. Pendaftaran baru bisa dibuka **setelah minimal satu jalur**, tapi syarat ini hanya tertulis kecil di kartu jalur.
  4. Tes seleksi diatur di halaman lain (`/tes`), dengan syarat bobot total 100%.
  5. Tombol "Buka pendaftaran" ada di kartu Ketentuan.

  Tidak ada daftar periksa yang menunjukkan langkah mana yang sudah dan belum.
- **Field kurang dijelaskan:**
  - "Tahun ajaran siswa baru": maksudnya tahun ajaran saat siswa mulai sekolah, bukan tahun ajaran sekarang.
  - "Tingkat tujuan": angka 1–12.
  - "Wajib lunas sebelum verifikasi".
- **Jalur** hanya punya nama, kuota, dan centang "memakai tes". Arti jalur dan cara seleksinya tidak jelas (lihat T118).

## Usulan
1. **Satu istilah: "Gelombang pendaftaran"** di menu, judul, dan tombol. "Periode" tidak dipakai lagi di UI.
2. **Wizard 4 langkah** dengan progres di atas:
   1. **Dasar:** jenis (baru/pindahan), nama, tahun ajaran mulai sekolah, tingkat. Tingkat dipilih dari pilihan jenjang (SD kelas 1, SMP kelas 7, SMA/SMK kelas 10), bukan angka bebas.
   2. **Jadwal dan biaya:** tanggal buka/tutup, biaya, wajib lunas.
   3. **Jalur dan cara seleksi:** minimal satu jalur, langsung dengan metode seleksi (T118). Ada template cepat: "Satu jalur reguler tanpa tes".
   4. **Dokumen dan pratinjau:** dokumen wajib, pratinjau formulir publik, lalu **Buka sekarang** atau **Simpan sebagai draf**.
3. **Daftar periksa di detail gelombang:** "✓ Jalur ada · ✗ Bobot tes belum 100% · ✓ Tanggal valid", plus tombol Buka yang hanya aktif jika semua siap.
4. **Salin gelombang sebelumnya,** misalnya membuat Gelombang 2 dari Gelombang 1 dengan jalur dan tes yang sama.
5. **Detail gelombang memakai tab:** Ringkasan · Jalur & tes · Pendaftar · Peringkat & hasil. Ini menggantikan deretan tombol kecil.

## Keputusan (2026-11-15)
- Wizard **bertahap**; gelombang disimpan sebagai draf sejak langkah pertama.
- **Salin gelombang** dikerjakan sekalian di task ini.

## Kriteria selesai
- [x] Gelombang reguler tanpa tes bisa dibuat dan dibuka dalam satu alur tanpa pindah halaman manual
- [x] Istilah "Gelombang" konsisten di semua halaman staf
- [x] Daftar periksa menjelaskan kenapa gelombang belum bisa dibuka

## Catatan implementasi
- **Wizard** (`internal/admission/wizard.go`, template `period_wizard.html`):
  - `GET/POST /pendaftaran/periode/{id}/atur/{dasar|jadwal|jalur|dokumen}`.
  - Langkah dasar untuk gelombang baru dikirim ke `POST /pendaftaran/periode`. Jadwal sementara diisi hari ini sampai +30 hari, dan dokumen bawaan KK, akta, dan foto (pindahan: KK, akta, surat pindah, rapor).
  - Setiap langkah hanya menimpa field langkahnya di atas data tersimpan, lalu memanggil `Service.Update`. "Simpan dan keluar" kembali ke detail.
  - Langkah jalur memakai route jalur yang ada dengan `kembali=atur`; error form jalur selalu ditampilkan di langkah jalur.
  - "Masuk ke kelas" berupa pilihan dengan label jenjang (kelas 1/7/10).
  - Route lama `GET /{id}/ubah` diarahkan ke langkah dasar; `POST /{id}` dan `period_form.html` dihapus.
- **Daftar periksa** (`periodChecklist`): minimal satu jalur, bobot tes 100% jika ada jalur dengan tes, tanggal tutup belum lewat.
  - Dipakai di langkah dokumen, di kartu "Siap dibuka / Belum bisa dibuka" pada detail, dan saat membuka lewat `POST /{id}/status` (ditolak 409 jika belum siap).
- **Salin** (`Service.Copy`, `POST /{id}/salin`):
  - Ikut disalin: jenis, tahun ajaran, kelas, biaya, aturan biaya, dokumen, jalur, dan tes (tanpa jadwal tes dan nilai).
  - Tanggal buka = sehari setelah gelombang asal tutup (paling awal hari ini), dengan lama yang sama.
  - Nama "(salinan)", "(salinan 2)", dst. agar tidak bentrok.
  - Diaudit `admission.period_copy`.
- **Detail gelombang:**
  - Tab Ringkasan · Tes seleksi · Pendaftar · Peringkat & hasil (partial `admission_period_tabs`, juga di halaman tes dan peringkat).
  - Jalur tampil ringkas dengan tombol "Ubah jalur" ke wizard.
- **Istilah:** "Periode" diganti "Gelombang" di halaman staf, filter, kolom Excel rekap, dan pesan. URL tetap `/pendaftaran/periode`.
