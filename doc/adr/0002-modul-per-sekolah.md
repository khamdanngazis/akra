# ADR 0002 — Modul dan paket per sekolah

- **Status:** diterima 2026-11-22, diterapkan 2026-11-28 (T163–T169). Keputusan pemilik produk ada di bagian 8, keputusan final implementasi di bagian 9
- **Tanggal:** 2026-11-22
- **Terkait:** [analisis menu](../analisis-menu.md), [ADR 0001](0001-modul-ujian.md) (aktivasi modul ujian T125), task di `tasks_schoolos_mvp4/22-modul-sekolah/`

## 1. Masalah

Sekolah berbeda kebutuhan. Contohnya:
- sekolah negeri tidak menagih SPP;
- sekolah kecil tidak memakai ujian online;
- sebagian sekolah hanya butuh keuangan dan portal orang tua.

AKRA saat ini menyalakan semua fitur untuk semua sekolah, kecuali modul ujian. Kita ingin setiap sekolah memakai **paket modul** yang dipilih sesuai kebutuhan. Modul yang tidak dipakai tidak tampil dan tidak bisa dibuka, tanpa merusak modul lain.

## 2. Kondisi sekarang

| Bagian | Keadaan | Lokasi |
|---|---|---|
| Penyimpanan | Kunci pengaturan `<modul>.enabled = true` di `school_settings`. Hanya `exam.enabled` (dan `exam.release`) yang dipakai | `internal/platform/settings`, `cmd/modul` |
| Daftar modul di sesi | Dihitung **saat login** dari semua kunci `%.enabled`, lalu disimpan di sesi (`auth.Identity.Modules`). Perubahan baru terlihat di menu **setelah login ulang** | `internal/auth/repo.go`, `session.go` |
| Menu | `nav.MenuItem.Module` sudah ada; hanya 5 item ujian yang memakainya | `internal/platform/nav/menu.go` |
| Route | Hanya modul ujian yang diperiksa per request (`exam.RequireModule`, 404 bila nonaktif). Route modul lain selalu terbuka | `internal/exam/module.go` |
| Beranda, portal, laporan | Portal dan beranda memeriksa modul ujian secara khusus. Bagian keuangan, absensi, nilai, dan jadwal selalu tampil | `internal/home`, `internal/portal`, `internal/reports` |
| Pengelola | CLI `akra-modul` (modul ujian saja, per sekolah atau gelombang) | `cmd/modul` |

Kesimpulan: **fondasi sudah ada** (kunci per sekolah, field menu, middleware), tetapi baru dipakai satu modul. Daftar modul di sesi juga bisa basi.

## 3. Katalog modul

Batas modul diambil dari paket Go dan ketergantungan nyata antarpaket (`go list`), bukan dari menu.

| ID | Nama | Isi | Route (± jumlah di registry) | Wajib |
|---|---|---|---|---|
| `core` | Inti | Profil sekolah, tahun ajaran, kelas, siswa & wali, mapel, penugasan guru, tugas tambahan, pengguna & peran, impor siswa, audit umum, hub laporan, pengaturan | `/siswa`, `/kelas`, `/pengaturan/*`, `/impor/siswa`, `/laporan` (hub) | ✓ |
| `attendance` | Absensi | Absensi harian/per mapel, rekap, laporan kehadiran | `/absensi/*`, `/laporan/kehadiran*` (±8) | |
| `schedule` | Jadwal | Jadwal pelajaran, jadwal saya | `/jadwal/*` (6) | |
| `grades` | Nilai & Rapor | Komponen, input, rekap, rapor PDF, pengaturan rapor, impor nilai | `/nilai/*`, `/pengaturan/rapor`, `/impor/nilai` (±10) | |
| `billing` | Keuangan | Jenis tagihan, tagihan massal, pembayaran, kuitansi, tunggakan, pembatalan, audit keuangan, pengaturan | `/keuangan/*`, `/pengaturan/keuangan`, `/laporan/tunggakan*` (±30) | |
| `admission` | Pendaftaran | Periode, gelombang, jalur, formulir publik, pendaftar, tes, daftar ulang, jadikan siswa | `/pendaftaran/*`, `/s/{kode}/*` (±70) | |
| `dapodik` | Bantu Dapodik | Lembar salin, cek kesiapan, F-PD, impor/ekspor, nilai ke Dapodik | `/dapodik/*` (14) | |
| `exam` | Ujian Online | Bank soal sampai hasil (tahap rilis r1–r3) | `/ujian/*`, `/u/*` (±170) | |
| `announcements` | Pengumuman | Pengumuman staf dan orang tua | `/pengumuman`, `/portal/pengumuman` | |
| `portal` | Portal Orang Tua | Akun wali, aktivasi, halaman anak | `/portal/*`, `/aktivasi` | |

