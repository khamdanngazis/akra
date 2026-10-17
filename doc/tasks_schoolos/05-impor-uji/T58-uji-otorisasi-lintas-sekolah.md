# T58 — Uji otorisasi lintas sekolah

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T51, T46
**Estimasi:** 1–2 hari

## Tujuan
Otomatis membuktikan bahwa pengguna sekolah A tidak bisa membaca atau mengubah data sekolah B di setiap route utama. Ini mitigasi utama R1.

## Langkah
1. Buat `internal/testutil/fixtures.go`:
   - Fungsi `SeedTwoSchools(t, db)` yang membuat sekolah A dan B, masing-masing dengan:
     - satu admin/TU, satu bendahara, satu guru (wali kelas), satu kepala sekolah
     - satu kelas, dua siswa, satu wali dengan akun, satu tagihan, satu pembayaran, satu bukti, satu pengumuman
   - Kembalikan ID semua entitas untuk dipakai di uji.
2. Buat daftar route utama di `internal/testutil/routes.go`. Setiap entri berisi: method, path template (dengan `{id}`), peran yang diizinkan, dan cara membuat ID milik sekolah lain.
3. Buat `internal/testutil/cross_tenant_test.go`:
   - Untuk setiap route dan setiap pengguna sekolah A, coba akses ID milik sekolah B.
   - Hasil yang diharapkan: `404` atau `403`. Tidak boleh `200`, dan tidak boleh ada data sekolah B di respons.
   - Untuk route POST dan PUT, pastikan data sekolah B tidak berubah setelah request.
4. Tambahkan langkah CI yang menjalankan uji ini setiap push.
5. Uji juga bahwa setiap route baru yang didaftarkan harus punya entri di `routes.go`. Jika tidak, uji gagal dengan pesan yang jelas. Ini mencegah route baru lolos tanpa uji.

## File yang disentuh
- `internal/testutil/fixtures.go`, `routes.go`, `cross_tenant_test.go`
- `.github/workflows/test.yml` atau konfigurasi CI yang dipakai

## Kriteria selesai
- [ ] Semua route di daftar diuji dan lulus
- [ ] Mengubah satu route tanpa menambah entri di `routes.go` menyebabkan uji gagal
- [ ] Uji sengaja dibuat gagal (misalnya menghapus satu pengecekan `school_id`) dan uji mendeteksinya
- [ ] Uji berjalan di CI dan menghalangi merge jika gagal

## Catatan
- Uji ini harus dianggap sebagai bagian dari fitur, bukan tugas tambahan. Setiap fase baru menambah route dan entri di sini.
