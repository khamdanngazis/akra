# Daftar Task MVP 4 SchoolOS — Ujian Online Terintegrasi

Acuan: modul tes seleksi pendaftaran (T93a, T119), komponen dan input nilai (T69–T70), penugasan guru (T65), serta cara kerja modul Quiz Moodle sebagai pembanding.

## Latar Belakang
- Saat ini tes seleksi dikerjakan di kertas. TU mengisi nilai secara manual (T93a), dibantu acuan penilaian dan lembar PDF (T119). Ulangan dan PTS/PAS siswa juga di kertas, lalu nilainya diketik guru ke input nilai (T70).
- MVP 4 menambah **ujian online** di dalam AKRA untuk dua kelompok peserta:
  1. **Calon siswa:** tes seleksi pendaftaran. Nilainya langsung masuk ke tes seleksi dan peringkat (T93a, T93b, T118).
  2. **Siswa:** ulangan harian dan PTS/PAS. Nilainya langsung masuk ke komponen nilai rapor (T69–T70).
- Peserta memakai **HP sendiri maupun komputer lab**. Dalam satu sekolah bisa ada **ratusan peserta bersamaan**.

## Keputusan Arah (diambil 2026-11-16)
- **Modul sendiri di AKRA, bukan integrasi Moodle** (pilihan C). Alasannya:
  - Sekolah tidak perlu merawat server Moodle.
  - Calon siswa tidak perlu akun Moodle.
  - Nilai masuk ke AKRA tanpa sinkronisasi.
- **Kompatibel dengan format soal Moodle:** impor Moodle XML dan GIFT (T125), supaya bank soal yang sudah ada di Moodle tidak diketik ulang.
- Integrasi LTI 1.3 dengan Moodle sekolah: di luar cakupan MVP 4, kandidat MVP berikutnya.

## Gerbang Masuk
Task MVP 4 **tidak dimulai** sebelum:
1. MVP 3 lulus T105 (uji dengan operator dan Dapodik asli).
2. Keputusan yang ditandai **[keputusan]** di bawah sudah diambil.

## Aturan Task
- Sama dengan MVP 1–3: kecil, punya dependensi, punya kriteria selesai yang bisa dicek.
- Setiap route baru wajib masuk daftar uji lintas sekolah (`internal/testutil/routes.go`, T58) dan uji orang tua (T59).
- Route peserta ujian (login peserta, halaman ujian, simpan jawaban) wajib:
  - punya rate limit;
  - hanya membuka ujian dan jawaban milik peserta itu sendiri;
  - tidak membuka data sekolah lain.
- **Kunci jawaban tidak pernah dikirim ke perangkat peserta.** HTML/JSON halaman ujian hanya memuat soal dan opsi. Penilaian selalu di server.
- **Waktu ujian dihitung di server.** Jam perangkat peserta hanya untuk tampilan hitung mundur.
- **Jawaban tidak boleh hilang karena koneksi.** Setiap jawaban disimpan otomatis dan dicadangkan di perangkat sampai server mengonfirmasi (T131).
- Beban server dirancang untuk **500 peserta bersamaan per sekolah** (T139).
  - Soal dimuat sekali di awal.
  - Simpan jawaban berupa request kecil.
  - Tidak ada job River per jawaban.
- Nama paket kode: `internal/exam`. Label UI: **"Ujian Online"**.

## Daftar Fase

| Folder | Task | Isi | Estimasi |
|---|---|---|---|
| `14-bank-soal/` | T123–T126 | Model bank soal, editor soal dan media, impor Moodle XML/GIFT, impor/ekspor Excel | 2 minggu |
| `15-paket-dan-jadwal/` | T127–T129 | Paket ujian, jadwal sesi dan peserta, kartu peserta dan login peserta | 1,5 minggu |
| `16-pelaksanaan/` | T130–T134 | Halaman ujian, simpan otomatis tahan putus koneksi, timer server, antikecurangan, pengawasan langsung | 2,5 minggu |
| `17-penilaian/` | T135–T138 | Penilaian otomatis dan esai, hasil dan analisis butir, integrasi tes seleksi, integrasi nilai rapor | 1,5 minggu |
| `18-uji/` | T139–T141 | Uji beban ratusan peserta, uji lintas sekolah dan privasi, pilot di sekolah | 1 minggu |

Total perkiraan: 8,5 minggu.

## Urutan Kritis
1. T123 → T124 (bank soal dulu; tanpa soal tidak ada yang bisa diuji)
2. T127 → T128 → T129 → T130 → T131 → T132 (jalur minimum ujian bisa berjalan)
3. T135 → T137 (tes seleksi pendaftaran didahulukan, karena pendaftaran sekolah swasta umumnya Desember–Februari)
4. T133, T134, T136, T138
5. T125, T126 (impor bisa paralel setelah T124)
6. T139 sebelum pilot; T140 dan T141 terakhir

**Titik rilis pertama (tes seleksi online):** T123, T124, T127–T132, T135, T137, T139. Fitur lain menyusul.

## Keputusan yang Harus Diambil
1. **[keputusan] Login siswa (T129).** Siswa belum punya akun AKRA. Pilihan:
   - (a) **Kartu peserta per ujian.** NIS + kode acak yang dicetak di kartu dan hanya berlaku di jadwal ujian itu. Tanpa akun permanen. *Saran.*
   - (b) Akun siswa permanen (peran baru `siswa`), lengkap dengan aktivasi dan lupa password.
2. **[keputusan] Rumus matematika (T124).** Perlu dukungan rumus (KaTeX, ditulis seperti `\frac{1}{2}`) sejak MVP 4, atau cukup gambar?
3. **[keputusan] Aplikasi exam browser (T133).** Untuk lab, perlu dukungan Safe Exam Browser (cek Config Key) atau aplikasi "Exambro" Android? Saran: MVP 4 cukup antikecurangan berbasis browser, dengan dukungan SEB sebagai opsi per jadwal.
4. **[keputusan] Pembahasan setelah ujian (T136).** Bolehkah peserta melihat nilai dan pembahasan setelah ujian? Saran:
   - calon siswa: tidak boleh;
   - siswa: diatur per paket.
5. **[keputusan] Berbagi bank soal (T123).** Bank soal milik guru pribadi, atau dipakai bersama per mapel di sekolah? Saran: per mapel sekolah, dan yang bisa mengubah hanya pembuat soal dan admin_tu.

## Di Luar Cakupan
- Integrasi LTI/Web Services dengan Moodle.
- Proctoring kamera atau AI (rekam wajah, deteksi orang lain).
- Mode ujian luring penuh, yaitu server lokal di lab tanpa internet. Yang ada hanya toleransi putus koneksi sementara (T131).
- Computerized adaptive testing (soal menyesuaikan kemampuan).
- Tugas/LMS (materi, pengumpulan tugas, forum).
- Ujian berbayar atau tryout untuk umum.
