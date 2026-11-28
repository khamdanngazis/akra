# T168 — Pengelola modul dan paket untuk tim implementator (CLI)

**Fase:** 22 Modul per Sekolah
**Dependensi:** T163
**Estimasi:** 1,5 hari

## Latar belakang
`akra-modul` hanya mengenal modul ujian. Tim implementator AKRA perlu menyalakan dan mematikan modul apa pun, memasang paket, dan melihat status semua sekolah.

Sesuai keputusan 1 ADR 0002, **UI implementator belum dibuat**: untuk sekarang semuanya lewat CLI. Logika pengelolaan ditaruh di satu layanan agar UI implementator nanti tinggal memakainya.

## Langkah
1. **Layanan `module.Admin`**, satu-satunya jalan mengubah modul:
   - `Enable` / `Disable` / `ApplyPackage`;
   - cek ketergantungan wajib saat mengaktifkan, dan tolak dengan pesan jelas;
   - **penjaga** saat menonaktifkan:
     - exam ditolak bila ada sesi berlangsung atau terjadwal dalam 24 jam;
     - admission ditolak bila periode sedang dibuka;
     - billing hanya peringatan bila ada tagihan belum lunas (butuh `-force`);
   - semua perubahan tercatat di audit dengan catatan wajib (`-note`).
2. **Paket templat di katalog:** Dasar, Akademik, Keuangan, Lengkap (keputusan 2 ADR). `ApplyPackage` menyalakan modul paket, mematikan modul di luar paket (dengan penjaga), dan mengisi `schools.package`.
3. **CLI `akra-modul` diperluas** (pola gelombang dan `-list` dari T155 tetap):

   ```sh
   akra-modul -school a,b -module billing -on -note "kontrak 2027"
   akra-modul -school a -module exam -on -option release=r2 -note "..."
   akra-modul -file gelombang.txt -package lengkap -note "..."
   akra-modul -list [-module exam]
   ```

   Satu perintah tetap satu transaksi. `-list` menampilkan sekolah × modul dan paket.
4. **Sekolah baru:** pembuatan sekolah (`cmd/seed`, `cmd/demoseed`, onboarding) menerima paket; bawaannya paket Dasar.
5. **Panduan singkat untuk tim implementator** di `doc/implementator-modul.md`: arti setiap modul dan paket, perintah umum, penjaga, dan cara membaca audit.

## Menyusul (di luar task ini)
Dasbor atau UI implementator: tabel sekolah × modul, ubah modul/paket dengan catatan, dan antrean permintaan sekolah. UI ini memakai `module.Admin`.

## Catatan implementasi
- **`module.Admin{By}`:**
  - metode `Set`, `Enable`, `Disable`, `ApplyPackage`, semuanya dalam transaksi pemanggil (`pgx.Tx`);
  - semua pemeriksaan (catatan, pilihan, ketergantungan, penjaga) selesai sebelum ada yang ditulis;
  - setiap modul yang berubah dicatat `module.<kode>` dan paket `module.package` di audit, dengan `note` dan `by`; tanpa perubahan nyata tidak ada tulis maupun audit.
- **Pilihan modul divalidasi:** saat ini hanya `exam.release` ∈ r1/r2/r3.
- **Penjaga** memakai SQL langsung (tanpa impor paket ujian atau pendaftaran):
  - ujian: sesi tidak dibatalkan yang belum berakhir dan mulai sebelum 24 jam lagi;
  - pendaftaran: periode berstatus `buka`;
  - keuangan: tagihan tidak dibatalkan yang belum lunas (`GuardError.Forceable`, lolos dengan `force` sebagai peringatan).
- **Ketergantungan wajib:** katalog sekarang hanya bergantung pada inti, jadi `DependencyError` diuji dengan katalog sementara.
- **Paket** ada di `module.Packages` (Dasar, Akademik, Keuangan, Lengkap). `ApplyPackage` mempertahankan pilihan modul yang ada, mis. tahap rilis ujian.
- **`module.Save` dan `exam.SetModule` dihapus.** Uji yang perlu mengatur modul tanpa penjaga (mis. mematikan ujian saat sesi berjalan) memakai helper uji yang menulis langsung. `TestOnlyAdminWritesModules` memeriksa bahwa tidak ada kode aplikasi lain yang menulis `school_modules` atau `schools.package`; uji, `internal/testutil`, dan migrasi dikecualikan.
- **CLI `akra-modul`:**
  - flag `-module`, `-on`/`-off`, `-option k=v` (berulang), `-package`, `-note` (wajib untuk perubahan), `-force`, `-list [-module]`;
  - `-option` tanpa `-on`/`-off` hanya mengubah pilihan;
  - bentuk lama `-exam`/`-release` tetap diterima;
  - `-list` menampilkan sekolah × modul (✓/·) dan paket; `-list -module` menampilkan satu modul dengan pilihan dan tanggal perubahan terakhir.
- **Sekolah baru:**
  - `akra-seed -package` (bawaan `dasar`) memasang paket saat sekolah dibuat;
  - `akra-demoseed -package` bawaannya kosong (semua modul kecuali ujian, seperti sebelumnya), supaya demo tetap lengkap;
  - tidak ada onboarding lain di aplikasi.
- **Uji `akra-seed`** kini membersihkan sekolahnya dengan `demo.DeleteSchool`, karena paket menulis audit yang append-only.
- **Panduan:** [doc/implementator-modul.md](../../implementator-modul.md). Contoh perintah di README dan `doc/uji-mvp4.md` diperbarui (kini dengan `-note`).

## Kriteria selesai
- [x] Uji layanan: ketergantungan ditolak, penjaga menolak atau memperingatkan sesuai aturan, audit tercatat dengan catatan, paket diterapkan dalam satu transaksi
- [x] Uji CLI: `-module`, `-package`, `-option`, `-list`, berkas gelombang, dan `-force`
- [x] Tidak ada jalan lain mengubah `school_modules` selain `module.Admin` (dicek di uji/grep)
- [x] Panduan implementator tersedia