**Bukan modul:** Laporan adalah hub lintas modul yang menampilkan laporan dari modul aktif. Beranda adalah ringkasan lintas modul.

### Fitur di dalam modul
Pilihan fitur yang sudah ada tetap berupa pengaturan sekolah di dalam modulnya, dan hanya berlaku bila modulnya aktif:

| Modul | Fitur | Kunci sekarang |
|---|---|---|
| attendance | Absensi per mapel | `attendance_per_subject` |
| grades | Skala, KKM, format rapor, kebijakan remedial, nilai per komponen di portal | `report.*` |
| billing | Ambang persetujuan pembatalan | `void_approval_threshold` |
| admission | Format NIS, daftar ulang | `admission.*` |
| exam | Tahap rilis r1–r3, telaah soal, target kisi-kisi | `exam.release`, pengaturan ujian |

## 4. Ketergantungan antarmodul

Dari graf impor paket dan fungsinya:

| Modul | Wajib | Opsional (fitur hilang bila modul itu mati) |
|---|---|---|
| attendance | core | – |
| schedule | core | – |
| grades | core | attendance: rekap kehadiran di rapor |
| billing | core | – |
| admission | core | billing: biaya formulir dan kuitansi (`payments`); exam: tes seleksi online (r1) |
| dapodik | core | grades: nilai ke Dapodik |
| exam | core (mapel, penugasan, tugas) | grades: kirim nilai ke rapor; admission: tes seleksi; attendance: tidak ada |
| announcements | core | – |
| portal | core | Tab per modul: absensi, jadwal, nilai/rapor, tagihan/kuitansi, ujian, pengumuman |

**Aturan:**
- Ketergantungan **wajib** dicek saat modul diaktifkan.
- Ketergantungan **opsional** tidak menghalangi. Fitur yang bergantung disembunyikan, dengan keterangan "aktif bila modul X dipakai".

## 5. Paket

Paket adalah **templat** kumpulan modul, bukan batas teknis. Konfigurasi sekolah tetap berupa daftar modul, sehingga paket kustom bisa dibuat. Usulan awal (nama dan harga diputuskan pemilik produk):

| Paket | Modul | Untuk |
|---|---|---|
| Dasar | core, attendance, schedule, announcements, portal | Sekolah yang mulai digital: data siswa, absensi, komunikasi |
| Akademik | Dasar + grades, dapodik | Operator yang mengurus rapor dan Dapodik |
| Keuangan | core, billing, announcements, portal | Yayasan yang hanya perlu SPP dan tagihan |
| Lengkap | semua modul, termasuk admission dan exam | Sekolah swasta dengan SPMB dan ujian online |

## 6. Rancangan

### Data
- **Tabel `school_modules`:**

  | Kolom | Isi |
  |---|---|
  | `school_id`, `module` | kunci |
  | `enabled` | aktif atau tidak |
  | `options jsonb` | mis. `{"release": "r2"}` untuk ujian |
  | `changed_at`, `changed_by`, `note` | jejak perubahan |

  Kolom `schools.package` hanya label paket untuk tampilan dan laporan.
- **Migrasi:**
  - `exam.enabled` dan `exam.release` dipindah ke `school_modules`;
  - semua sekolah yang sudah ada mendapat **semua modul non-ujian aktif**, supaya tidak ada yang berubah saat rilis;
  - kunci lama dihapus setelah kode berhenti membacanya.
