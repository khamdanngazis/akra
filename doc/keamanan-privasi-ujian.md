# Keamanan, Akses, dan Privasi Modul Ujian (T153)

Hasil uji keamanan modul ujian sebelum dipakai sekolah. Semua uji otomatis
berjalan di `go test ./...` (lokal dan CI) tanpa layanan berbayar.

## Uji otomatis

| Area | Uji | Berkas |
|---|---|---|
| Lintas sekolah | Registry route mencakup semua `/ujian/...` dan `/u/...`; uji T58 lulus | `internal/testutil/routes.go`, `cmd/server` |
| Sesi peserta × route staf | Sesi siswa dan pendaftar ke semua route staf (≥ 300 request): dialihkan ke `/login` atau 4xx. Sesi staf ke halaman `/u/...`: ditolak | `cmd/server/participant_isolation_test.go` |
| Peserta A × milik B | Attempt, halaman selesai, gambar, dan hasil milik B → 404 | `internal/exam/security_test.go` (`TestParticipantCrossAccess`) |
| Token attempt | Token attempt lain, dipalsukan, kedaluwarsa, kosong → 401 | idem |
| Kebocoran soal dan kunci | Pemindai memeriksa setiap respons GET route peserta di setiap tahap (sebelum mulai, saat mengerjakan, setelah kumpul), balasan simpan, dan skrip service worker serta halaman ujian. Semua jenis soal dan soal bergambar ikut. Uji gagal bila ada route peserta baru yang belum dipindai | `TestParticipantLeakScanner` |
| Gambar soal | Hanya terbuka selama attempt berjalan; setelah dikumpulkan → 404 | idem |
| Matriks peran | Per sel, termasuk guru non-pengawas, guru mapel lain, bendahara, dan pengawas di luar jendela waktu. Orang tua diuji di T59 | `internal/exam/access_test.go` |
| Tebak kode akses | Terkunci setelah batas percobaan | `TestCodeBruteForceLocksOverHTTP` |
| Banjir simpan jawaban | Dibatasi per attempt (20 request beruntun, isi ulang 1/detik) → 429 + `Retry-After`. Attempt lain dari IP yang sama tidak terdampak. Browser mengantre ulang jawaban, tidak hilang | `TestSaveFloodLimitedPerAttempt`, `TestSaveLimiterRefill` |
| Satu IP lab, 40 peserta | Login tetap lolos (T142) | `TestLabLoginsBehindOneIP` |
| Pemindai log | Log tidak memuat kode akses, nama peserta, isi jawaban, atau komentar guru | `TestLogsHaveNoParticipantSecrets` |
| Masa simpan | Job dengan data kedaluwarsa; aman diulang | `internal/exam/retention_test.go` |
| Ekspor data siswa | Memuat data ujian tanpa kunci jawaban; hanya admin_tu; tercatat di audit | `internal/students/data_export_test.go`, `TestStudentExamExport` |
| Header keamanan | `nosniff`, `X-Frame-Options: DENY`, CSP `frame-ancestors 'none'`, `Referrer-Policy`, dan HSTS di luar development | `internal/platform/httpserver/headers_test.go` |

## Masa simpan data ujian

Dijalankan job River `exam_retention` sehari sekali (juga saat server mulai).

- **Ulangan, PTS/PAS, latihan:** attempt, jawaban, dan kejadian disimpan sampai **akhir tahun ajaran berikutnya** setelah tahun ajaran ujian. Bila tahun ajaran berikutnya belum dibuat, batasnya akhir tahun ajaran ujian + 365 hari. Ujian tanpa tahun ajaran: 365 hari setelah sesi berakhir.
- Setelah lewat, per sesi:
  - jawaban (termasuk komentar esai), kejadian integritas, kode akses, dan catatan kirim-rapor dihapus;
  - id perangkat dikosongkan;
  - peserta dilepas dari identitas siswa/pendaftar.
- **Tetap disimpan:** nilai dan durasi attempt tanpa identitas, untuk statistik dan analisis butir.
- **Rapor:** nilai yang sudah dikirim ke rapor mengikuti aturan rapor; tabel nilai rapor tidak disentuh.
- **Tes seleksi:** mengikuti masa simpan pendaftar (T113). Saat pendaftar dianonimkan, data ujiannya ikut dihapus dan dilepas dari identitas. Nilai tes tetap tersimpan.
- Setiap sesi yang diproses tercatat di audit (`exam.data_retention`).

## Ekspor data siswa (hak akses UU PDP)

Menu admin TU: `GET /siswa/{id}/data-pribadi.json`. Isinya identitas, data rinci, wali, riwayat kelas, absensi, nilai, tagihan, pembayaran, dan data ujian.

- **Data ujian:** jawaban, poin, komentar guru, dan kejadian integritas.
- **Tidak ikut:** kunci jawaban dan pembahasan. Itu milik sekolah, bukan data pribadi siswa.
- **Akses:** setiap unduhan tercatat di audit (`student.data_export`) dan tidak di-cache browser.

## Tinjauan keamanan

Tinjauan dengan skill `/security-review` atas perubahan T153, ditambah pemeriksaan manual.

| Bagian | Temuan | Status |
|---|---|---|
| Sanitizer HTML soal (`internal/exam/richtext`) | Allowlist ketat. `script`, `style`, `iframe`, `svg`, `math`, `form`, dan sejenisnya dibuang beserta isinya. Tidak ada atribut `style`/`on*`/`href`. `img` disusun ulang hanya dengan `data-asset` (UUID) dan `alt`; `src` diisi server | Aman |
| Impor Word `.docx` | Batas jumlah dan ukuran entri zip, baca nyata dibatasi `LimitReader` (zip bomb). `DOCTYPE` ditolak; parser XML Go tidak memproses entitas eksternal (XXE) | Aman |
| Impor Excel `.xlsx` | Batas ukuran unggahan, `UnzipSizeLimit` 100 MB dan `UnzipXMLSizeLimit` 50 MB, batas jumlah soal | Aman |
| Impor Moodle XML/GIFT | Batas ukuran, decoder `Strict`, tanpa entitas eksternal, batas jumlah soal | Aman |
| Unggahan gambar | Batas body request; tipe dari isi berkas (PNG/JPEG/WebP/GIF, SVG ditolak). Dimensi dibaca dulu tanpa decode penuh; di atas 8000 px ditolak (decompression bomb). Gambar besar diperkecil ke 1600 px | Aman |
| Header keamanan HTTP | Belum ada header global, sehingga halaman bisa dibingkai situs lain (clickjacking pada tombol kumpulkan/simpan) dan HTML tanpa `nosniff` | **Ditutup** di T153: middleware `SecurityHeaders` |
| Banjir simpan jawaban | Belum ada batas per attempt | **Ditutup** di T153: token bucket per attempt |
| `/security-review` atas kode baru (ekspor data, masa simpan, limiter, header) | Tidak ada temuan HIGH/MEDIUM. Ekspor hanya admin_tu dan dibatasi `school_id`. SQL masa simpan memakai subquery konstan dengan parameter | Aman |

### Dicatat, tidak diubah

- **Pembatas simpan disimpan di memori per replika.** Dengan beberapa replika, batas efektif dikali jumlah replika. Ini tetap mencegah banjir dari satu attempt dan tidak butuh Redis.
- **Ekspor data siswa memuat teks soal ujian yang sudah dikerjakan.** Admin TU sebaiknya menyerahkan berkas setelah semua sesi ujian terkait selesai. Kunci jawaban tidak pernah ikut.
