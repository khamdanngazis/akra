# Rencana MVP 4 SchoolOS — Ujian Online Terintegrasi

Versi 2 (2026-11-16): rencana disusun ulang supaya modul ujian **siap produksi dan benar-benar terpakai**, bukan sekadar bisa didemokan.

Acuan:
- modul AKRA yang sudah ada: tes seleksi (T93a, T119), metode seleksi (T118), komponen dan input nilai (T69–T70), deskripsi capaian (T74b), portal orang tua (T75), penugasan guru (T65);
- modul Quiz Moodle dan ANBK sebagai pembanding cara kerja yang sudah dikenal sekolah.

## 1. Tujuan
Sekolah bisa menjalankan **seluruh siklus ujian** di AKRA:
1. menulis soal (termasuk dari Word);
2. menyusun ulangan, PTS/PAS, dan tes seleksi;
3. mengatur ruang, sesi, dan pengawas;
4. melaksanakan ujian di HP maupun lab;
5. menilai;
6. menganalisis;
7. mengirim nilai ke rapor atau ke peringkat pendaftaran.

Semua tanpa kertas dan tanpa mengetik ulang.

## 2. Siapa yang Memakai dan Apa yang Mereka Butuhkan

| Pemakai | Perangkat | Pekerjaan utama | Yang membuat mereka berhenti memakai |
|---|---|---|---|
| **Guru mapel** | Laptop, kadang HP | Menulis soal, membuat ulangan untuk kelasnya, koreksi esai, remedial, mengirim nilai ke rapor | Harus mengetik ulang soal dari Word; rumus sulit; koreksi esai lambat; dokumen administrasi (kisi-kisi, analisis butir) tetap dibuat manual |
| **Koordinator ujian** (wakasek kurikulum/panitia, sering seorang guru) | Laptop | Mengatur PTS/PAS satu sekolah: jadwal mapel per hari, ruang, sesi, pengawas, kartu peserta, berita acara | Membagi ratusan siswa ke ruang dan sesi secara manual; tidak tahu kondisi semua ruang saat ujian |
| **Pengawas** (guru) | HP | Membacakan token, memantau, menolong siswa yang bermasalah | Harus lari ke TU atau operator untuk mereset login; tidak tahu siapa yang keluar dari halaman |
| **Siswa** (SD kelas atas, SMP, SMA) | HP sendiri atau komputer lab | Login, mengerjakan, mengumpulkan | Lupa password; sinyal putus lalu jawaban hilang; tampilan membingungkan; HP lama berat |
| **Calon siswa dan orang tua** | HP | Tes seleksi di sekolah atau dari rumah | Tidak tahu cara login; takut jawaban hilang |
| **TU pendaftaran** | Laptop | Menjadwalkan tes seleksi, melihat nilai masuk ke peringkat | Mengetik nilai dari kertas |
| **Kepala sekolah** | HP/laptop | Ringkasan pelaksanaan dan hasil | Data tersebar |
| **Orang tua siswa** | HP | Melihat jadwal dan nilai ulangan anak | Tidak ada informasi |

## 3. Prinsip Produk dan UI
1. **Tidak pernah kehilangan pekerjaan.**
   - Jawaban peserta disimpan otomatis dan dicadangkan di perangkat.
   - Draf soal guru juga tersimpan otomatis.
   - Tidak ada hapus permanen untuk soal atau ujian; yang ada arsip.
2. **Satu jalan yang jelas.**
   - Membuat ujian memakai wizard bertahap dengan cek kesiapan, seperti gelombang pendaftaran (T117).
   - Halaman detail selalu punya kartu "langkah berikutnya", seperti T116.
3. **Desain sesuai perangkat pemakai.**
   - Siswa dan pengawas: HP lebih dulu.
   - Guru dan koordinator: laptop lebih dulu, tetapi tetap bisa dipakai di tablet.