- **Katalog modul ada di kode** (`internal/platform/module`): ID, nama, ketergantungan, fitur, dan prefix route. Kode tidak bisa merujuk modul yang tidak ada.

### Membaca status per request
- Middleware memuat **set modul aktif sekolah sekali per request**, dengan cache di memori per replika selama 30 detik, lalu menaruhnya di context.
- Menu, beranda, portal, dan halaman membaca dari context. Sesi tidak lagi menyimpan daftar modul, sehingga perubahan berlaku tanpa login ulang. Paling lama 30 detik per replika.

### Penegakan (semua titik)

| Titik | Perilaku bila modul nonaktif |
|---|---|
| Route staf | Halaman "Modul ini tidak aktif untuk sekolah Anda" (status 404), tanpa membocorkan data |
| Route publik (`/s/...`, `/u/...`) | 404 seperti sekarang |
| Menu samping, bottom nav, pintasan beranda | Item hilang; kelompok kosong hilang |
| Beranda | Kartu dan angka modul itu hilang (keuangan, absensi, ujian, pengumuman, kesiapan Dapodik) |
| Portal orang tua | Tab dan ringkasan modul itu hilang; tagihan dan kuitansi lama tidak tampil |
| Laporan | Laporan dan sumber pembuat laporan modul itu hilang; konfigurasi laporan tersimpan dikunci |
| Impor | Target "nilai" hilang bila grades mati |
| Pencarian header | Bendahara: pencarian pembayaran hanya bila billing aktif |
| Fitur lintas modul | Sesuai tabel bagian 4 |
| Peran | Peran **bendahara** hanya bisa diberikan bila billing atau admission aktif. Tugas **koordinator ujian** hanya bila exam aktif. Pengguna yang sudah ada tidak dihapus |
| Job latar belakang | Job berkala tetap jalan untuk data yang ada (mis. masa simpan); job yang dipicu pengguna tidak bisa dipicu karena route-nya tertutup |
| Data | **Tidak dihapus.** Data tetap utuh dan muncul lagi saat modul diaktifkan kembali. Masa simpan tetap berlaku |

### Menonaktifkan modul
- **Dicegah** bila sedang dipakai:
  - exam: sesi berlangsung atau terjadwal dalam 24 jam;
  - admission: periode pendaftaran sedang dibuka.
- **Hanya peringatan** untuk keuangan: tagihan belum lunas tetap tersimpan, tetapi tidak terlihat orang tua.
- Setiap perubahan tercatat di audit (`module.<id>`) dengan siapa, kapan, dan catatan.

### Siapa yang mengatur
- **Tim implementator AKRA** mengatur modul dan paket setiap sekolah (keputusan 1).
- **Untuk sekarang tanpa UI pengelola:** perubahan lewat CLI `akra-modul`. Layanannya dirancang agar bisa dipakai UI implementator nanti tanpa diubah.
- **Admin sekolah** melihat halaman **Pengaturan → Modul & paket** (baca saja): modul aktif dan isinya, dengan keterangan untuk menghubungi tim implementator AKRA bila butuh modul lain.
- Tombol permintaan dan dasbor implementator menyusul bersama UI implementator.

## 7. Pengujian
- **Registry route:** setiap entri punya `Module`. Uji memastikan setiap route modul memberi 404 / "modul tidak aktif" bila modulnya mati, dan route modul lain tetap 200.
- **Kombinasi:** satu sekolah "inti saja", satu per paket, dan "semua kecuali X" untuk setiap modul. Diuji untuk menu, beranda, portal, dan laporan (tanpa error dan tanpa tautan ke modul mati).
- **Uji lintas sekolah (T58)** dan **uji orang tua (T59)** tetap lulus. Keduanya berjalan dengan konfigurasi bawaan; untuk setiap kombinasi, yang diuji adalah tanpa 5xx, tanpa tautan ke modul mati, dan penegakan route per modul (lihat bagian 9).

## 8. Keputusan (2026-11-22)

