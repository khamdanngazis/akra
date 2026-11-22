# Analisis Menu AKRA

- **Tanggal:** 2026-11-22
- **Cakupan:** seluruh menu staf dan orang tua (bukan hanya modul ujian), semua peran, semua halaman yang terdaftar di registry route (`internal/testutil/routes.go`).
- **Terkait:** [ADR 0002 — Modul per sekolah](adr/0002-modul-per-sekolah.md), task [T156](tasks_schoolos_mvp4/21-rapikan/T156-menu-seluruh-aplikasi.md).

## 1. Kondisi sekarang

Menu didefinisikan di `internal/platform/nav/menu.go` sebagai satu daftar datar `Items` (36 item). Setiap pengguna melihat item yang cocok dengan peran, tugas tambahan, dan modul aktif sekolahnya, dalam urutan daftar itu.

### Jumlah item per peran

| Peran | Item | Catatan |
|---|---:|---|
| Admin TU | 24 | + Agenda dan Pengaturan Ujian bila modul ujian aktif |
| Kepala sekolah | 20 | hampir semua halaman, termasuk halaman operasional keuangan |
| Guru | 11 | + Agenda dan Pengaturan Ujian bila koordinator |
| Bendahara | 10 | |
| Orang tua | 2 | Anak Saya, Pengumuman |

### Matriks menu × peran (urutan sekarang)

| # | Menu | Path | Admin TU | Kepala | Guru | Bendahara | Ortu |
|---|---|---|:-:|:-:|:-:|:-:|:-:|
| 1 | Beranda | `/` | ✓ | ✓ | ✓ | ✓ | |
| 2 | Anak Saya | `/portal` | | | | | ✓ |
| 3 | Absensi | `/absensi` | | | ✓ | | |
| 4 | Rekap Absensi | `/absensi/rekap` | ✓ | ✓ | | | |
| 5 | Siswa | `/siswa` | ✓ | ✓ | | | |
| 6 | Pendaftaran | `/pendaftaran` | ✓ | ✓ | | | |
| 7 | Dapodik | `/dapodik` | ✓ | ✓ | | | |
| 8 | Kelas | `/kelas` | ✓ | ✓ | | | |
| 9 | Jadwal | `/jadwal` | ✓ | ✓ | | | |
| 10 | Jadwal Saya | `/jadwal/saya` | | | ✓ | | |
| 11 | Pembayaran | `/keuangan/pembayaran/baru` | | | | ✓ | |
| 12 | Tagihan Massal | `/keuangan/tagihan-massal` | | | | ✓ | |
| 13 | Biaya Pendaftaran | `/pendaftaran/biaya` | | | | ✓ | |
| 14 | Rekap Pembayaran | `/keuangan/rekap-pembayaran` | | ✓ | | ✓ | |
| 15 | Tunggakan | `/keuangan/tunggakan` | ✓ | ✓ | | ✓ | |
| 16 | Jenis Tagihan | `/keuangan/jenis-tagihan` | ✓ | ✓ | | ✓ | |
| 17 | Pembatalan | `/keuangan/pembatalan` | | ✓ | | | |
| 18 | Laporan | `/laporan` | ✓ | ✓ | | ✓ | |
| 19 | Pengumuman | `/pengumuman` | ✓ | ✓ | ✓ | ✓ | |
| 20 | Pengumuman | `/portal/pengumuman` | | | | | ✓ |
| 21 | Audit | `/keuangan/audit` | ✓ | ✓ | | | |
| 22 | Pengaturan Keuangan | `/pengaturan/keuangan` | | ✓ | | ✓ | |
| 23 | Profil Sekolah | `/pengaturan/sekolah` | ✓ | ✓ | | | |
| 24 | Tahun Ajaran | `/pengaturan/tahun-ajaran` | ✓ | ✓ | | | |
| 25 | Penugasan Guru | `/pengaturan/penugasan` | ✓ | | ✓ | | |
| 26 | Input Nilai | `/nilai/input` | | | ✓ | | |
| 27 | Rekap Nilai | `/nilai/rekap` | ✓ | ✓ | ✓ | | |
| 28 | Komponen Nilai | `/nilai/komponen` | ✓ | | ✓ | | |
| 29 | Nilai ke Dapodik | `/dapodik/nilai` | | | ✓ | | |
| 30 | Ujian | `/ujian` | ✓ | ✓ | ✓ | | |
| 31 | Agenda Ujian | `/ujian/agenda` | ✓ | ✓ | koord. | | |
| 32 | Telaah Soal | `/ujian/telaah` | ✓ | | ✓ | | |
| 33 | Pengaturan Ujian | `/ujian/pengaturan` | ✓ | | koord. | | |
| 34 | Tugas Tambahan | `/pengaturan/tugas` | ✓ | | | | |
| 35 | Pengaturan Rapor | `/pengaturan/rapor` | ✓ | | | | |
| 36 | Mata Pelajaran | `/pengaturan/mapel` | ✓ | | | | |