4. **Mirip yang sudah dikenal.** Halaman ujian meniru pola ANBK: grid nomor soal, ragu-ragu, token. Siswa tidak perlu belajar dari nol.
5. **Bahasa sederhana dan status selalu terlihat**, misalnya "Tersimpan", "Offline — jawaban aman", "Menunggu koreksi esai 12 jawaban".
6. **Kosong berarti panduan.**
   - Halaman kosong menampilkan langkah awal dan contoh.
   - Sekolah baru mendapat bank soal contoh dan ujian latihan.
7. **Dokumen administrasi keluar otomatis:** kisi-kisi, kartu soal, daftar hadir, berita acara, analisis butir, daftar remedial. Ini pekerjaan yang paling menyita waktu guru dan panitia.
8. **Konsisten dengan AKRA:** navigasi kembali (T121/T122), komponen tabel/kartu, bahasa UI Indonesia, kode dan komentar sesuai aturan proyek.

## 4. Standar Siap Produksi
Berlaku untuk **semua** task MVP 4. Task belum selesai bila standar ini tidak terpenuhi.

1. **Keamanan soal dan kunci**
   - Kunci jawaban dan pembahasan tidak pernah dikirim ke perangkat peserta. Penilaian selalu di server.
   - Gambar soal disajikan lewat URL bertanda tangan (presign) yang hanya berlaku selama attempt.
   - Soal tidak bisa dibuka peserta sebelum sesi dimulai.
2. **Keandalan**
   - Target ketersediaan 99,9% selama jendela sesi ujian terjadwal.
   - Jawaban yang sudah dikonfirmasi server tidak boleh hilang (RPO 0 untuk jawaban).
   - Deploy diblokir selama ada sesi ujian aktif (T145).
3. **Kinerja**
   - p95 simpan jawaban < 300 ms; p95 buka ujian < 2 detik di 4G.
   - Halaman ujian < 150 KB transfer awal di luar gambar, termasuk CSS dan font rumus.
   - Mulus di HP Android 2 GB RAM.
4. **Skala**
   - 500 peserta bersamaan per sekolah, dan **5.000 bersamaan di seluruh platform** (musim PTS/PAS serentak).
   - Aplikasi tetap tanpa state di memori, sehingga Railway bisa menjalankan lebih dari satu replika.
5. **Kompatibilitas** (diuji di T142)
   - Chrome Android ≥ 90, Safari iOS ≥ 15;
   - Chrome/Edge desktop 2 tahun terakhir, Firefox ESR;
   - layar 360 px sampai 1920 px.
6. **Aksesibilitas**
   - WCAG 2.1 AA untuk halaman peserta;
   - ukuran huruf bisa diperbesar, kontras tinggi, bisa dipakai dengan keyboard;
   - waktu tambahan untuk peserta berkebutuhan khusus.
7. **Multi-sekolah dan akses**
   - Setiap route masuk registry `internal/testutil/routes.go` (uji T58/T59).
   - Session peserta tidak bisa membuka route staf.
8. **Privasi anak (UU PDP)**
   - Data jawaban dan kejadian hanya untuk guru, pengawas, dan koordinator terkait.
   - Masa simpan ditetapkan (T153).
   - Kode akses disimpan sebagai hash dan tidak tercatat di log.
9. **Teramati**
   - Setiap endpoint peserta punya metrik latensi dan error.
   - Ada alert bila error simpan jawaban naik (T145).
10. **Zona waktu sekolah:** jadwal ujian memakai zona waktu sekolah (WIB/WITA/WIT), bukan jam server (T126).

## 5. Keputusan

**Semua keputusan diambil 2026-11-16:**
1. **Modul sendiri di AKRA** (bukan integrasi Moodle), dengan impor Moodle XML/GIFT. LTI 1.3 menjadi kandidat MVP berikutnya.
2. **Peserta:** calon siswa (tes seleksi) **dan** siswa (ulangan, PTS/PAS).
3. **Perangkat:** HP dan lab. Skala ratusan peserta per sekolah.
4. **Rumus matematika wajib.** Gambar saja tidak cukup.
5. **Safe Exam Browser opsional.** Yang wajib: bunyi tanda keluar halaman, diatur per sesi lewat lokasi ujian (di ruang / jarak jauh).
6. **Pembahasan:** calon siswa tidak melihat nilai maupun pembahasan; untuk siswa diatur per paket.
7. **Bank soal dipakai bersama per mapel.** Yang bisa mengubah soal hanya pembuat soal dan admin_tu.

