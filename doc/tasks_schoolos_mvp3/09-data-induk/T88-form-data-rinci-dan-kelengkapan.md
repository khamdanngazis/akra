# T88 — Form data rinci dan kelengkapan data Dapodik

**Fase:** 09 Data Induk
**Dependensi:** T86, T87
**Estimasi:** 2 hari

## Tujuan
TU bisa mengisi data rinci siswa dan orang tua di satu halaman, dan langsung tahu siswa mana yang datanya belum siap untuk Dapodik.

## Langkah
1. Halaman `GET/POST /siswa/{id}/data-rinci`, dibagi per bagian sesuai F-PD: Data Pribadi, Alamat, Ayah, Ibu, Wali, Kontak, Periodik, Registrasi. Dropdown dari T85.
2. Simpan per bagian (satu form per bagian) agar error di satu bagian tidak menghapus isian bagian lain.
3. Fungsi `Completeness(profile, guardians) (percent int, missing []Field)` di `internal/dapodik`. Daftar field wajib mengikuti field wajib Dapodik: nama, JK, tempat dan tanggal lahir, NIK, agama, alamat sampai kecamatan, tempat tinggal, transportasi, nama ibu kandung, jenis pendaftaran, tanggal masuk.
4. Badge "% siap Dapodik" di profil siswa dan kolom di daftar siswa. Filter "Belum lengkap" di daftar siswa.
5. Akses ubah: `admin_tu`. Lihat: `admin_tu`, `kepala_sekolah` (NIK utuh); `guru` wali kelas melihat versi ter-mask.

## File yang disentuh
- `internal/dapodik/completeness.go`, `completeness_test.go`
- `internal/students/profile_handler.go`
- `web/templates/students/profile_form.html`, daftar siswa

## Kriteria selesai
- [ ] Siswa dengan semua field wajib terisi menunjukkan 100%
- [ ] Error validasi di bagian Alamat tidak menghapus isian bagian lain
- [ ] Filter "Belum lengkap" hanya menampilkan siswa < 100% di sekolah sendiri
- [ ] Route baru masuk daftar uji T58 dan T59