## 2. Masalah

### Struktur
1. **Datar dan panjang.** Admin TU menggulir 24 item tanpa pengelompokan. Kepala sekolah 20.
2. **Urutan mencampur area.**
   - Dapodik terselip di antara Pendaftaran dan Kelas.
   - Menu nilai (26–28) ada di bawah pengaturan, jauh dari Absensi dan Jadwal.
   - Ujian ada setelah Nilai ke Dapodik.
3. **Pengaturan tercecer di 8 tempat dengan 3 pola URL:**
   - menu: Profil Sekolah dan Tahun Ajaran di tengah; Pengaturan Keuangan di tengah; Tugas Tambahan, Pengaturan Rapor, dan Mata Pelajaran di bawah;
   - di dalam halaman lain: `/pendaftaran/pengaturan` dan `/ujian/pengaturan`;
   - absensi per mapel ada di halaman **Mata Pelajaran**, yang tidak berkaitan dengan absensi.
4. **Tidak ada titik masuk untuk beberapa halaman:**
   - **Impor nilai** (`/impor/nilai`) dan **pemetaan kolom impor** (`/impor/mapping`) hanya bisa dicapai dari halaman impor siswa.
   - Halaman impor siswa sendiri hanya bisa dicapai dari dalam Siswa (`Also`).
5. **Modul hanya dikenal untuk ujian.** Lima item ujian punya `Module: "exam"`; 31 item lain selalu tampil, walau sekolah tidak memakai keuangan, pendaftaran, atau Dapodik. Lihat ADR 0002.

### Penamaan
6. **Label tidak menyebut objek:**
   - **Audit** sebenarnya hanya audit keuangan (keterangannya "Jejak setiap aksi keuangan").
   - **Pembayaran** langsung membuka formulir catat pembayaran.
   - **Penugasan Guru** untuk guru berjudul "Penugasan Saya".
7. **Label kembar:** Pengumuman staf dan Pengumuman orang tua (beda peran, jadi tidak bertabrakan). Jadwal vs Jadwal Saya mengikuti pola yang sama dan perlu dipertahankan konsisten.

### HP dan beranda
8. **Bottom navigation HP mengambil 4 item pertama sesuai urutan daftar** (`nav.Bottom`), bukan yang paling sering dipakai:

   | Peran | Sekarang | Masalah |
   |---|---|---|
   | Admin TU | Beranda, Rekap Absensi, Siswa, Pendaftaran | Pendaftaran musiman; Ujian dan Tunggakan tidak ada |
   | Kepala | Beranda, Rekap Absensi, Siswa, Pendaftaran | Laporan tidak ada |
   | Guru | Beranda, Absensi, Jadwal Saya, Pengumuman | **Input Nilai dan Ujian tidak ada** |
   | Bendahara | Beranda, Pembayaran, Tagihan Massal, Biaya Pendaftaran | Tunggakan tidak ada; Biaya Pendaftaran musiman |

9. **Kartu pintasan beranda** (`nav.Shortcuts`) juga mengambil 6 item pertama berketerangan. Akibatnya, mengubah urutan menu mengubah beranda semua peran.

## 3. Usulan struktur

### Prinsip
1. **Menu dikelompokkan per area kerja.** Satu kelompok bisa berisi item dari beberapa modul (mis. Akademik berisi absensi, jadwal, nilai). Setiap item tetap membawa `Module`-nya sendiri, sehingga item modul yang tidak aktif hilang dan kelompok kosong tidak tampil (ADR 0002).
2. **Satu pintu pengaturan.** Semua pengaturan dipindah ke satu item **Pengaturan** (`/pengaturan`): halaman pusat berisi bagian per modul aktif. Ini mengurangi 6–8 item di menu admin TU dan kepala sekolah.
3. **Label menyebut objek atau tugasnya:** Audit → **Audit Keuangan**, Pembayaran → **Catat Pembayaran**, Penugasan Guru (untuk guru) → **Mapel Saya**.
4. **Bottom navigation dan pintasan beranda dipilih eksplisit per peran**, tidak dari urutan daftar.

