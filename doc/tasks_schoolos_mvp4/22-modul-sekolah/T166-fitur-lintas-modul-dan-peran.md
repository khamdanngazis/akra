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

## Kriteria selesai
- [ ] Uji setiap baris tabel: fitur tersembunyi dengan keterangan saat modulnya mati, dan route aksinya ditolak bila dipanggil langsung
- [ ] Uji biaya formulir di sekolah tanpa billing: admin TU mencatat dan mengunduh kuitansi; nomor kuitansi tetap berurutan
- [ ] Uji form pengguna: peran bendahara dan tugas koordinator tidak ditawarkan tanpa modulnya
- [ ] Pengguna bendahara di sekolah tanpa billing bisa login dan melihat beranda yang menjelaskan keadaannya
