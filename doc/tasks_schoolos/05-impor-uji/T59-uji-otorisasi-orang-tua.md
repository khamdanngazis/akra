# T59 — Uji otorisasi orang tua

**Fase:** 05 Impor dan Pengujian
**Dependensi:** T45, T58
**Estimasi:** 1 hari

## Tujuan
Membuktikan bahwa orang tua hanya melihat anaknya sendiri, dan tidak bisa mengakses halaman staf.

## Langkah
1. Gunakan fixture dari T58, lalu tambahkan:
   - Dua orang tua di sekolah A: orang tua 1 (anak X), orang tua 2 (anak Y).
   - Satu orang tua di sekolah B.
2. Uji kepemilikan:
   - Orang tua 1 membuka detail, tagihan, dan kuitansi milik anak X: `200`.
   - Orang tua 1 membuka hal yang sama untuk anak Y: `404`.
   - Orang tua 1 membuka kuitansi milik anak Y: `404`.
   - Orang tua 1 membuka pengumuman untuk kelas anak Y: tidak muncul.
3. Uji peran:
   - Orang tua mengakses setiap route di bawah `/siswa`, `/keuangan`, `/absensi`, `/laporan`, `/impor`, `/pengaturan`, `/kelas`, dan `/pengumuman` (akses tulis): `403`.
   - Orang tua mengakses `/portal/*` yang tidak ada di daftar: `404`.
4. Uji sesi:
   - Setelah orang tua 1 logout, akses ke `/portal` kembali ke login.
5. Tambahkan uji untuk enumerasi: orang tua yang mencoba ID acak tidak mendapat respons berbeda antara "ada tetapi bukan miliknya" dan "tidak ada".

## File yang disentuh
- `internal/testutil/parent_auth_test.go`
- `internal/testutil/fixtures.go` (tambahan orang tua)

## Kriteria selesai
- [ ] Semua uji kepemilikan lulus
- [ ] Semua uji peran menghasilkan `403` atau `404` sesuai
- [ ] Respons `404` untuk "bukan milik saya" dan "tidak ada" identik (status dan body)
- [ ] Uji berjalan di CI

## Catatan
- Uji ini melengkapi T58. T58 fokus pada sekolah yang berbeda, T59 fokus pada orang tua di sekolah yang sama.
