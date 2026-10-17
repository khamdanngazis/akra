# T10 — RBAC dasar

**Fase:** 01 Fondasi
**Dependensi:** T08
**Estimasi:** 3 jam

## Tujuan
Akses route dibatasi berdasarkan peran. Peran yang tidak sesuai mendapat `403`.

## Langkah
1. Buat `internal/auth/roles.go`:
   - Konstanta: `RoleAdminTU = "admin_tu"`, `RoleBendahara = "bendahara"`, `RoleGuru = "guru"`, `RoleKepalaSekolah = "kepala_sekolah"`, `RoleOrangTua = "orang_tua"`.
   - Fungsi `HasAnyRole(identity, roles...) bool`.
2. Buat middleware `RequireRole(roles ...string) func(http.Handler) http.Handler`:
   - Jika tidak ada identitas (tidak login), redirect ke `/login` (sudah ditangani T08).
   - Jika identitas ada tetapi peran tidak cocok, kembalikan `403` dengan halaman "Akses ditolak".
3. Peran diambil dari sesi, dibaca sekali di T08 dan disimpan ke `Identity`.
4. Tambahkan contoh pemakaian di `README.md`:
   ```go
   r.With(auth.RequireRole(auth.RoleBendahara)).Get("/tagihan", ...)
   ```
5. Buat route uji sementara `GET /debug/bendahara` (hanya aktif di development) untuk verifikasi, lalu hapus setelah uji selesai.
6. Aturan dokumentasi: setiap route baru wajib menyebut peran yang diizinkan di komentar di atas handler.

## File yang disentuh
- `internal/auth/roles.go`
- `internal/auth/middleware.go` (tambahan)
- `README.md`

## Kriteria selesai
- [x] Uji unit: `RequireRole("bendahara")` menolak `guru` dengan `403`
- [x] Uji unit: `RequireRole("bendahara")` mengizinkan `bendahara`
- [x] Uji unit: pengguna dengan dua peran (misalnya `guru` dan `bendahara`) diizinkan jika salah satu cocok
- [x] Route debug sudah dihapus sebelum merge ke `main`

## Catatan
- Pembatasan per sekolah (`school_id`) bukan tanggung jawab RBAC ini. Itu diuji di T58 dan dipakai di setiap modul.
