# Rancangan Arsitektur SchoolOS Indonesia

**Cakupan:** MVP 1 dan kesiapan untuk MVP 2
**Segmen awal:** sekolah swasta
**Status:** draf untuk ditinjau

---

## 1. Ringkasan

SchoolOS dibangun sebagai satu service monolit modular di Go. Halaman dirender di server (SSR) dengan `html/template`, dan Alpine.js dipakai untuk interaksi kecil di browser. Data disimpan di PostgreSQL, file di Cloudflare R2, dan proses latar belakang (kuitansi massal, impor Excel) memakai River, yang berjalan di Postgres. Seluruh aplikasi di-deploy di Railway sebagai satu binary.

## 2. Prinsip Arsitektur

- **Monolit modular.** Setiap domain berada di paket sendiri dengan batas yang jelas, sehingga bisa dipecah jika diperlukan.
- **Multi-tenant sejak awal.** Setiap data utama memiliki `school_id`. Isolasi dijaga di repository dan diuji.
- **Aturan bisnis di server.** Status tagihan, hak akses, dan validasi pembayaran tidak dihitung di browser.
- **Data keuangan bersifat append-only.** Pembatalan dicatat sebagai entri baru. Tidak ada penghapusan baris pembayaran.
- **Uang disimpan sebagai integer rupiah.** Tidak ada tipe float untuk nominal.
- **Konfigurasi, bukan kode, untuk perbedaan antarsekolah.** Format nilai, mapping impor, dan konfigurasi laporan disimpan sebagai data.

## 3. Komponen

```
[Browser: halaman SSR + Alpine.js]   [Browser: portal orang tua]
                 │                              │
                 └──────────────┬───────────────┘
                                ▼
                 [Service Go (monolit modular)]
                 ├─ auth        login, sesi, RBAC, CSRF
                 ├─ school      tahun ajaran, profil sekolah
                 ├─ classes     kelas, wali kelas
                 ├─ students    siswa, orang tua, enrollments
                 ├─ attendance  absensi harian
                 ├─ billing     jenis tagihan, tagihan massal
                 ├─ payments    pembayaran, bukti, pembatalan, kuitansi
                 ├─ reports     rekap, ekspor
                 ├─ importer    impor Excel per baris
                 ├─ audit       jejak perubahan
                 └─ platform    db, R2, konfigurasi, logger
                                │
          ┌─────────────────────┼──────────────────────┐
          ▼                     ▼                      ▼
    [PostgreSQL]          [Cloudflare R2]         [River worker]
    data utama, sesi,     bukti transfer,         kuitansi massal,
    antrean River         PDF, berkas ekspor      impor besar
```

Worker berjalan dalam proses yang sama pada tahap awal dan dapat dipisah menjadi service sendiri bila beban meningkat.

## 4. Stack

| Bagian | Pilihan |
|---|---|
| Bahasa dan router | Go, `chi` |
| Template | `html/template` (atau `templ` jika diputuskan) |
| Interaktivitas | Alpine.js, dimuat dari file lokal |
| Styling | Tailwind CSS (build sekali) atau CSS biasa |
| Database | PostgreSQL di Railway |
| Akses data | `pgx` + `sqlc` |
| Migrasi | `golang-migrate` |
| Sesi dan login | Cookie `httpOnly`, `SameSite=Lax`, disimpan di tabel `sessions` |
| CSRF | Token di setiap form POST |
| Antrean | River |
| File | Cloudflare R2 via AWS SDK for Go v2 |
| PDF | `maroto` atau `gofpdf` |
| Excel | `excelize` |
| Aset | `embed.FS` (template dan statis masuk ke binary) |

## 5. Struktur Folder

```
schoolos/
├── cmd/server/main.go
├── internal/
│   ├── auth/
│   ├── school/
│   ├── classes/
│   ├── students/
│   ├── attendance/
│   ├── billing/
│   ├── payments/
│   ├── reports/
│   ├── importer/
│   ├── audit/
│   └── platform/
├── web/
│   ├── templates/
│   ├── static/
│   └── embed.go
├── db/
│   ├── migrations/
│   └── queries/
└── Dockerfile
```

Setiap modul memiliki `handler.go` (HTTP dan render), `service.go` (aturan bisnis), dan `repo.go` (query).