| # | Pertanyaan | Keputusan |
|---|---|---|
| 1 | Siapa yang menyalakan/mematikan modul? | **Tim implementator AKRA.** UI implementator belum dibuat; sementara lewat CLI `akra-modul`. Sekolah hanya melihat modulnya |
| 2 | Isi dan nama paket | **Empat paket di bagian 5:** Dasar, Akademik, Keuangan, Lengkap |
| 3 | Sekolah yang sudah ada saat rilis | **Semua modul non-ujian aktif**; ujian tetap per sekolah seperti sekarang |
| 4 | Biaya formulir pendaftaran tanpa modul keuangan | **Tetap bisa.** Kuitansi dan penomoran dari `payments` dipakai bersama; dicatat admin TU bila tidak ada bendahara |
| 5 | Inti cukup atau perlu dipecah (mis. pengguna/peran, mapel) | Cukup; mapel dan penugasan dibutuhkan absensi per mapel, nilai, jadwal, dan ujian (usulan, tidak dipersoalkan) |

## 9. Keputusan final implementasi (T163–T169, 2026-11-28)

| Bagian | Keputusan |
|---|---|
| Penyimpanan | Tabel `school_modules` (migrasi 089) dengan `options`, `changed_at`, `changed_by` (teks: pelaku tim implementator), `note`; `schools.package` label paket. Modul tanpa baris memakai bawaan katalog: semua aktif kecuali ujian. Kunci lama `exam.enabled`/`exam.release` tidak dibaca lagi dan dihapus di migrasi rilis berikutnya |
| Katalog | `internal/platform/module`: 10 modul, ketergantungan wajib (saat ini hanya inti), ketergantungan opsional, pola route per modul, fitur, dan pilihan yang divalidasi (`exam.release`) |
| Status per request | `module.Middleware` di grup login memuat set modul sekali per request (cache 30 detik per replika); sesi tidak menyimpan modul. Template memakai `modules`/`hasModule`. Halaman publik membaca per sekolah (`module.Load`) |
| Penegakan route | Satu penjaga terpusat (`module.Enforce`) dari pola route katalog, bukan middleware per paket. Satu path bisa butuh beberapa modul (mis. tab tagihan portal: portal + keuangan). Fitur lintas modul yang ada di route modul lain (kirim ke rapor, nilai ke Dapodik) ikut dipetakan ke modul nilai. Registry route mencatat `Module` setiap route dan diuji sama dengan katalog |
| Halaman staf modul mati | 404 "Modul tidak aktif" dengan nama modul, dirender sebelum handler; JSON dan unduhan 404 biasa |
| Lintas modul | Sesuai bagian 4 dan T166. Tes online lama tetap tertaut saat modul ujian dimatikan (nilai tidak hilang); tes baru hanya di kertas. Hub Laporan dan Pengaturan yang kosong untuk pengguna disembunyikan |
| Pengelola | `module.Admin` satu-satunya jalan mengubah modul: catatan wajib, ketergantungan, penjaga (ujian: sesi dalam 24 jam; pendaftaran: periode dibuka; keuangan: tagihan belum lunas, bisa `force`), audit `module.<kode>` dan `module.package`. CLI `akra-modul` memakainya; UI implementator menyusul dengan layanan yang sama |
| Paket | `module.Packages`: Dasar, Akademik, Keuangan, Lengkap. Sekolah baru lewat `akra-seed` mendapat paket Dasar. Sekolah demo (`akra-demoseed`) tanpa paket kecuali diminta, agar demo tetap lengkap |
| Sekolah | Pengaturan → Modul & Paket (admin TU, kepala sekolah), baca saja |
| Uji | Penegakan per modul (`TestModuleGate`), pemindai tautan halaman lintas modul, fitur lintas modul, dan kombinasi: inti saja, setiap paket, dan semua kecuali X (`TestModuleCombinations`, 14 kombinasi × ±480 halaman, tanpa 5xx dan tanpa tautan ke modul mati). Harness visual `scripts/visual.sh --modules` memotret menu dan beranda paket Dasar dan Keuangan |
| Panduan | [doc/implementator-modul.md](../implementator-modul.md) untuk tim implementator; runbook ujian bagian 7 untuk "modul tidak aktif" |
