# T149 — Integrasi dengan tes seleksi pendaftaran

**Fase:** 19 Penilaian
**Dependensi:** T133, T146, T93a, T118, T119
**Estimasi:** 2 hari

## Tujuan
Tes seleksi dikerjakan online, dan nilainya langsung masuk ke peringkat pendaftaran tanpa diketik TU. Pengalaman calon siswa dan orang tua jelas sejak menerima kartu tes.

## Langkah
1. Di tab "Tes seleksi" gelombang (T119), setiap tes punya **cara tes**: "Di kertas / wawancara" (seperti sekarang) atau **"Ujian online"**.
   - Ujian online dipilih dari ujian `seleksi`, atau dibuat langsung lewat wizard (T133) dengan gelombang terisi.
   - Migrasi: `admission_tests.exam_id NULL`.
2. **Peserta otomatis:** pendaftar `diverifikasi` di jalur bertes. Pendaftar yang terverifikasi belakangan masuk ke sesi yang dipilih TU, dengan pemberitahuan di daftar tindak lanjut (T116).
3. **Lokasi:** di sekolah (`di_ruang`, ada pengawas, bunyi aktif) atau dari rumah (`jarak_jauh`). Untuk dari rumah, aturan integritas lebih ketat disarankan: kunci setelah N kali keluar halaman.
4. **Kartu tes** di halaman status pendaftaran dan bukti pendaftaran (T95, T119):
   - jadwal, lokasi/tautan, **kode akses**, QR;
   - tombol **Cek perangkat** (T136) dan **Coba ujian latihan** untuk calon siswa.
5. **Nilai masuk:**
   - saat nilai final, ditulis ke `applicant_test_scores` (sumber `ujian_online`);
   - tidak hadir tetap NULL (belum lengkap);
   - nilai minimal (T119) dan peringkat (T93b, T118) berjalan seperti biasa.
6. Nilai dari ujian online **tidak bisa disunting** di grid "Isi nilai". Ditandai ikon dengan tautan ke detail hasil; koreksi lewat nilai ulang (T146).
7. Kartu "Nilai tes" di detail pendaftar menampilkan sumber, durasi, dan ringkasan integritas.

## File yang disentuh
- `db/migrations/0000XX_admission_test_exam.up.sql` dan `.down.sql`
- `internal/admission/selection.go`, `selection_handler.go`, `status_public.go`
- `internal/exam/sync_admission.go`, tes
- `web/templates/admission/tests.html`, `public_status.html`

## Kriteria selesai
- [ ] Nilai ujian online masuk ke peringkat tanpa input manual; tidak hadir = NULL
- [ ] Nilai ulang memperbarui `applicant_test_scores` dan peringkat
- [ ] Calon siswa melihat kartu tes dengan kode dan bisa login dari HP
- [ ] Ujian milik sekolah lain tidak bisa dipilih (uji lintas sekolah)
