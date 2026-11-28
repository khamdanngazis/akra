# T163 — Katalog modul dan status modul per sekolah

**Fase:** 22 Modul per Sekolah
**Dependensi:** T156, keputusan ADR 0002 bagian 8
**Estimasi:** 2 hari
**Rancangan:** [ADR 0002](../../adr/0002-modul-per-sekolah.md)

## Latar belakang
Status modul saat ini tersimpan sebagai kunci `<modul>.enabled` di `school_settings`, dan baru dipakai modul ujian. Daftar modul dihitung saat login lalu disimpan di sesi, sehingga perubahan baru terlihat setelah login ulang.

## Langkah
1. **Paket `internal/platform/module`:**
   - katalog modul (ID, nama, keterangan, ketergantungan wajib dan opsional, prefix route, fitur) sesuai ADR 0002 bagian 3–4;
   - tipe `Set` dengan `Has(id)` dan `HasOption(id, key)`;
   - `core` selalu aktif.
2. **Tabel `school_modules`:** `school_id`, `module`, `enabled`, `options jsonb`, `changed_at`, `changed_by`, `note`; PK `(school_id, module)`. Kolom `schools.package` (label paket, boleh kosong).
3. **Migrasi data:**
   - semua sekolah: semua modul non-ujian aktif;
   - `exam`: dari `exam.enabled` dan `exam.release` (`options.release`);
   - kunci lama dihapus di migrasi berikutnya, setelah kode tidak membacanya.
4. **`module.Load(ctx, q, schoolID)`** dengan cache per replika selama 30 detik. **`module.Set(...)`** menulis, menghapus cache replika ini, dan mencatat audit `module.<id>`.
5. **Middleware `module.Context`** di grup login staf, portal, dan publik per sekolah: memuat `Set` sekali per request ke context. **`auth.Identity.Modules`** tidak lagi diisi dari sesi; menu dan template membaca dari context (fungsi template `hasModule`).
6. **`exam.LoadModule` dan `exam.SetModule`** memakai paket baru. Perilaku modul ujian tidak berubah: tahap rilis tetap dari `options.release`.

## Catatan implementasi
- **Paket `internal/platform/module`:** katalog (`Catalog`, `Get`, `Known`), `Set` (`Has`, `HasName`, `Option`, `HasOption`, `IDs`), `Load` dengan cache per replika `CacheTTL` 30 detik (jam `module.Now` bisa diganti di uji), `GetState`, `Save` (menulis, menghapus cache replika, audit `module.<id>` lewat `Auditor` dari pemanggil), `Forget`, dan `Middleware`. Paket ini tidak bergantung pada `auth` atau `audit`.
- **Sekolah tanpa baris untuk suatu modul** memakai bawaan katalog: semua modul aktif kecuali ujian. Sekolah baru yang dibuat sebelum T168 (paket) jadi tetap berperilaku seperti sekarang.
- **Migrasi 089:** tabel `school_modules` (`changed_by` berupa teks karena pelakunya tim implementator, bukan pengguna sekolah) dan `schools.package` (teks, bawaan kosong). Sekolah lama mendapat delapan modul non-ujian aktif, dan ujian dari `exam.enabled`/`exam.release`. Migrasi turun mengembalikan status ujian ke kunci lama.
- **Kunci lama `exam.enabled` dan `exam.release`** tidak lagi dibaca kode (konstantanya dihapus). Barisnya dihapus di migrasi rilis berikutnya, agar replika lama saat deploy bergulir tetap benar.
- **Sesi tidak lagi menyimpan modul:** `auth.Identity.Modules`, kunci sesi `modules`, dan kolom modul di query login dihapus. Middleware di grup login memuat `Set` ke context; template memakai fungsi per request `modules` dan `hasModule`, dan pusat Pengaturan memakai `module.From`.
- **Menu kini menyaring semua modul katalog**, bukan hanya ujian. `nav.DefaultModules` sama dengan bawaan katalog (diuji).
- **Route publik** (`/s/...` pendaftaran, `/u/...` peserta) belum memakai middleware: halaman peserta tetap memeriksa modul ujian lewat `exam.LoadModule` (kini dari `school_modules` dengan cache). Penegakan route publik dan staf lainnya di T164.
- **`exam.LoadModule` dan `exam.SetModule`** memakai paket baru; `SetModule` menerima pelaku dan auditor. `akra-modul` membaca dan menulis `school_modules`, mencatat pelaku `akra-modul (<USER>)`, dan pesan "login ulang" diganti "paling lama 30 detik".
- **Registry route** belum punya kolom `Module`; kolom itu dan ujinya ditambahkan di T164. Di T163 yang diuji: setiap `Module` di menu dan pengaturan ada di katalog.

## Kriteria selesai
- [x] Migrasi naik/turun di `akra_test`; sekolah yang sudah ada mendapat modul sesuai aturan migrasi (uji migrasi)
- [x] Perubahan modul terlihat di menu tanpa login ulang, paling lama 30 detik (uji dengan jam palsu)
- [x] Katalog: uji tidak ada ketergantungan melingkar, dan setiap `Module` di menu ada di katalog (registry route menyusul di T164, lihat catatan)
- [x] Uji modul ujian lama (T125) tetap lulus
