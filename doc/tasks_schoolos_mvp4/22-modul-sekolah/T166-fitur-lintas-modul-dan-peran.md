# T166 — Fitur lintas modul dan peran mengikuti modul

**Fase:** 22 Modul per Sekolah
**Dependensi:** T163, T164
**Estimasi:** 1,5 hari

## Latar belakang
Beberapa fitur memakai dua modul sekaligus (ADR 0002 bagian 4), dan beberapa peran hanya berarti bila modul tertentu aktif.

## Langkah
1. **Ketergantungan opsional:** fitur disembunyikan dengan keterangan "Aktif bila modul X dipakai" bila modul itu mati:

   | Fitur | Butuh modul |
   |---|---|
   | Pendaftaran: biaya formulir dan kuitansi | **Tidak disembunyikan** (keputusan 4 ADR). Tanpa billing, admin TU mencatat biaya formulir di halaman pendaftar, dengan penomoran dan PDF kuitansi `payments` yang sama |
   | Pendaftaran: tes seleksi online | exam (r1); tanpa exam, nilai tes diisi manual seperti sebelum MVP 4 |
   | Ujian: kirim nilai ke rapor; tujuan "Ulangan" yang menulis ke rapor | grades |
   | Ujian: tujuan "Tes seleksi" | admission |
   | Dapodik: nilai ke Dapodik | grades |
   | Rapor: rekap kehadiran | attendance |
   | Siswa: tagihan di profil siswa | billing |

2. **Peran:**
   - **bendahara** hanya bisa ditambahkan ke pengguna bila billing atau admission aktif;
   - tugas **koordinator ujian** hanya bila exam aktif;
   - pengguna yang sudah punya peran itu tidak diubah, tetapi tanpa modulnya menu mereka hanya Beranda dan Pengumuman, dengan keterangan di beranda.
3. **Label peran di header** (`roleLabel`) tidak berubah.

## Catatan implementasi
- **Biaya formulir tanpa keuangan:**
  - admin TU boleh mencatat dan membatalkan biaya formulir hanya bila modul keuangan mati (`canRecordFee`); bila aktif, tetap bendahara (403 untuk admin);
  - nomor dan PDF kuitansi tetap dari `payments.NextReceiptNumber`, jadi berurutan;
  - halaman pendaftar menjelaskan bahwa admin TU yang mencatat, dengan tautan ke Biaya Pendaftaran.
- **Tes seleksi online tanpa modul ujian:**
  - pilihan "Ujian online" tidak ditawarkan (keterangan "Ujian online aktif bila modul Ujian Online dipakai"), dan tes baru atau tautan ujian baru ditolak di server;
  - tes online yang sudah ada **tetap tertaut** dan nilainya tetap: saat diubah, tautannya dibiarkan, tetapi boleh beralih ke "di kertas";
  - tautan ke halaman ujian (hasil, detail peserta, buat sesi) disembunyikan;
  - kartu tes online di halaman status pendaftar tidak tampil (dibaca per sekolah karena halaman publik).
- **Ujian ↔ nilai:**
  - `/ujian/paket/{id}/rapor` dan `/dapodik/nilai` kini masuk pola route modul nilai, jadi 404 bila nilai mati (registry ikut);
  - sub-tab dan tombol utama "Kirim ke rapor", tugas "Kirim ke rapor" di beranda ujian, dan pilihan komponen nilai di aturan ujian tidak tampil;
  - komponen yang sudah tersimpan tidak terhapus saat aturan disimpan.
- **Tujuan "Tes seleksi"** hanya ditawarkan bila pendaftaran aktif (`exam.Module.Offers`).
- **Nilai ke Dapodik:** item menu memakai `Needs` (modul tambahan yang wajib aktif), dan tautannya di halaman Dapodik ikut hilang.
- **Rapor tanpa absensi:** sudah di T165.
- **Profil siswa** belum punya bagian tagihan, jadi tidak ada yang diubah. Tombol akun portal wali di profil siswa kini mengikuti modul portal.
- **Peran:**
  - tidak ada form pengguna di aplikasi: peran diberikan tim implementator lewat `akra-seed`, yang kini menolak peran bendahara bila keuangan dan pendaftaran sama-sama mati;
  - tugas koordinator ujian di `/pengaturan/tugas` tidak ditawarkan dan ditolak bila ujian mati;
  - pengguna yang sudah punya peran atau tugas itu tidak diubah.
- **Bendahara tanpa keuangan:**
  - menu hanya Beranda dan Pengumuman (ditambah Biaya Pendaftaran bila pendaftaran aktif); hub Laporan dan Pengaturan yang kosong untuk pengguna itu kini disembunyikan;
  - beranda menjelaskan keadaannya.
- **Label peran di header** tidak berubah.

## Kriteria selesai
- [x] Uji setiap baris tabel: fitur tersembunyi dengan keterangan saat modulnya mati, dan route aksinya ditolak bila dipanggil langsung
- [x] Uji biaya formulir di sekolah tanpa billing: admin TU mencatat dan mengunduh kuitansi; nomor kuitansi tetap berurutan
- [x] Uji peran: bendahara ditolak `akra-seed` dan tugas koordinator tidak ditawarkan tanpa modulnya (tidak ada form pengguna di aplikasi)
- [x] Pengguna bendahara di sekolah tanpa billing bisa login dan melihat beranda yang menjelaskan keadaannya
