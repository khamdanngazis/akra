# T99 — Formulir F-PD terisi (PDF)

**Fase:** 11 Bantu Dapodik
**Dependensi:** T85, T86, T87
**Estimasi:** 2 hari

## Tujuan
Sekolah bisa mengunduh formulir peserta didik (F-PD) yang sudah terisi dari data SchoolOS, untuk arsip, tanda tangan orang tua, atau diserahkan ke operator, tanpa membuat formulir sendiri.

## Langkah
1. PDF dibuat dengan `go-pdf/fpdf` (seperti kuitansi T41 dan rapor T74).
2. Isi dan penomoran mengikuti F-PD 2027: Data Pribadi (1–27), Ayah (28–34), Ibu (35–41), Wali (42–47), Kontak (48–50), Data Periodik, Prestasi, Beasiswa, Kesejahteraan, Registrasi, Pendaftaran Keluar.
3. Pilihan ditulis sebagai kode dan label (mis. `01) Islam`). Field kosong dicetak sebagai garis isian agar bisa dilengkapi tangan.
4. Kop sekolah dari pengaturan, kolom tanda tangan wali kelas dan orang tua/wali, serta kalimat pernyataan tanggung jawab kebenaran data seperti F-PD.
5. Unduh per siswa `GET /siswa/{id}/formulir-pd.pdf` dan massal per kelas (satu PDF banyak halaman) lewat job River jika lebih dari 40 siswa.
6. Versi kosong `GET /formulir/pd-kosong.pdf` untuk pendaftar yang mengisi di kertas.
7. Akses: `admin_tu`, `kepala_sekolah`. Wali kelas mendapat versi dengan NIK ter-mask.

## File yang disentuh
- `internal/dapodik/fpd_pdf.go`, `fpd_pdf_test.go`
- `internal/dapodik/handler.go`

## Kriteria selesai
- [ ] Siswa dengan data lengkap menghasilkan PDF dengan semua nomor field terisi
- [ ] Nama panjang dan alamat panjang tidak keluar dari kolom (dipotong rapi atau dibungkus)
- [ ] PDF massal 40 siswa selesai tanpa membuat halaman macet
- [ ] Versi wali kelas tidak memuat NIK utuh

## Catatan implementasi
PDF kelas dibuat langsung untuk maksimal 100 siswa (bukan job River). Versi ter-mask untuk wali kelas tidak dibuat karena guru belum punya akses ke halaman ini.
