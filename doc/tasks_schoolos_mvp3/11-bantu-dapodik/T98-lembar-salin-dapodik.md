# T98 — Lembar salin Dapodik

**Fase:** 11 Bantu Dapodik
**Dependensi:** T85, T88
**Estimasi:** 2 hari

## Tujuan
Operator Dapodik bisa memasukkan data siswa ke form Dapodik dengan salin-tempel, sesuai urutan field Dapodik, tanpa mengetik ulang.

## Latar belakang
Dapodik 2027 tidak punya import Excel untuk siswa. Penambahan siswa lewat form "Registrasi" atau "Tarik Data". Label dan urutan field form tersedia dalam bentuk teks biasa di `classic/app.js` Dapodik (bagian ini tidak dienkripsi).

## Langkah
1. Ambil urutan bagian dan field form Peserta Didik dari `classic/app.js` Dapodik 2027. Simpan sebagai daftar statis di `internal/dapodik/layout.go`: `Section{Name, Fields []Field{Label, Key, Kind}}`, dengan `Kind` = teks, tanggal, pilihan, angka. Catat versi Dapodik.
2. Halaman `GET /siswa/{id}/dapodik`:
   - Bagian dan field tampil persis dengan urutan dan label Dapodik.
   - Tiap field punya tombol salin (Clipboard API, Alpine.js). Field kosong ditandai, tidak bisa disalin.
   - Pilihan ditampilkan dengan label persis Dapodik (dari T85), karena combobox Dapodik dicari dengan mengetik label.
   - Tanggal dalam format `dd/mm/yyyy`.
3. Mode "Salin berikutnya": satu tombol (dan pintasan keyboard) yang menyalin field berikutnya secara berurutan dan menyorot field yang sedang aktif. Operator cukup bolak-balik: klik di akra, tempel di Dapodik.
4. Tombol "Tandai sudah masuk Dapodik": simpan `dapodik_entered_at` dan `dapodik_entered_by` di `student_profiles`. Daftar siswa bisa difilter "belum masuk Dapodik".
5. Navigasi "Siswa berikutnya" dalam satu kelas agar operator bisa bekerja per kelas.
6. Akses: `admin_tu`. NIK dan No KK tampil utuh di halaman ini.

## File yang disentuh
- `internal/dapodik/layout.go`, `layout_test.go`
- `internal/dapodik/handler.go`
- `web/templates/dapodik/copy_sheet.html`
- migrasi kecil untuk `dapodik_entered_at`, `dapodik_entered_by`

## Kriteria selesai
- [ ] Urutan field di halaman sama dengan form Dapodik 2027 (diperiksa operator di T105)
- [ ] Setiap field pilihan memakai label T85, tidak ada label yang ditulis tangan di template
- [ ] Mode "Salin berikutnya" melewati field kosong dan berhenti di akhir bagian dengan tanda yang jelas
- [ ] Route masuk daftar uji T58 dan T59

## Catatan
- Pengisian otomatis ke Dapodik (extension atau autofill) di luar cakupan.