## 6. Model Data Awal (Garis Besar)

- `schools`, `academic_years`, `classes`
- `students`, `guardians`, `student_guardians`
- `enrollments`: siswa di kelas pada tahun ajaran tertentu. Absensi dan nilai terhubung ke sini.
- `attendance_sessions`, `attendance_records`
- `fee_types`, `invoices`, `invoice_items`
- `payments`, `payment_proofs`, `payment_voids`
- `users`, `roles`, `user_roles`, `sessions`
- `audit_logs`
- Untuk MVP 2: `grade_components`, `grades`, `report_configs`, `import_mappings`, `announcements`

Semua tabel data utama memiliki `school_id`. Tabel yang berkaitan dengan tahun ajaran juga memiliki `academic_year_id`.

## 7. Alur Utama

1. **Awal tahun ajaran:** admin membuat tahun ajaran dan kelas, lalu mengimpor siswa dan orang tua. Impor diproses per baris; baris gagal dicatat dalam laporan kesalahan.
2. **Tagihan bulanan:** bendahara membuat tagihan massal untuk satu kelas atau semua kelas. Pembuatan dijalankan oleh River bila jumlah baris besar.
3. **Pembayaran:** bendahara mencatat pembayaran, mengunggah bukti transfer ke R2, lalu sistem memperbarui status tagihan dan membuat kuitansi PDF.
4. **Absensi:** guru membuka sesi absensi harian untuk kelasnya dan mengisi status tiap siswa.
5. **Portal orang tua:** orang tua login dan hanya melihat siswa yang terhubung dengannya.

## 8. Keamanan

- Autentikasi dengan cookie `httpOnly`, `Secure`, dan `SameSite=Lax`.
- CSRF token di setiap form yang mengubah data.
- RBAC diperiksa di middleware dan diulang di service untuk aksi sensitif.
- Setiap query data sekolah wajib menyertakan `school_id` dari sesi, tidak dari parameter URL.
- Orang tua hanya dapat mengakses siswa yang terhubung melalui `student_guardians`.
- Bukti transfer dan PDF di R2 diakses lewat URL bertanda tangan dengan masa berlaku singkat, bukan URL publik.
- Log akses dan perubahan data keuangan disimpan di `audit_logs`.
- Konfigurasi rahasia disimpan sebagai variabel lingkungan di Railway, tidak di repositori.

## 9. Operasional

- **Deploy:** satu service Railway dengan `Dockerfile` multi-stage, satu database PostgreSQL.
- **Backup:** `pg_dump` harian ke R2, dengan uji pemulihan sebelum pilot.
- **Monitoring:** log terstruktur, health check endpoint, dan notifikasi bila service gagal.
- **Lingkungan:** minimal `staging` dan `production` terpisah.
- **Penyimpanan file:** batas ukuran unggahan bukti transfer dan kompresi gambar.

## 10. Risiko

