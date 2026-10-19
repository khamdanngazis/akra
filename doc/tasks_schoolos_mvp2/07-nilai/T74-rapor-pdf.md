# T74 — Rapor PDF sederhana **[wawancara]**

**Fase:** 07 Nilai
**Dependensi:** T72, T73, T41
**Estimasi:** 2–3 hari

## Tujuan
Rapor per siswa bisa dicetak sebagai PDF dengan nilai akhir, huruf, dan kehadiran, sesuai format yang disepakati di wawancara.

## Syarat mulai
Layout rapor disetujui oleh minimal satu kepala sekolah calon pilot. Tanpa persetujuan layout, task ini tidak dimulai.

## Langkah
1. Buat `internal/grades/report_pdf.go` memakai `maroto` atau `gofpdf`, mengikuti pola T41.
2. Isi rapor:
   - Identitas siswa, kelas, tahun ajaran, semester
   - Tabel mapel: KKM, nilai akhir, huruf, catatan (kosong jika tidak diisi)
   - Ringkasan kehadiran dari T27
   - Nama kepala sekolah dan wali kelas dari T73
3. Hanya tampilkan rapor yang semua mapelnya lengkap. Jika ada yang belum lengkap, tampilkan daftar mapel yang kosong dan jangan cetak.
4. Rute `GET /nilai/rapor/{enrollmentID}.pdf`. Akses: wali kelas dan admin. Orang tua mengunduh lewat portal (T75).
5. Audit: `report_card.print`.

## File yang disentuh
- `internal/grades/report_pdf.go`, `report_pdf_test.go`
- `internal/grades/handler.go` (route)

## Kriteria selesai
- [ ] Rapor siswa dengan semua nilai lengkap terbentuk dan terbuka dengan benar
- [ ] Rapor dengan satu mapel kosong ditolak dan menyebut mapel tersebut
- [ ] Angka di rapor sama dengan rekap T72
- [ ] Setiap cetak tercatat di audit

## Catatan
- Layout disimpan sebagai satu fungsi. Perubahan layout tidak mengubah data.
