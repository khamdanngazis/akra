# T153 — Uji keamanan, akses, dan privasi

**Fase:** 20 Siap Produksi
**Dependensi:** semua task 14–19
**Estimasi:** 2,5 hari

## Tujuan
Memastikan ujian tidak membocorkan soal atau kunci, peserta hanya mengakses miliknya, dan data anak diperlakukan sesuai UU PDP.

## Langkah
1. **Lintas sekolah:** semua route `/ujian/...` dan `/u/...` ada di registry `internal/testutil/routes.go` dengan jenis yang benar; uji T58 lulus.
2. **Akses peserta:**
   - session siswa/pendaftar × semua route staf → ditolak;
   - peserta A × attempt/jawaban/hasil peserta B → 404;
   - token attempt yang dipalsukan atau kedaluwarsa → ditolak.
3. **Kebocoran soal dan kunci:**
   - pemindai otomatis memeriksa setiap respons route peserta (HTML, JSON, cache service worker) dan memastikan tidak ada nilai `answer_key`/`explanation_*` sebelum waktunya;
   - URL gambar soal tidak bisa dibuka sebelum attempt dimulai atau setelah kedaluwarsa.
4. **Matriks peran** (T125) diuji per sel. Termasuk guru non-pengawas, guru mapel lain, bendahara, orang tua (T59), dan pengawas di luar jendela waktu.
5. **Rate limit dan penyalahgunaan:**
   - tebak kode akses (brute force) → terkunci;
   - banjir simpan jawaban → dibatasi per attempt;
   - satu IP lab dengan 40 peserta tetap lolos (T142).
6. **Privasi anak:**
   - **Masa simpan:** attempt, jawaban, dan kejadian disimpan sampai akhir tahun ajaran berikutnya, lalu dianonimkan. Nilai yang sudah masuk rapor mengikuti aturan rapor. Data ujian seleksi calon siswa yang tidak diterima mengikuti T113. Dijalankan job River.
   - Log aplikasi tidak memuat kode akses, isi jawaban, atau nama peserta (uji pemindai log).
   - Ekspor data siswa (hak akses UU PDP) mencakup data ujian.
7. **Tinjauan keamanan** dengan skill `/security-review` dan pemeriksaan manual pada sanitizer HTML soal, impor file (XXE, zip bomb `.docx`), dan unggahan gambar.

## Kriteria selesai
- [ ] Semua uji otomatis lulus di CI
- [ ] Pemindai kebocoran mencakup semua jenis soal dan cache service worker
- [ ] Job masa simpan diuji dengan data kedaluwarsa
- [ ] Temuan tinjauan keamanan ditutup atau dicatat dengan alasan