| No | Risiko | Dampak | Kemungkinan | Mitigasi |
|---|---|---|---|---|
| R1 | Kebocoran data antar keluarga atau sekolah karena handler lupa memeriksa `school_id` atau kepemilikan anak | Tinggi | Sedang | Pengujian otomatis lintas tenant, kode akses terpusat di repository, tinjauan kode untuk setiap route baru |
| R2 | Satu proses melambat saat impor besar atau kuitansi massal | Sedang | Tinggi | Pekerjaan berat dipindah ke River, batas ukuran impor, indikator progres |
| R3 | Ekosistem Go + SSR + Alpine kurang umum, sulit mencari bantuan | Sedang | Sedang | Dokumentasi pola yang dipakai, struktur modul yang seragam, pemilihan library yang populer |
| R4 | Autentikasi, RBAC, CSRF, dan reset password ditulis sendiri dan mengandung celah | Tinggi | Sedang | Gunakan library yang sudah matang, uji alur login dan reset, batasi percobaan login |
| R5 | Selisih nominal pembayaran karena pembulatan atau transfer yang tidak persis sama | Tinggi | Tinggi | Uang sebagai integer, status "sebagian", pencatatan selisih, rekonsiliasi manual oleh bendahara |
| R6 | Pembayaran ganda atau pembatalan yang tidak konsisten | Tinggi | Sedang | Append-only, validasi duplikasi bukti, aturan siapa yang boleh membatalkan, pencatatan di audit log |
| R7 | Laporan yang bisa disesuaikan tumbuh menjadi query builder yang sulit dirawat | Sedang | Tinggi | Batasi kolom dan operator yang tersedia, tidak merangkai SQL dari input pengguna, tambahkan fitur bertahap |
| R8 | Laporan lambat untuk data besar | Sedang | Sedang | Indeks pada `school_id`, `academic_year_id`, dan `class_id`; ukur dengan data uji sebelum pilot |
| R9 | Alpine.js tidak cocok untuk layar interaktif berat di MVP 2 | Sedang | Sedang | Pisahkan halaman berat dengan komponen JavaScript terpisah, tanpa mengubah backend |
| R10 | Ketergantungan pada koneksi internet, tanpa dukungan offline | Sedang | Sedang | Halaman ringan, kompresi, uji di jaringan lambat, komunikasi bahwa offline belum didukung |
| R11 | Data berada di luar Indonesia | Sedang | Sedang | Konfirmasi kebutuhan sekolah dan UU Pelindungan Data Pribadi; pertimbangkan region yang sesuai |
| R12 | Bus factor: pengetahuan terpusat pada satu orang | Tinggi | Sedang | Dokumentasi arsitektur, pola kode, prosedur deploy, dan backup; tinjauan kode oleh orang kedua |
| R13 | Backup tidak bisa dipulihkan saat dibutuhkan | Tinggi | Rendah | Uji pemulihan berkala, catat waktu pemulihan |
| R14 | Biaya Railway naik karena pemakaian tidak terduga | Sedang | Sedang | Pantau pemakaian, batas sumber daya, cek harga terbaru sebelum pilot |
| R15 | Pengujian kurang sehingga regresi tidak terdeteksi | Tinggi | Tinggi | Uji unit untuk aturan keuangan, uji integrasi untuk alur utama, uji otorisasi lintas tenant |

## 11. Keputusan Teknis

| Keputusan | Pilihan | Alasan |
|---|---|---|
| Template | `html/template` | Tanpa langkah generate kode dan tanpa dependensi tambahan. `templ` ditunda sampai tim dan kebutuhan membesar. |
| Styling | CSS biasa dengan CSS variables | Tanpa build step Node. Bila perlu utility class, gunakan Tailwind standalone CLI (binary, tanpa Node). |
| Sesi | `scs` (alexedwards/scs) | Library sesi yang sudah teruji, disimpan di Postgres. |
| Hash password | `argon2id` | Standar yang direkomendasikan untuk hash password. |
| CSRF | `nosurf` | Library CSRF yang sudah teruji untuk `net/http`. |
| Reset password | Dibuat sendiri | Tabel token sekali pakai dengan masa berlaku singkat (misalnya 30 menit), token disimpan dalam bentuk hash. |
| Region Railway | Singapura | Paling dekat dengan Indonesia. Kebutuhan lokasi data dikonfirmasi ke calon sekolah sebelum pilot. |

## 12. Keputusan Bisnis

| Keputusan | Pilihan | Catatan |
|---|---|---|
| Aturan pembatalan pembayaran | Hanya bendahara yang bisa membatalkan, wajib mengisi alasan, pembatalan di atas nominal tertentu perlu persetujuan kepala sekolah, dan hanya dapat dilakukan dalam bulan yang sama | Batas nominal dan jangka waktu disesuaikan dengan hasil wawancara |
| Model pilot | Berbayar dengan harga diskon | Menguji kesediaan membayar, sesuai ukuran keberhasilan di rencana produk |

## 13. Risiko Terkait Keputusan

- Jika library autentikasi dan sesi tidak dipakai, risiko R4 meningkat dan perlu pengujian keamanan tambahan.
- Jika lokasi data di Indonesia menjadi syarat sekolah, region dan hosting perlu diubah sebelum pilot.

## 14. Langkah Berikutnya

1. Tinjau dan setujui dokumen ini.
2. Susun skema database MVP 1 dari garis besar di bagian 6.
3. Tulis pengujian otorisasi lintas tenant dan pengujian aturan keuangan sebelum modul dibangun.
4. Siapkan repositori, Dockerfile, dan deploy kosong ke Railway untuk memastikan alurnya berjalan.