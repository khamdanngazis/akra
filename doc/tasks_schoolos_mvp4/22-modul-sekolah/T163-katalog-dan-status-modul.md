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

## Kriteria selesai
- [ ] Migrasi naik/turun di `akra_test`; sekolah yang sudah ada mendapat modul sesuai aturan migrasi (uji migrasi)
- [ ] Perubahan modul terlihat di menu tanpa login ulang, paling lama 30 detik (uji dengan jam palsu)
- [ ] Katalog: uji tidak ada ketergantungan melingkar, dan setiap `Module` di menu dan registry route ada di katalog
- [ ] Uji modul ujian lama (T125) tetap lulus