### Kelompok dan item

| Kelompok | Item | Modul |
|---|---|---|
| (tanpa judul) | Beranda · Anak Saya (ortu) | inti · portal |
| Akademik | Siswa · Kelas · Jadwal / Jadwal Saya · Absensi / Rekap Absensi · Input Nilai · Rekap Nilai · Komponen Nilai · Mapel Saya (guru) | inti · inti · jadwal · absensi · nilai · nilai · nilai · inti |
| Ujian | Ujian · Agenda Ujian | ujian |
| Pendaftaran | Pendaftaran (admin, kepala) · Biaya Pendaftaran (bendahara) | pendaftaran |
| Keuangan | Catat Pembayaran · Tagihan Massal · Rekap Pembayaran · Tunggakan · Jenis Tagihan · Pembatalan · Audit Keuangan | keuangan |
| Dapodik | Dapodik · Nilai ke Dapodik (guru) | dapodik |
| Informasi | Pengumuman · Laporan | pengumuman · inti |
| (bawah) | Pengaturan | inti |

Item yang keluar dari menu ke **pusat Pengaturan**: Profil Sekolah, Tahun Ajaran, Mata Pelajaran, Penugasan Guru (admin), Tugas Tambahan, Pengaturan Rapor, Pengaturan Keuangan, Pengaturan Ujian, ditambah Pengaturan Pendaftaran dan Absensi per mapel yang sekarang tersembunyi. Telaah Soal pindah ke sub-navigasi modul ujian (T157).

### Hasil per peran (semua modul aktif)

| Peran | Sekarang | Usulan | Kelompok yang tampil |
|---|---:|---:|---|
| Admin TU | 24 | 17 | Akademik (6), Ujian (2), Pendaftaran, Keuangan (3), Dapodik, Informasi (2), Pengaturan |
| Kepala | 20 | 18 | Akademik (5), Ujian (2), Pendaftaran, Keuangan (5), Dapodik, Informasi (2), Pengaturan |
| Guru | 11 | 10 | Akademik (6), Ujian, Dapodik, Informasi |
| Bendahara | 10 | 10 | Keuangan (5), Pendaftaran, Informasi (2), Pengaturan |
| Orang tua | 2 | 2 | – |

Jumlah item admin TU dan kepala sekolah turun, tetapi yang lebih penting setiap item kini berada di kelompoknya. Kelompok dengan satu item tampil tanpa judul, tetapi dipisah garis tipis agar tidak terlihat ikut kelompok di atasnya.

### Bottom navigation HP (maksimal 4 + tombol Menu)

| Peran | Usulan | Pengganti bila modul tidak aktif |
|---|---|---|
| Admin TU | Beranda, Siswa, Tunggakan, Ujian | Rekap Absensi, Pengumuman |
| Kepala | Beranda, Rekap Absensi, Laporan, Tunggakan | Siswa, Pengumuman |
| Guru | Beranda, Absensi, Input Nilai, Ujian | Jadwal Saya, Pengumuman |
| Bendahara | Beranda, Catat Pembayaran, Tunggakan, Rekap Pembayaran | Laporan |
| Orang tua | Anak Saya, Pengumuman | – |

### Titik masuk yang ditambahkan
- **Impor nilai:** tombol "Impor dari Excel" di Input Nilai dan Rekap Nilai.
- **Pemetaan kolom impor:** dari halaman impor siswa dan nilai.
- **Laporan:** hub hanya menampilkan laporan dari modul aktif (kehadiran → absensi, tunggakan → keuangan, sumber pembuat laporan per modul).

## 4. Tindak lanjut
- [T156](tasks_schoolos_mvp4/21-rapikan/T156-menu-seluruh-aplikasi.md): menu berkelompok, pusat Pengaturan, bottom nav dan pintasan per peran, label, titik masuk impor.
- [T167](tasks_schoolos_mvp4/22-modul-sekolah/T167-modul-dan-paket-untuk-sekolah.md): pusat Pengaturan hanya menampilkan modul aktif, dan halaman "Modul & paket".
- Item menu mengikuti modul aktif: [ADR 0002](adr/0002-modul-per-sekolah.md) dan folder `22-modul-sekolah/`.