8. **Login siswa memakai model gabungan** (T124):
   - identitas siswa yang sesungguhnya: peran `siswa`, session terikat `student_id`;
   - di MVP 4, login dengan **NIS + kode kartu ujian**;
   - tabel kredensial sudah mendukung password permanen untuk portal siswa di MVP berikutnya.
9. **Jadwal rilis tidak dipaksa.** Target bulan di bagian 7 adalah arah, bukan tenggat. Kualitas dan standar siap produksi (bagian 4) didahulukan; rilis bergeser bila kriteria pilot (T155) belum terpenuhi.
10. **Stimulus bersama dan tulisan Arab didukung sejak awal:**
    - **stimulus bersama:** teks bacaan, tabel, atau gambar untuk beberapa soal; tetap berkelompok saat diacak; tampil dua kolom di desktop;
    - **tulisan Arab:** teks Arab berharakat untuk PAI/Bahasa Arab di editor, impor, halaman ujian, dan cetak.

    Dicakup di T123, T127–T131, T133, T135, T138, T142.

## 6. Daftar Fase

| Folder | Task | Isi | Estimasi |
|---|---|---|---|
| `14-fondasi/` | T123–T126 | Spike teknis dan ADR, identitas siswa, peran koordinator dan pengawas serta aktivasi modul, zona waktu sekolah | 2 minggu |
| `15-bank-soal/` | T127–T132 | Bank soal dan stimulus, editor soal dengan rumus dan tulisan Arab, impor Word, impor Moodle, Excel, kisi-kisi dan kartu soal | 4,5 minggu |
| `16-persiapan-ujian/` | T133–T137 | Wizard buat ujian, agenda PTS/PAS, dokumen ujian, cek kesiapan dan ujian latihan, susulan dan remedial | 3,5 minggu |
| `17-pelaksanaan/` | T138–T142 | Halaman ujian (termasuk stimulus dan tulisan Arab), simpan otomatis dan offline, timer, integritas dan bunyi tanda, kompatibilitas perangkat | 3,5 minggu |
| `18-hari-h/` | T143–T145 | Layar pengawas, pusat kendali koordinator, keandalan dan observabilitas | 2 minggu |
| `19-penilaian/` | T146–T151 | Penilaian otomatis, koreksi esai, analisis butir dan TP, integrasi seleksi, integrasi rapor, hasil untuk siswa dan orang tua | 3 minggu |
| `20-siap-produksi/` | T152–T155 | Uji beban, uji keamanan dan privasi, panduan dan onboarding, pilot dan rollout | 2 minggu + pilot |
| `21-rapikan/` | T156–T162 | Rapikan navigasi dan tampilan: menu seluruh aplikasi berkelompok dengan pusat Pengaturan, kerangka modul ujian (sub-navigasi, breadcrumb, judul), beranda modul, daftar dan detail ujian, bank soal, komponen halaman bersama | 2,5 minggu |
| `22-modul-sekolah/` | T163–T169 | Modul dan paket per sekolah (ADR 0002): katalog modul, penegakan di route/beranda/portal/laporan, fitur lintas modul dan peran, halaman modul untuk sekolah (baca saja), pengelola untuk tim implementator lewat CLI, uji kombinasi | 2 minggu |
| `23-perbaikan/` | T170– | Perbaikan dari pilot (T155), diprioritaskan P0–P3 sebelum gelombang rollout berikutnya | sesuai kebutuhan |

Total perkiraan: **± 20 minggu pengembangan** (± 100 hari kerja, satu pengembang penuh waktu dibantu Claude), ditambah pilot. Pekerjaan dibagi ke tiga rilis agar sekolah bisa memakai lebih awal.

## 7. Rilis Bertahap

