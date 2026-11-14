# T96 — Pendaftar diterima menjadi siswa

**Fase:** 10 Pendaftaran
**Dependensi:** T93, T86, T87, T21, T24
**Estimasi:** 2 hari

## Tujuan
Pendaftar yang diterima menjadi siswa lengkap dengan profil dan orang tua, tanpa input ulang.

## Keputusan
Format NIS bisa diatur sekolah, bawaan `{tahun}{urut:3}`, tetap bisa diisi manual (2026-11-14).

## Langkah
1. Aksi "Jadikan siswa", per pendaftar atau massal untuk yang berstatus `diterima`.
2. Dalam satu transaksi per pendaftar:
   - Buat `students` (NIS otomatis atau diisi manual), `student_profiles` dari `profile jsonb` dan kolom terenkripsi pendaftar. Ciphertext didekripsi lalu dienkripsi ulang dengan additional data tabel tujuan (T85b).
   - Buat atau cocokkan `guardians` (cocokkan lewat blind index NIK `nik_idx` atau nomor HP di sekolah yang sama agar kakak-adik berbagi wali), lalu `student_guardians`.
   - `registration_type = 'Siswa Baru'` (atau sesuai isian), `entry_date` = tanggal mulai tahun ajaran.
   - Tautkan `applicants.student_id`.
3. Enrollment ke kelas opsional; jika kelas belum dibuat, siswa masuk daftar "belum punya kelas".
4. Format NIS diatur di pengaturan sekolah, mis. `{yyyy}{seq:3}`. NIS bentrok ditolak dengan pesan jelas.
5. Idempoten: pendaftar yang sudah punya `student_id` dilewati.
6. Opsi membuat akun portal orang tua memakai alur T24 (undangan email), bukan password dibuat sistem.
7. Untuk massal, jalankan sebagai job River dengan laporan hasil per pendaftar (seperti impor T55).

## File yang disentuh
- `internal/admission/convert.go`, `convert_test.go`, `convert_job.go`
- `internal/students/service.go` (jika perlu fungsi pembuatan dengan profil)

## Kriteria selesai
- [ ] Menjalankan konversi dua kali tidak membuat siswa ganda
- [ ] Dua pendaftar bersaudara dengan ibu ber-NIK sama berbagi satu data wali
- [ ] Data profil siswa hasil konversi identik dengan isian pendaftaran
- [ ] Kegagalan satu pendaftar di job massal tidak menggagalkan yang lain

## Catatan implementasi
Konversi massal berjalan langsung (bukan job River) dengan savepoint per pendaftar; jumlah pendaftar per proses biasanya ratusan. Akun portal orang tua tidak dibuat otomatis; TU memakai alur yang sudah ada di halaman siswa. Penghitung NIS per awalan di tabel `nis_counters`.
