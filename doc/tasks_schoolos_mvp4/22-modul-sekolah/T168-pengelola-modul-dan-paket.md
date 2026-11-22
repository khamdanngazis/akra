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

## Kriteria selesai
- [ ] Uji layanan: ketergantungan ditolak, penjaga menolak atau memperingatkan sesuai aturan, audit tercatat dengan catatan, paket diterapkan dalam satu transaksi
- [ ] Uji CLI: `-module`, `-package`, `-option`, `-list`, berkas gelombang, dan `-force`
- [ ] Tidak ada jalan lain mengubah `school_modules` selain `module.Admin` (dicek di uji/grep)
- [ ] Panduan implementator tersedia
