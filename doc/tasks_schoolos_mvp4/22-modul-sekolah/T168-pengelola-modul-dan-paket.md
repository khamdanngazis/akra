# T168 — Pengelola modul dan paket (tim AKRA)

**Fase:** 22 Modul per Sekolah
**Dependensi:** T163
**Estimasi:** 2 hari

## Latar belakang
`akra-modul` hanya mengenal modul ujian. Tim AKRA perlu menyalakan dan mematikan modul apa pun, memasang paket, dan melihat semua sekolah di satu tempat. Pengecekan pengamannya harus sama, baik dari CLI maupun dasbor.

## Langkah
1. **Layanan `module.Admin`** dipakai bersama oleh CLI dan dasbor:
   - `Enable` / `Disable` / `ApplyPackage`;
   - cek ketergantungan wajib saat mengaktifkan, dan tolak dengan pesan jelas;
   - **penjaga** saat menonaktifkan: exam ditolak bila ada sesi berlangsung atau terjadwal dalam 24 jam; admission ditolak bila periode sedang dibuka; billing hanya peringatan bila ada tagihan belum lunas (butuh `-force` / centang konfirmasi);
   - semua perubahan tercatat di audit dengan catatan wajib.
2. **Paket templat di katalog** (Dasar, Akademik, Keuangan, Lengkap, sesuai keputusan 2 ADR). `ApplyPackage` menyalakan modul paket, mematikan modul di luar paket (dengan penjaga), dan mengisi `schools.package`.
3. **CLI `akra-modul` diperluas:**

   ```sh
   akra-modul -school a,b -module billing -on
   akra-modul -school a -module exam -on -option release=r2
   akra-modul -file gelombang.txt -package lengkap
   akra-modul -list [-module exam]
   ```

   Satu perintah tetap satu transaksi.
4. **Dasbor internal** `/internal/sekolah` (Basic auth `OPS_TOKEN`):
   - tabel sekolah × modul;
   - ubah modul atau paket per sekolah dengan catatan;
   - antrean permintaan dari T167.
5. **Sekolah baru:** pembuatan sekolah (`cmd/seed`, `cmd/demoseed`, onboarding) menerima paket; bawaan paket Dasar.

## Kriteria selesai
- [ ] Uji layanan: ketergantungan ditolak, penjaga menolak atau memperingatkan sesuai aturan, audit tercatat, paket diterapkan dalam satu transaksi
- [ ] CLI dan dasbor memakai layanan yang sama (uji keduanya)
- [ ] Dasbor tidak bisa dibuka tanpa `OPS_TOKEN` dan tidak tercantum di menu sekolah
