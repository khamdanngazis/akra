# Uji MVP4: beban ujian online dan rencana kapasitas (T152)

Dokumen ini mencatat uji beban ujian online dan rencana kapasitas untuk musim PTS/PAS. Semua angka di bawah berasal dari **uji lokal** (bukan staging Railway); lihat bagian [Batasan](#batasan) sebelum memakainya untuk keputusan biaya.

## Alat

| Alat | Isi |
|---|---|
| `scripts/loadtest` (Go) | `seed`: data uji; `run`: menjalankan skenario dan memverifikasi hasil |
| `cmd/savesim -lb` | load balancer lokal (round-robin, melewati replika yang tidak sehat) untuk dua replika |

`run` menjalankan **alur peserta sebenarnya** lewat HTTP, sama seperti browser:

1. halaman login (token CSRF dan cookie) → login dengan nomor dan kode kartu;
2. beranda → halaman konfirmasi → **mulai** dengan token sesi yang diambil pengawas dari `token.json`;
3. halaman ujian beserta 10 gambar soal (6 sekaligus, seperti browser);
4. **simpan** berkala dengan antrian per peserta dan `seq` naik; yang gagal dikirim ulang dengan jeda bertambah (T139);
5. **kumpul**. Sebagian peserta sengaja tidak kumpul agar job kumpul otomatis ikut teruji.

Selama uji, 15 pengawas per sekolah memantau layar pengawas dan token sesi tiap 10 detik. Setiap peserta mencatat jawaban terakhirnya. Setelah uji, catatan itu dicocokkan dengan database: **jawaban hilang** = jawaban terakhir klien yang tidak sama dengan yang tersimpan. Gangguan jaringan (`-fail`) menggagalkan request acak: separuh sebelum terkirim, separuh setelah server menjawab (jawaban hilang di jalan).

### Data uji

`seed` membuat sekolah `ujibeban-1` … `ujibeban-N`. Setiap sekolah berisi:

- 500 siswa di 15 kelas;
- satu ujian ulangan 40 soal, 10 di antaranya bergambar (JPEG 800×500, ±60 KB, diproses seperti unggahan guru);
- satu sesi per kelas dengan pengawas;
- kartu peserta berkode, yang dibuat lewat fungsi aplikasi yang sama dengan wizard dan cetak kartu.

Hanya untuk database uji beban. Jangan pernah menjalankannya di database produksi atau pilot.

```sh
createdb akra_load
DATABASE_URL=postgres://.../akra_load migrate -path db/migrations -database "$DATABASE_URL" up
DATABASE_URL=... STORAGE_LOCAL_DIR=/tmp/akra-load go run ./scripts/loadtest seed -schools 10 -start-in 3m -duration 90 -out loadtest/exam-data.json

# S1–S3 satu sekolah
DATABASE_URL=... go run ./scripts/loadtest run -data loadtest/exam-data.json -url http://localhost:8091 -schools 1 \
  -login-window 2m -work 90m -save-interval 20s -submit-window 1m -no-submit 0.05 -wait-autosubmit 10m -pids <pid server>
# S4: -schools 10 ke load balancer; S5: tambah -fail 0.10 dan matikan/restart replika selama berjalan
```

`run` keluar dengan status 1 bila target tidak tercapai, dan mencetak p50/p95/p99 per endpoint, error server, jawaban hilang, puncak koneksi database, serta CPU proses server.

## Lingkungan uji

| | |
|---|---|
| Mesin | laptop WSL2, 8 inti, RAM 7 GB. Postgres (Docker), replika aplikasi, load balancer, dan driver beban berjalan di mesin yang **sama** |
| Postgres | 18, `max_connections` 100, data uji di DB terpisah `akra_load` |
| Aplikasi | build `cmd/server` saat uji, `APP_ENV=development`, penyimpanan gambar lokal (lewat aplikasi; di produksi langsung dari R2) |
| Masa mengerjakan | dipersingkat dari 90 menit menjadi 5–8 menit. Laju simpan per detik sama dengan ujian sebenarnya; yang berbeda hanya lamanya |

## Hasil

| Skenario | Status |
|---|---|
| S1–S3 satu sekolah (500) | **Lulus** |
| S4 musim PAS (5.000) | **Belum**: gagal di mesin lokal; perlu diulang di staging |
| S5 gangguan (500, replika restart/mati, 10% jaringan gagal) | **Lulus**, 0 jawaban hilang |

### S1–S3: satu sekolah, 500 peserta (satu replika, pool 20)

Login serentak 500 peserta dalam 2 menit setelah sesi dibuka, simpan tiap ±20 detik, kumpul serentak dalam 1 menit terakhir; 5% tidak kumpul.

| Endpoint | Jumlah | p50 | p95 | p99 |
|---|---:|---:|---:|---:|
| login | 500 | 29 ms | 71 ms | 102 ms |
| mulai | 500 | 18 ms | 45 ms | 83 ms |
| **buka ujian** (halaman + 10 gambar) | 500 | 61 ms | **147 ms** | 204 ms |
| **simpan** | 13.509 | 4 ms | **7 ms** | 12 ms |
| kumpul | 476 | 22 ms | 43 ms | 60 ms |
| layar pengawas | 1.530 | 23 ms | 77 ms | 233 ms |

- **Error server: 0** dari 30.075 request.
- **Jawaban hilang: 0** dari 14.504 jawaban yang dicocokkan.
- Semua attempt dikumpulkan: 476 oleh peserta, 24 oleh job kumpul otomatis setelah tenggat.
- Koneksi database puncak 20 (sama dengan ukuran pool); CPU server rata-rata 0,09 inti, puncak 0,65 inti.
- **Lulus** semua target.

Catatan: pada percobaan pertama, mesin uji tertidur 11 menit di tengah uji. Job kumpul otomatis tertahan, dan alert "Kumpul otomatis tertunda > 3 menit" (T145) menyala sesuai harapan. Job langsung pulih begitu mesin aktif kembali.

### S4: musim PAS, 10 sekolah × 500 = 5.000 peserta: **belum lulus, perlu staging**

Konfigurasi: dua replika (pool `DB_MAX_CONNS=32`, total 64 dari batas 100) di belakang load balancer lokal, jam mulai sama, login 5.000 peserta dalam 2 menit.

| Putaran | Hasil |
|---|---|
| 5.000 peserta | Runtuh saat lonjakan login: request habis waktu, ulang-coba klien menambah beban, error 32%, simpan p95 15,8 dtk. **Jawaban hilang tetap 0** (8.874 dicocokkan) |
| 2.000 peserta (1) | Lonjakan login lambat (login p95 15 dtk, buka ujian p95 36 dtk); simpan saat stabil tetap cepat (p50 6 ms); 0 hilang |
| 2.000 peserta (2), dengan profil CPU | **Lulus**: simpan p95 42 ms, buka ujian p95 913 ms, 0 error, 0 dari 40.556 hilang |
| 2.000 peserta (3), setelah perbaikan renderer | Simpan p95 339 ms, buka ujian p95 8 dtk; 0 error, 0 hilang |

Hasil di atas ±1.000 peserta **tidak stabil di mesin ini**: putaran yang sama memberi hasil sangat berbeda, padahal CPU replika hanya ±0,9 inti dan puncaknya 3,3 dari 8 inti. Yang jenuh adalah mesinnya: 2.000–5.000 klien simulasi, load balancer, dan Postgres berebut inti yang sama. Selain itu, mesin dua kali tertidur di tengah uji. Karena itu **S4 harus diulang di staging**; lihat [Uji S4 di staging](#uji-s4-di-staging).

Temuan dari S4 lokal:

- **Profil CPU saat lonjakan** (replika A, 90 detik):
  - `html/template.Clone` 7,4% CPU, dan GC 22%: renderer menyalin seluruh template di setiap request;
  - argon2 untuk login ±15% (sengaja mahal; satu replika memverifikasi ±205 kode/detik);
  - render fragmen layar pengawas 12%;
  - gzip ±5%.
- **Perbaikan yang diterapkan:** renderer memakai ulang salinan template lewat `sync.Pool` per halaman. `Clone` turun dari 7,4% menjadi 2,1% CPU dan GC dari 22% menjadi 18,7%. Diuji: 400 render paralel dengan token CSRF yang tidak tertukar (`-race`).
- **pprof opsional:** `PPROF_ADDR=127.0.0.1:6060` membuka profiler Go di alamat terpisah. Selalu mati di production.
- **Batas login per IP:** 3.000 login per 15 menit per IP. Uji dari satu mesin (semua dari satu IP) akan kena batas ini setelah 3.000 login. Uji di staging harus memakai beberapa mesin/IP, atau ≤3.000 peserta per IP per 15 menit. Untuk sekolah sungguhan, satu sekolah 500–1.000 murid di balik satu IP NAT masih jauh di bawah batas.
- **Reaksi berantai saat jenuh:** request yang habis waktu membatalkan query, pgx menutup koneksinya, lalu koneksi baru dibuka beramai-ramai. Di uji lokal ini muncul sebagai ratusan `failed to connect` ke Postgres, sementara log Postgres bersih. Ini akibat beban berlebih, bukan penyebabnya; di staging perlu diamati apakah terjadi juga.


### S5: gangguan, satu sekolah 500 peserta: **lulus**

Dua replika (pool 20) di belakang load balancer. 10% request klien gagal acak (3.012 request; separuh tidak terkirim, separuh jawabannya hilang di jalan). Di tengah fase mengerjakan, replika A di-restart (SIGTERM, hidup lagi 6 detik kemudian) dan replika B dimatikan paksa (SIGKILL, hidup lagi 20 detik kemudian).

| Endpoint | p50 | p95 |
|---|---:|---:|
| login | 32 ms | 102 ms |
| buka ujian | 300 ms | 907 ms |
| simpan (13.418) | 6 ms | 10 ms |
| kumpul | 21 ms | 30 ms |

- **Jawaban hilang: 0** dari 15.219; tidak ada antrian yang tersisa di klien.
- Error server 6 dari 29.768 (0,02%), semuanya pengalihan gambar saat replika B mati.
- Semua peserta pulih otomatis: tidak ada yang gagal login, mulai, atau kumpul. 479 kumpul sendiri; 21 ditutup job kumpul otomatis.


## Batasan

- **Satu mesin untuk semuanya.** Postgres, replika, load balancer, dan driver beban berbagi 8 inti dan RAM 7 GB. Latensi di atas ±1.000 peserta lebih mencerminkan mesin uji daripada aplikasi.
- **Mesin tertidur** dua kali di tengah uji (11 dan ±10 menit). Selama itu semua proses berhenti; job kumpul otomatis tertahan lalu pulih begitu mesin aktif. Angka latensi di sekitar jeda itu tidak dipakai.
- **Gambar dari aplikasi.** Di uji lokal, gambar dikirim aplikasi (penyimpanan lokal). Di produksi, aplikasi hanya mengalihkan ke URL R2 bertanda tangan, jadi beban gambar lebih ringan dari yang diukur.
- **Masa mengerjakan dipersingkat** dari 90 menit menjadi 5–8 menit; laju simpan per detik sama.
- **CPU per replika diukur dari `/proc` proses server**, bukan CPU container Railway.


## Rencana kapasitas

### Biaya beban yang terukur

| Beban | CPU aplikasi | Catatan |
|---|---|---|
| 500 peserta mengerjakan (±25 simpan/dtk + 15 pengawas) | rata-rata 0,09 inti | simpan p95 7 ms |
| Lonjakan login 500 peserta dalam 2 menit | puncak 0,65 inti | argon2 + render halaman |
| 2.000 peserta, dua replika | rata-rata 0,7–0,8 inti total, puncak 3,3 inti | puncak saat login |
| Koneksi database | sama dengan ukuran pool per replika | 20–22 untuk satu replika pool 20 |

Simpan jawaban sangat murah. Yang mahal adalah **lonjakan awal**: login (argon2), halaman ujian, dan pengalihan gambar, semuanya dalam 2 menit setelah token dibacakan. Bila naik linear dari angka di atas, 5.000 peserta login dalam 2 menit butuh **±7–8 inti aplikasi** selama lonjakan dan ±1 inti saat mengerjakan. Angka ini perkiraan dari uji lokal dan harus dibuktikan di staging.

### Konfigurasi Railway yang disarankan

| Kondisi | Aplikasi | Postgres | Catatan |
|---|---|---|---|
| Hari biasa, ≤500 peserta bersamaan | 1 replika, `DB_MAX_CONNS=20` | paket kecil (1 vCPU, 1 GB) | sesuai uji S1–S3 |
| Ujian satu sekolah besar / beberapa sekolah, ≤2.000 | 2 replika, pool 20–32 | 2 vCPU, 2 GB | dua replika juga melindungi dari replika mati (S5) |
| **Musim PAS, ±5.000 dengan jam mulai sama** | **4 replika**, pool 20 (total 80) | 2–4 vCPU, 4 GB, `max_connections` ≥ 100 | perlu dibuktikan lewat [uji S4 di staging](#uji-s4-di-staging) |

Aturan pool: **jumlah replika × `DB_MAX_CONNS` + ±10** (migrasi, job, psql) harus ≤ `max_connections` Postgres. Dengan batas 100: 4 replika × 20 = 80.

Cara murah mengurangi lonjakan: jam mulai sekolah dibuat berselang 5–10 menit. Lonjakan login lalu tersebar, dan replika yang sama bisa melayani lebih banyak peserta.

### Kapan menambah replika

- `/internal/` atau alert menunjukkan **p95 simpan > 300 ms** atau **antre kumpul otomatis** (T145): tambah satu replika.
- **CPU replika > 70%** saat ujian (metrik Railway): tambah satu replika sebelum sesi berikutnya.
- **Koneksi pool habis** tetapi Postgres santai: naikkan `DB_MAX_CONNS`, dengan tetap menjaga aturan pool di atas.
- **Postgres CPU > 70%** atau koneksi mendekati batas: naikkan paket Postgres. Jangan menambah replika dulu, karena itu hanya menambah koneksi.

### Biaya

Railway menagih pemakaian per detik: $20 per vCPU per bulan, $10 per GB RAM per bulan, egress $0,05/GB, dan paket Pro $20/bulan sudah termasuk $20 pemakaian ([harga Railway](https://docs.railway.com/pricing/plans), dicek November 2026). Yang ditagih adalah pemakaian nyata, bukan batas yang diatur.

| Kondisi | Perkiraan pemakaian | Perkiraan biaya |
|---|---|---|
| Bulan biasa | aplikasi rata-rata 0,2 vCPU + 0,5 GB; Postgres 0,2 vCPU + 1 GB | ±$4 + $5 + $4 + $10 ≈ **$25/bulan** (sebagian tertutup $20 paket Pro) |
| Tambahan seminggu musim PAS | 3 replika tambahan × ±0,5 GB selama 7 hari; CPU ±45 vCPU-jam selama sesi | memori ±$3,5 + CPU ±$1,3 ≈ **< $10** |
| Gambar soal | dari R2, bukan Railway | egress R2 gratis; penyimpanan R2 terpisah |

Biaya terbesar justru memori replika yang menyala, bukan CPU. Karena itu replika tambahan cukup dinyalakan H-1 dan dimatikan setelah musim selesai. Angka di atas perkiraan; pastikan lewat dasbor pemakaian Railway setelah uji S4 di staging.

### Prosedur menjelang musim PAS

1. **H-14:** jalankan [uji S4 di staging](#uji-s4-di-staging) dengan konfigurasi rencana. Catat hasilnya di dokumen ini.
2. **H-7:** pastikan jam mulai sekolah sudah diketahui; minta jeda 5–10 menit antar-sekolah bila memungkinkan.
3. **H-1:**
   - naikkan replika (Railway → Settings → Replicas) sesuai tabel;
   - periksa aturan pool dan `max_connections`;
   - pastikan alert (T145) aktif dan penjaga deploy berjalan.
4. **Hari H:** pantau `/internal/` dan metrik Railway mengikuti [runbook ujian](runbook-ujian.md). Bila p95 simpan naik, tambah replika; aplikasi siap banyak replika.
5. **Setelah musim:** turunkan replika ke konfigurasi hari biasa.

### Uji S4 di staging

Belum dijalankan karena memakai sumber daya Railway berbayar dan butuh persetujuan. Langkahnya:

1. **Siapkan lingkungan uji beban:** layanan aplikasi dan Postgres terpisah dari pilot (bisa project/environment baru), `APP_ENV=staging`, penyimpanan R2 bucket uji. **Jangan pernah memakai database produksi atau pilot.**
2. **Konfigurasi:** sesuai baris "Musim PAS" di tabel di atas. Catat vCPU/RAM per replika, `DB_MAX_CONNS`, dan paket Postgres.
3. **Data:** jalankan `seed -schools 10` dari mesin yang bisa menjangkau database uji, dengan `STORAGE_DRIVER=r2` dan kredensial bucket uji (`R2_ACCOUNT_ID`, `R2_BUCKET`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`) agar gambar soal terunggah ke R2 seperti di produksi.
4. **Generator beban:** dari ≥2 mesin di luar Railway (mis. VPS di Jakarta/Singapura). Bagi sekolah per mesin (`-from`, `-schools`) agar setiap IP ≤ 3.000 login per 15 menit.
5. **Pengamatan:** metrik CPU/RAM Railway per replika dan Postgres, koneksi Postgres, `/internal/`.
6. **Perkiraan biaya satu putaran** (±1 jam): ±10 vCPU-jam + ±10 GB-jam ≈ **< $1**, ditambah VPS generator.


## Pilot dan rollout (T155)

Status: **belum dimulai.** Alat ukur dan prosedur sudah siap. Pilot dijalankan bersama sekolah dan diisi di bagian ini setelah setiap rilis.

### Rencana pilot per rilis

| Rilis | Sekolah pilot | Skenario | Mulai | Status |
|---|---|---|---|---|
| R1 — tes seleksi online | 1–2 sekolah | Satu sesi tes di sekolah (lab/HP) dan satu sesi dari rumah | | belum |
| R2 — ulangan guru | 3–5 guru dari sekolah pilot | Ulangan harian dengan HP siswa; minimal satu ulangan diimpor dari Word | | belum |
| R3 — PTS/PAS sekolah | 1 sekolah | PTS/PAS penuh (lab dan HP), koordinator memakai agenda dan pusat kendali | | belum |

Aktivasi per sekolah pilot (dicatat di audit sebagai `module.exam`):

```sh
akra-modul -school <kode> -exam on -release r1
```

### Pendampingan setiap sekolah pilot

| Kapan | Kegiatan | Alat |
|---|---|---|
| H-7 | Siswa cek perangkat; perangkat yang gagal diganti atau dipinjami | `/u/cek`, panduan peserta (Ujian → Bantuan) |
| H-7 | Isi kontak dukungan dan jam layanan di server | `SUPPORT_WHATSAPP`, `SUPPORT_EMAIL`, `SUPPORT_HOURS`, `SUPPORT_HOURS_PEAK` |
| H-3 | Cetak kartu dengan **panduan peserta di balik kartu**; ujian latihan dibuka untuk kelas peserta | Detail ujian/agenda → Dokumen; Ujian → Ujian latihan |
| H-2 | Gladi pengawas di sesi latihan: layar pengawas, arti warna, pindah perangkat, reset login, jeda, berita acara | Panduan pengawas (PDF), [doc/panduan-ujian/uji-keterbacaan.md](panduan-ujian/uji-keterbacaan.md) |
| Hari H | Tim AKRA hadir (langsung atau daring) di ujian pertama; tim siaga memantau alert | `/internal/`, [runbook-ujian.md](runbook-ujian.md) |
| H+1 sampai H+3 | Laporan ukuran, perbandingan nilai, wawancara singkat | `akra-ops pilot` (di bawah) |

### Data yang dikumpulkan

**Ukuran dari AKRA**, dihitung `akra-ops pilot`. Perintah ini hanya membaca, dan keluarannya Markdown siap tempel ke bagian Hasil:

```sh
DATABASE_URL=... APP_BASE_URL=https://<host> akra-ops pilot -school <kode> -from 2027-02-01 -to 2027-02-28 -tickets <jumlah tiket> -sample 10
```

| Ukuran (indeks bagian 9) | Cara dihitung |
|---|---|
| 0 jawaban hilang | Soal yang perubahannya dikirim perangkat setelah attempt ditutup dan belum pernah tersimpan (`exam_lost_answers`) |
| ≥ 95% attempt tanpa bantuan pengawas | Attempt **tanpa** pindah perangkat, reset login, buka kunci, tambah waktu peserta, atau dikumpulkan pengawas |
| Kelas mulai ≤ 5 menit sejak token | Per sesi (≥ 5 peserta): menit sejak peserta pertama mulai sampai 90% peserta mulai |
| Ulangan 20 soal ≤ 15 menit (≤ 5 dari Word) | Per ujian: menit sejak soal atau impor pertama guru (≤ 3 jam sebelum ujian dibuat) sampai dijadwalkan |
| Koreksi 30 esai ≤ 20 menit | Per korektor per ujian: rata-rata jeda antar-esai (jeda > 10 menit diabaikan) × 30 |
| ≥ 70% guru membuat ujian kedua | Guru dengan ≥ 2 ujian di rentang. "Tanpa didampingi" dicek dari catatan pendamping |
| Tiket ≤ 1 per 100 peserta | Jumlah tiket dari catatan tim (`-tickets`) dibagi peserta |
| Insiden | Kejadian platform (`ops_incidents`) di rentang; yang masih terbuka |

**Dari tim:**
- tiket bantuan (WhatsApp/email/telepon) dengan kategori;
- kejadian di berita acara;
- wawancara singkat 5–10 menit dengan guru, pengawas, koordinator, dan 5 siswa:
  1. Bagian mana yang paling membingungkan?
  2. Kapan Anda perlu bertanya atau dibantu?
  3. Apa yang lebih lambat daripada cara lama?
  4. Mau memakai lagi di ulangan/ujian berikutnya? Mengapa?

**Perbandingan nilai:** `-sample 10` memilih 10 lembar acak bernilai final. Guru mengoreksi manual dari halaman hasil peserta tanpa melihat nilai AKRA, lalu mengisi kolom "Nilai manual". Setiap selisih ditelusuri: kunci salah, aturan penilaian (mis. PG kompleks proporsional), atau bug.

### Kriteria lanjut ke rollout

| Kriteria | R1 | R2 | R3 |
|---|---|---|---|
| 0 jawaban hilang | | | |
| Tidak ada insiden kategori tinggi yang terbuka | | | |
| ≥ 95% attempt tanpa bantuan | | | |
| 10 lembar sampel cocok dengan koreksi manual | | | |
| Guru pilot mau memakai lagi | | | |

Bila satu kriteria belum terpenuhi, rollout rilis itu ditunda. Masalahnya dicatat sebagai task di `doc/tasks_schoolos_mvp4/21-perbaikan/`, diperbaiki, lalu pilot diulang untuk bagian yang gagal.

### Rollout bertahap

| Gelombang | Sekolah | Syarat masuk | Perintah |
|---|---|---|---|
| 1 | 5 sekolah | Kriteria lanjut rilis terpenuhi | `akra-modul -file gelombang-1.txt -exam on -release <r>` |
| 2 | 20 sekolah | Gelombang 1 berjalan 2 minggu tanpa insiden tinggi; tiket ≤ 1 per 100 peserta | `akra-modul -file gelombang-2.txt -exam on -release <r>` |
| 3 | Semua sekolah | Gelombang 2 lulus ukuran yang sama | per berkas daftar sekolah |

- Satu berkas gelombang diterapkan dalam satu transaksi: bila satu kode salah, tidak ada yang berubah. Status semua sekolah dicek dengan `akra-modul -list`.
- Setiap gelombang: pengumuman ke admin TU, webinar singkat 30 menit (isi [doc/panduan-ujian/](panduan-ujian/README.md)), dan tautan panduan cetak.
- **Musim PAS pertama** dipantau khusus:
  - `SUPPORT_HOURS_PEAK` diisi; jam layanan diperluas tampil otomatis di halaman bantuan sekolah yang punya agenda;
  - tim siaga mengikuti [runbook-ujian.md](runbook-ujian.md) dan jadwal musim ujian di `/internal/`;
  - kapasitas mengikuti bagian "Prosedur menjelang musim PAS" di atas, termasuk uji S4 di staging lebih dulu.

### Hasil pilot

_Tempel keluaran `akra-ops pilot` per sekolah di sini, lalu isi tiket, wawancara, perbandingan nilai, dan keputusan lanjut/tunda._