| Rilis | Isi | Task | Perkiraan pakai (bukan tenggat) |
|---|---|---|---|
| **R1 — Tes seleksi online** | Calon siswa ujian online; nilai masuk ke peringkat | T123–T128, T133 (tanpa varian), T135 (kartu), T136, T138–T141, T143, T145, T146, T149, T152–T153 (lingkup R1), T154 (panduan TU) | Gelombang akhir pendaftaran 2027 (Feb–Mar 2027) |
| **R2 — Ulangan guru** | Guru membuat ulangan untuk kelasnya, dari Word; koreksi, analisis, rapor | T129–T132, T137, T142, T147, T148, T150, T151 | Semester genap 2026/2027 (Mar–Mei 2027) |
| **R3 — PTS/PAS sekolah** | Agenda ujian satu sekolah, ruang, sesi, pengawas, pusat kendali, uji beban platform | T134, T144, sisa T135, uji beban skala platform, T155 | PAS/PAT Juni 2027 |

Setiap rilis diaktifkan **per sekolah** lewat aktivasi modul (T125). Pilot dulu, lalu dibuka bertahap.

## 8. Gerbang Masuk dan Urutan Kritis
**Gerbang masuk:** task MVP 4 dimulai setelah:
1. MVP 3 lulus T105 (uji dengan operator dan Dapodik asli);
2. ADR hasil spike T123 disetujui. Spike T123 sendiri boleh dikerjakan lebih awal.

**Urutan kritis:**
1. T123 (spike) lebih dulu: rendering rumus, impor Word, dan protokol simpan jawaban adalah risiko terbesar.
2. T124, T125, T126 → T127 → T128
3. T133 → T136 → T138 → T139 → T140 → T141 → T143 → T146 → T149 (jalur R1)
4. T145 dan T152 wajib lulus sebelum R1 dipakai sekolah.

## 9. Ukuran Keberhasilan (diukur saat pilot dan setelah rilis)
- **0 jawaban hilang.**
- ≥ 95% attempt selesai tanpa bantuan pengawas.
- Satu kelas (30 siswa) sudah mengerjakan dalam ≤ 5 menit sejak token dibacakan.
- Guru membuat ulangan 20 soal: ≤ 15 menit dari nol, ≤ 5 menit dari file Word.
- Koreksi 30 esai pendek ≤ 20 menit.
- ≥ 70% guru pilot membuat ujian kedua tanpa didampingi.
- Tiket bantuan ≤ 1 per 100 peserta.

## 10. Risiko dan Mitigasi

| Risiko | Dampak | Mitigasi |
|---|---|---|
| Rumus dari Word (MathType lama berupa objek OLE/WMF) tidak bisa dikonversi | Guru kecewa karena impor tidak lengkap | Spike T123; template Word resmi dengan Equation bawaan Word; rumus yang gagal dilaporkan per soal dan bisa diperbaiki di editor |
| Internet sekolah lemah atau WiFi lab penuh | Peserta tidak bisa login atau menyimpan | Muat soal sekali, mode offline dengan service worker (T139), hemat data (T142), naskah cadangan cetak |
| Beban puncak serentak saat musim PAS | Server lambat atau tumbang | Uji beban platform (T152), replika, protokol simpan jawaban ringan, jendela deploy (T145) |
| Guru kurang terbiasa teknologi | Modul tidak dipakai | Impor Word, contoh bawaan, wizard, panduan dalam aplikasi, pendampingan saat pilot (T154–T155) |
| HP lama atau browser bawaan vendor | Tampilan rusak | Matriks perangkat dan uji di HP murah (T142), cek perangkat sebelum ujian (T136) |
| Kecurangan (kirim jawaban lewat chat, foto layar) | Nilai tidak valid | Acak soal dan opsi, varian paket, bunyi tanda dan log kejadian, analisis kemiripan esai (T147). Pencegahan mutlak di luar cakupan |

## 11. Di Luar Cakupan
- Integrasi LTI/Web Services dengan Moodle.
- Proctoring kamera atau AI.
- Server ujian lokal di lab tanpa internet sama sekali. Yang ada hanya toleransi putus koneksi (T139).
- Computerized adaptive testing.
- LMS (materi, tugas, forum).
- Penilaian esai otomatis dengan AI. Kandidat setelah data koreksi guru cukup.
- Tryout berbayar untuk umum.
