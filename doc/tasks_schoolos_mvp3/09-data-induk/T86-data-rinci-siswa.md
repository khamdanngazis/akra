# T86 — Data rinci siswa

**Fase:** 09 Data Induk
**Dependensi:** T85, T85b
**Estimasi:** 2 hari

## Tujuan
Siswa punya data lengkap sesuai F-PD Dapodik, tanpa membuat tabel `students` menjadi lebar dan tanpa mengubah alur yang sudah ada.

## Langkah
1. Migrasi `student_profiles` (1:1 dengan `students`, FK komposit `(school_id, student_id)`):
   - Identitas: `nisn`, `nik_enc` + `nik_idx`, `kk_enc` + `kk_idx`, `birth_place`, `birth_cert_enc`, `religion`, `citizenship` (`WNI`/`WNA`), `country`, `special_needs text[]`.
   - Alamat: `street`, `rt`, `rw`, `hamlet`, `village`, `district`, `postal_code`, `latitude numeric(9,6)`, `longitude numeric(9,6)`, `residence_type`, `transport_mode`.
   - Keluarga dan bantuan: `child_order`, `siblings`, `has_kip`, `kip_enc`, `kip_name`, `pip_eligible`, `pip_reason`, `kks_enc`.
   - Kolom `*_enc` dan `*_idx` bertipe `bytea`, dienkripsi dan diindeks lewat T85b.
   - Kontak: `home_phone`, `mobile_phone`, `email`.
   - Periodik: `height_cm`, `weight_kg`, `head_cm`, `distance_km`, `travel_minutes`.
   - Registrasi: `registration_type`, `entry_date`, `previous_school`, `exam_number`, `diploma_serial`, `skhun_number`.
   - `updated_at`, `updated_by`.
2. Semua kolom boleh `NULL`. Data bisa dilengkapi bertahap.
3. Constraint di database: NISN `^[0-9]{10}$`, lintang −11..6, bujur 95..141, kode referensi lewat `CHECK` panjang 2 digit. Aturan NIK 16 digit (WNI), No KK 16 digit, dan NIK ≠ KK dicek di service sebelum enkripsi, karena database hanya melihat ciphertext.
4. Validasi di service memakai `ref.Valid` (T85) dan pesan error dalam bahasa Indonesia per field.
5. `UNIQUE (school_id, nisn) WHERE nisn IS NOT NULL` dan `UNIQUE (school_id, nik_idx) WHERE nik_idx IS NOT NULL`.
6. Service `students.Profile`: `Get`, `Upsert`. Field sensitif di model memakai tipe `pii.Value` (T85b): tampil ter-mask secara bawaan, nilai utuh hanya lewat `Reveal()` untuk `admin_tu`/`kepala_sekolah`.
7. Audit perubahan profil dengan nilai NIK, KK, dan no akta diredaksi.

## File yang disentuh
- `db/migrations/0000XX_student_profiles.up.sql` dan `.down.sql`
- `db/queries/student_profiles.sql`
- `internal/students/profile.go`, `profile_test.go`

## Kriteria selesai
- [ ] NIK sama dengan No KK ditolak
- [ ] NISN 9 digit atau berisi huruf ditolak dengan pesan per field
- [ ] NIK ganda di sekolah yang sama ditolak; di sekolah lain boleh
- [ ] Audit log tidak memuat NIK atau No KK dalam bentuk utuh
- [ ] Kolom `nik_enc`, `kk_enc`, `birth_cert_enc` di database tidak berisi teks NIK/KK/akta (dicek langsung dengan SQL di test)
- [ ] Siswa tanpa profil tetap tampil normal di semua halaman lama
