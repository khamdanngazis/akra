# T109 — Formulir pendaftaran siswa baru dan siswa pindahan dipisah, isian dirapikan

**Fase:** 13 Perbaikan
**Dependensi:** T89, T90, T91, T92, T96
**Estimasi:** 2–3 hari

## Latar belakang
Formulir pendaftaran saat ini menggabungkan isian untuk siswa baru dan siswa pindahan (mis. nomor peserta ujian, seri ijazah, SKHUN, sekolah asal sebelum pindah). Akibatnya formulir terasa sangat panjang dan seakan-akan banyak yang wajib diisi, padahal sebagian besar tidak relevan bagi orang tua calon siswa baru.

## Keputusan
Formulir pendaftaran yang sekarang **hanya untuk siswa baru**. Siswa pindahan memakai **formulir terpisah** (2026-11-14).

## Langkah
1. Formulir siswa baru (`/s/{kode}/daftar`):
   - Jenis pendaftaran otomatis "Siswa Baru"; isian khusus pindahan disembunyikan (nomor peserta ujian, seri ijazah, SKHUN, alasan pindah).
   - "Sekolah asal" berlabel jenjang sebelumnya (mis. TK/PAUD untuk SD, SD untuk SMP) dan opsional jika memang tidak ada.
   - Tinjau ulang isian yang ditampilkan agar hanya yang relevan untuk siswa baru; isian Dapodik lain tetap bisa dilengkapi TU setelah diterima.
2. Formulir siswa pindahan terpisah (mis. `/s/{kode}/pindah`):
   - Jenis pendaftaran otomatis "Pindahan".
   - Isian khusus: sekolah asal (sebelum pindah), NPSN sekolah asal (opsional), kelas/tingkat terakhir dan tingkat tujuan, NISN (wajib bila ada), alasan pindah, tanggal rencana masuk.
   - Dokumen khusus: surat keterangan pindah dari sekolah asal, rapor terakhir; dokumen umum (KK, akta) sesuai pengaturan.
   - Bisa dibuka kapan saja oleh sekolah (tidak terikat gelombang siswa baru), atau memakai periode bertipe "pindahan".
3. Pengaturan di sisi sekolah:
   - Periode punya jenis: "Siswa baru" atau "Pindahan" (atau pengaturan terpisah untuk penerimaan pindahan), dengan dokumen wajib masing-masing.
   - Halaman periode menampilkan link formulir yang sesuai.
4. Sisi TU:
   - "Input pendaftar" (T92) menanyakan jenis lebih dulu (baru / pindahan) lalu memakai formulir yang sesuai.
   - Daftar pendaftar bisa difilter per jenis; detail pendaftar menampilkan jenisnya.
   - Pindahan diterima → jadi siswa (T96) dengan `registration_type = 02 Pindahan`, tanggal masuk dari tanggal rencana masuk, dan ditempatkan di kelas tingkat tujuan.
5. Isian wajib (`Missing`) dibedakan per jenis, sehingga siswa baru tidak diminta isian khusus pindahan dan sebaliknya.
6. Nomor pendaftaran: tetap satu urutan per tahun ajaran, atau awalan berbeda untuk pindahan (mis. `PINDAH-2027-0001`) — putuskan saat implementasi.

7. Pisahkan bagian "Keluarga dan bantuan" (berlaku untuk kedua formulir dan form data rinci staf):
   - **Keluarga:** anak ke-berapa, jumlah saudara kandung.
   - **Bantuan pemerintah (opsional):** pertanyaan "Punya KIP?"; nomor dan nama di KIP baru muncul jika Ya. Hal yang sama untuk KKS/PKH. Petunjuk: "Isi jika keluarga menerima kartu bantuan dari pemerintah. Kosongkan jika tidak."
   - **Layak PIP (usulan dari sekolah) dan alasan PIP** diisi sekolah: disembunyikan dari formulir orang tua, tetap ada di form data rinci staf.
8. Bagian orang tua/wali menyesuaikan isian sebelumnya:
   - **Wali** hanya tampil jika "Tempat tinggal" (langkah Alamat) diisi "Wali"; untuk pilihan lain disembunyikan, dengan tautan "+ Tambah data wali" untuk kasus khusus (mis. panti asuhan, orang tua wafat).
   - Data wali yang sudah terisi tetap tampil dan tidak dihapus saat tempat tinggal diubah.
   - **Ayah/ibu dengan pekerjaan "Meninggal Dunia":** NIK, penghasilan, dan isian lain yang tidak relevan disembunyikan.
   - "Mempunyai wali" di lembar salin Dapodik tetap mengikuti ada tidaknya data wali.

## File yang disentuh
- `db/migrations/0000XX_admission_kind.up.sql` (jenis periode/pendaftar, isian pindahan)
- `internal/admission/applicant.go`, `public_handler.go`, `staff_entry.go`, `convert.go`, `period.go`
- `web/templates/admission/public_*.html`, `period_*.html`, `applicants.html`, `staff_new.html`

## Kriteria selesai
- [ ] Formulir siswa baru tidak menampilkan isian khusus pindahan
- [ ] Formulir pindahan punya isian dan dokumen khusus pindahan
- [ ] Isian wajib berbeda per jenis dan diuji unit
- [ ] Pindahan yang diterima menjadi siswa dengan jenis pendaftaran "Pindahan" dan kelas tujuan
- [ ] Bagian Keluarga dan Bantuan pemerintah terpisah; isian KIP/KKS hanya muncul jika dijawab Ya
- [ ] Layak PIP dan alasan PIP tidak tampil di formulir orang tua
- [ ] Bagian Wali hanya tampil jika tinggal bersama wali (atau ditambahkan manual); data wali yang ada tidak hilang
- [ ] Isian orang tua yang meninggal disederhanakan
- [ ] Route baru masuk daftar uji T58/T59; uji handler untuk kedua formulir
