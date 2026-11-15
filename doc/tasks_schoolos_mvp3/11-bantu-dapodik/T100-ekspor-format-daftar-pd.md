# T100 — Ekspor format Daftar Peserta Didik Dapodik

**Fase:** 11 Bantu Dapodik
**Dependensi:** T86, T87
**Estimasi:** 1 hari

## Tujuan
SchoolOS bisa menghasilkan Excel daftar siswa dengan kolom yang sama persis dengan ekspor "Daftar Peserta Didik" dari Dapodik, untuk dinas, pencocokan data, atau arsip.

## Langkah
1. Acuan: `template_excel/template-daftar-pd.xlsx` Dapodik 2027. Baris 1–4 judul dan info sekolah, header di baris 5–6 dengan sel gabungan, 66 kolom dari `No` sampai `Jarak Rumah ke Sekolah (KM)`.
2. Kolom bergrup (Data Ayah, Data Ibu, Data Wali) memakai header dua baris: Nama, Tahun Lahir, Jenjang Pendidikan, Pekerjaan, Penghasilan, NIK.
3. Pilihan diekspor sebagai label (sama seperti ekspor Dapodik), bukan kode.
4. NIK, No KK, NISN, dan nomor lain disimpan sebagai teks agar nol di depan tidak hilang.
5. Filter: semua siswa aktif atau per kelas. Akses: `admin_tu`, `kepala_sekolah`. NIK dan No KK didekripsi hanya saat membuat file; setiap ekspor yang memuat NIK dicatat di audit.
6. Dibuat dengan excelize, sebagai job jika lebih dari 500 siswa (pakai pola ekspor laporan yang ada).
7. File hasil ekspor di storage dihapus otomatis setelah diunduh atau paling lama 24 jam.

## File yang disentuh
- `internal/dapodik/export_pd.go`, `export_pd_test.go`

## Kriteria selesai
- [x] Header hasil ekspor identik dengan template Dapodik 2027 (uji membandingkan sel baris 5–6)
- [x] NIK `0012…` tetap diawali nol saat dibuka di Excel
- [x] File hasil ekspor bisa diimpor kembali lewat T101 tanpa perubahan

## Catatan implementasi
Ekspor dibuat langsung untuk maksimal 3.000 siswa. File langsung dikirim ke browser (tidak disimpan di storage), jadi tidak perlu penghapusan 24 jam.
