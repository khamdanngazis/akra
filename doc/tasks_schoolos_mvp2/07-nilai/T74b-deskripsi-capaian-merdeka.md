# T74b — Deskripsi capaian Kurikulum Merdeka **[wawancara]**

**Fase:** 07 Nilai
**Dependensi:** T73, T74
**Estimasi:** 2–3 hari

## Tujuan
Rapor format Kurikulum Merdeka (`report.format = merdeka`, T73) memuat deskripsi capaian per siswa per mapel, bukan hanya nilai dan predikat.

## Latar belakang
Ditambahkan saat T73 (21 Oktober 2026): sekolah ingin format rapor bisa dipilih. Pilihan "Kurikulum Merdeka" sudah ada di pengaturan rapor, tetapi baru mengganti sebutan KKM menjadi KKTP. Deskripsi capaian adalah data baru yang ditulis guru, bukan konfigurasi.

## Langkah (usulan, dikonfirmasi lewat wawancara)
1. Migrasi `achievement_descriptions`: `school_id`, `academic_year_id`, `enrollment_id`, `subject_id`, `highlight text` (capaian yang menonjol), `improvement text` (yang perlu ditingkatkan), `updated_by`, `updated_at`. `UNIQUE (enrollment_id, subject_id)`.
2. Halaman isian guru per kelas–mapel, mirip input nilai (T70). Akses sama: guru pengampu (T65).
3. Template deskripsi opsional per mapel (mis. "Ananda {nama} menunjukkan penguasaan yang baik dalam ...") agar guru tidak menulis dari nol.
4. Rapor PDF (T74) memakai deskripsi jika format `merdeka`; nilai akhir dan predikat tetap dari T71/T73.
5. Audit perubahan deskripsi.

## Kriteria selesai
- [ ] Rapor format Merdeka menampilkan deskripsi capaian per mapel
- [ ] Guru tanpa penugasan mapel mendapat `403`
- [ ] Rapor format angka tidak berubah
- [ ] Uji T58 dan T59 diperbarui

## Catatan
- Kerjakan hanya jika sekolah pilot memakai Kurikulum Merdeka.
