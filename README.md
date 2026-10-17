# AKRA

AKRA - One Platform for Every School.

Sistem manajemen sekolah (SchoolOS) untuk sekolah swasta di Indonesia. Rancangan ada di [doc/software-architecture.md](doc/software-architecture.md), rencana MVP di [doc/rencana-project.md](doc/rencana-project.md), dan daftar task di [doc/tasks_schoolos/00-index.md](doc/tasks_schoolos/00-index.md).

## Prasyarat

- Go 1.22.2 atau lebih baru
- `make`
- CLI [`golang-migrate`](https://github.com/golang-migrate/migrate) v4.20.1 (alat terpisah, bukan dependensi `go.mod`)

Pasang `golang-migrate` di Linux amd64:

```sh
curl -sSL https://github.com/golang-migrate/migrate/releases/download/v4.20.1/migrate.linux-amd64.tar.gz \
  | tar -xz migrate
install -m 0755 migrate ~/.local/bin/migrate
migrate -version
```

Untuk sistem lain, lihat [halaman rilis](https://github.com/golang-migrate/migrate/releases) atau `brew install golang-migrate` di macOS.

## Menjalankan

Siapkan PostgreSQL. Cara termudah dengan Docker:

```sh
docker compose up -d
```

Jika port 5432 sudah dipakai Postgres lain, pilih port lain dengan `POSTGRES_PORT=5433 docker compose up -d`, atau pakai Postgres yang sudah ada dan buat database `akra`.

Salin contoh konfigurasi, jalankan migrasi, lalu jalankan server:

```sh
cp .env.example .env
set -a && . ./.env && set +a
make migrate-up
go run ./cmd/server
```

Cek server:

```sh
curl -i localhost:8080/healthz      # proses hidup
curl -i localhost:8080/healthz/db   # 200 jika database terjangkau, 503 jika tidak
```

Build binary:

```sh
go build -o bin/akra ./cmd/server
```

Template dan aset statis (`web/templates`, `web/static`) ikut masuk ke binary lewat `embed.FS` ([web/embed.go](web/embed.go)), jadi deploy cukup satu file. Perubahan template atau CSS memerlukan rebuild, kecuali saat development dengan `WEB_DIR=web` (cukup restart).

Ukuran binary (Go 1.22.2, linux/amd64, 17 Oktober 2026):

| Build | Ukuran |
|---|---|
| `go build -o bin/akra ./cmd/server` | 16,9 MB |
| `go build -trimpath -ldflags="-s -w" -o bin/akra ./cmd/server` | 11,7 MB |

## Konfigurasi

| Variabel | Wajib | Default | Keterangan |
|---|---|---|---|
| `DATABASE_URL` | ya | | Connection string PostgreSQL |
| `DB_MAX_CONNS` | tidak | `10` | Batas koneksi pool (`MinConns` = 2) |
| `PORT` | tidak | `8080` | Port HTTP |
| `APP_ENV` | tidak | `development` | `development`, `staging`, atau `production` |
| `APP_BASE_URL` | ya, kecuali `development` | `http://localhost:<PORT>` | URL publik aplikasi tanpa path (mis. `https://app.akra.id`), untuk tautan di email. Tidak pernah diambil dari header `Host` |
| `MAIL_DRIVER` | tidak (wajib `resend` di production) | `log` | `log`: email ditulis ke log (dev/staging). `resend`: kirim lewat [Resend HTTP API](https://resend.com/docs/api-reference/emails/send-email) |
| `MAIL_FROM` | ya untuk `resend` | `AKRA <no-reply@akra.local>` | Alamat pengirim; domainnya harus sudah diverifikasi di Resend |
| `RESEND_API_KEY` | ya untuk `resend` | | API key Resend (`re_...`). Simpan di variabel Railway, jangan di repo |
| `CLIENT_IP_HEADER` | tidak | (kosong) | Header berisi IP klien dari proxy. Railway: `X-Real-IP` (sudah diuji tidak bisa dipalsukan klien). Kosong = alamat koneksi |
| `RIVER_WORKERS` | tidak | `4` | Jumlah job latar belakang yang berjalan bersamaan (River) |
| `WEB_DIR` | tidak | (kosong) | Baca template dan aset dari folder ini (mis. `web`) alih-alih dari binary. Hanya boleh saat `APP_ENV=development` |
| `ARGON2_MEMORY_KIB` | tidak | `19456` | Memori argon2id per hash (KiB) |
| `ARGON2_ITERATIONS` | tidak | `2` | Iterasi argon2id |
| `ARGON2_PARALLELISM` | tidak | `1` | Paralelisme argon2id |

Default argon2id mengikuti batas minimum OWASP (19 MiB, t=2, p=1). Mengubah parameter hanya berlaku untuk hash baru; hash lama tetap bisa diverifikasi karena parameternya tersimpan di dalam hash.

Server tidak akan start jika database tidak bisa dijangkau dalam 5 detik.

## Login

Login memakai **kode sekolah**, email, dan password. Email unik per sekolah, jadi kode sekolah (`schools.code`, mis. `sd-harapan`) menentukan tenant.

- Semua kegagalan kredensial (sekolah tidak ada, email tidak terdaftar, password salah, akun nonaktif) menampilkan pesan yang sama.
- Percobaan dicatat di `login_attempts`. Login dikunci selama 15 menit setelah 5 kegagalan untuk pasangan kode sekolah + email (dihitung sejak login sukses terakhir), atau 20 kegagalan dari satu IP. Batas IP lebih longgar karena banyak pengguna satu sekolah berbagi satu IP.
- Setelah login berhasil, token sesi diganti (mencegah session fixation) lalu `user_id`, `school_id`, dan daftar peran disimpan di sesi. Logout hanya lewat `POST /logout`.

### Reset password

Halaman `/lupa-password` meminta kode sekolah dan email, lalu `/reset-password?token=...` menampilkan form password baru.

- Pesan setelah meminta reset selalu sama, baik email terdaftar maupun tidak.
- Token: 32 byte dari `crypto/rand` (base64url di tautan). Database hanya menyimpan SHA-256-nya (`password_reset_tokens.token_hash`).
- Tautan berlaku 30 menit dan sekali pakai. Reset yang berhasil membatalkan semua tautan lain milik pengguna dan menghapus semua sesi aktifnya.
- Maksimal 3 tautan per pengguna per 30 menit; permintaan berikutnya diabaikan diam-diam.
- Password baru minimal 8 karakter.
- Email dikirim lewat [internal/platform/mail](internal/platform/mail/mail.go) sesuai `MAIL_DRIVER`:
  - `log` (default di development dan staging): isi email, **termasuk tautan reset**, ditulis ke log dengan level `WARN` (`email written to log instead of being sent`). Jangan bagikan log yang berisi tautan ini.
  - `resend`: `POST https://api.resend.com/emails` dengan `Authorization: Bearer $RESEND_API_KEY`. Wajib di production; server menolak start jika production memakai `log` atau `RESEND_API_KEY` kosong.
  - Gagal kirim hanya dicatat di log (`send password reset email`); pengguna tetap melihat pesan sukses yang sama agar email terdaftar tidak terbongkar.

### Sesi

Sesi dikelola [`scs`](https://github.com/alexedwards/scs) dan disimpan di tabel `sessions` (pgxstore).

| Pengaturan | Nilai |
|---|---|
| Cookie | `akra_session`, `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` di luar `development` |
| Lifetime | 12 jam (batas mutlak sejak login) |
| Idle timeout | 2 jam tanpa aktivitas |
| Pembersihan sesi kedaluwarsa | Otomatis oleh pgxstore setiap 5 menit |

- Selama belum ada halaman admin, pengguna dibuat langsung lewat SQL dengan hash dari `auth.Hasher`.

### Route yang wajib login

Middleware `auth.RequireLogin` mengalihkan request tanpa sesi ke `/login?next=<path>`. Parameter `next` hanya menerima path lokal (diawali `/`, bukan `//` atau URL absolut), selain itu diganti `/`. Route publik hanya `/healthz`, `/healthz/db`, `/login`, `/logout`, `/lupa-password`, `/reset-password`, `/aktivasi`, dan `/static/`. Semua route baru dipasang di grup `RequireLogin` di [cmd/server/routes.go](cmd/server/routes.go).

### CSRF

Semua request POST, PUT, PATCH, dan DELETE di grup aplikasi wajib membawa token CSRF ([nosurf](https://github.com/justinas/nosurf)), kecuali route health check yang berada di luar grup.

- Di form: sisipkan `{{ csrfField }}` (menghasilkan `<input type="hidden" name="csrf_token" ...>`).
- Untuk AJAX (Alpine.js): baca `<meta name="csrf-token" content="{{ csrfToken }}">` dan kirim lewat header `X-CSRF-Token`.
- Template di-parse dengan `csrf.PlaceholderFuncs`, lalu dirender dengan `tmpl.Clone()` + `Funcs(csrf.FuncMap(r))`. Lupa memasang `FuncMap` membuat render gagal, bukan menghasilkan form tanpa token.
- Cookie `akra_csrf`: `HttpOnly`, `SameSite=Lax`, `Path=/`, `Secure` di luar `development`. nosurf juga menolak request dengan header `Origin`/`Referer` dari host lain.
- Uji `TestCSRF_AllMutatingRoutesRequireToken` di [cmd/server/routes_test.go](cmd/server/routes_test.go) menelusuri semua route dan gagal jika ada route yang mengubah data tanpa perlindungan CSRF.

### Pembatasan peran (RBAC)

Peran tersimpan di sesi saat login dan dibaca ke `auth.Identity` oleh `RequireLogin`. `auth.RequireRole` meneruskan request jika pengguna punya **minimal satu** peran yang disebut; selain itu `403 Akses ditolak`.

| Konstanta | Kode | Peran |
|---|---|---|
| `auth.RoleAdminTU` | `admin_tu` | Admin/TU |
| `auth.RoleBendahara` | `bendahara` | Bendahara |
| `auth.RoleGuru` | `guru` | Guru |
| `auth.RoleKepalaSekolah` | `kepala_sekolah` | Kepala sekolah |
| `auth.RoleOrangTua` | `orang_tua` | Orang tua |

Contoh, di dalam grup `RequireLogin` di [cmd/server/routes.go](cmd/server/routes.go):

```go
// Peran: bendahara, kepala_sekolah (hanya baca).
r.With(auth.RequireRole(auth.RoleBendahara, auth.RoleKepalaSekolah)).Get("/tagihan", billingHandler.list)

// Peran: bendahara.
r.With(auth.RequireRole(auth.RoleBendahara)).Post("/tagihan", billingHandler.create)
```

Untuk logika di dalam handler atau service, gunakan `auth.HasAnyRole(id, ...)` atau `id.HasRole(...)`. RBAC tidak membatasi sekolah; isolasi `school_id` tetap wajib di setiap query (T58).

## Tampilan (SSR)

Halaman dirender di server dengan `html/template` lewat [internal/platform/render](internal/platform/render/render.go). Semua template di-parse saat start; template yang rusak membuat server gagal start dengan nama file-nya.

```
web/templates/
├── layout.html          kerangka halaman: define "layout", memanggil blok "title" dan "content"
├── partials/            template bersama (logo, flash, app_header)
├── auth/login.html      halaman "auth/login"
└── home.html            halaman "home"
web/static/
├── css/app.css          CSS variables di :root (warna brand, jarak, font), tanpa Tailwind
├── js/alpine.min.js     Alpine.js 3.17.4 (dist/cdn.min.js dari npm alpinejs@3.17.4)
├── fonts/               Plus Jakarta Sans variable, subset latin (@fontsource-variable/plus-jakarta-sans@5.3.0, lisensi OFL)
└── img/favicon.svg
```

- Halaman baru: buat `web/templates/<nama>.html` yang mendefinisikan blok `title` dan `content`, lalu panggil `rd.Render(w, r, http.StatusOK, "<nama>", data)`.
- Halaman 404/500 bersama ada di [internal/platform/page](internal/platform/page/page.go) (`page.NotFound`, `page.ServerError`).
- Fungsi template umum: `{{ seq 1 12 }}` untuk daftar angka.
- `/static/` dilayani dengan `Cache-Control: public, max-age=86400`. Di template, rujuk aset dengan `{{ asset "css/app.css" }}`, yang menghasilkan `/static/css/app.css?v=<hash isi file>`. URL berubah setiap isi file berubah, jadi browser tidak menahan versi lama. Pengecualian: font, karena URL-nya juga dipakai `url()` di CSS.
- Tidak ada aset dari CDN; semua dimuat dari `/static/`. Uji `TestTemplates_NoExternalResources` gagal jika template atau CSS memuat URL eksternal.
- Pesan sekali tampil memakai `auth.SetFlash` / `auth.PopFlash` (disimpan di sesi), bukan query string.
- Tabel lebar dibungkus `<div class="table-wrap">` agar halaman tidak scroll horizontal di layar 360px.

## Tahun Ajaran

Tabel `academic_years` (migrasi 000008), fungsi di [internal/school/academic_year_repo.go](internal/school/academic_year_repo.go).

- Tepat satu tahun ajaran aktif per sekolah, dijaga unique partial index `academic_years_one_active`. Tahun ajaran baru selalu nonaktif.
- `end_date` harus setelah `start_date`, dan nama unik per sekolah (database menolak keduanya).
- Ganti tahun aktif hanya lewat `school.SetActive(ctx, tx, schoolID, yearID)` di dalam transaksi. Fungsi ini mengunci semua tahun ajaran sekolah dulu agar dua aktivasi bersamaan tidak bertabrakan.
- Halaman `/pengaturan/tahun-ajaran`: daftar untuk `admin_tu` dan `kepala_sekolah` (hanya baca); tambah, ubah, dan aktifkan hanya `admin_tu`. Mengaktifkan memakai konfirmasi (Alpine). Tidak ada tombol hapus.
- Setiap create, update, dan activate dicatat di `audit_logs` (`academic_year.create`, `.update`, `.activate`) dalam transaksi yang sama.
- ID di URL divalidasi formatnya (`database.IsUUID`) sebelum query; ID milik sekolah lain atau tidak valid menghasilkan `404`.
- Semua fungsi repository menerima `school_id` dan memperlakukan data sekolah lain sebagai tidak ada (`school.ErrNotFound`). Fungsi repository menerima `database.DBTX` (pool atau transaksi), sehingga perubahan data dan `audit.Record` bisa berada di satu transaksi.

## Kelas

Tabel `classes` (migrasi 000009), fungsi di [internal/classes/repo.go](internal/classes/repo.go): `Create`, `GetByID`, `ListByYear`.

- Kelas terikat ke satu tahun ajaran. Nama unik per tahun ajaran tanpa membedakan huruf besar/kecil (`1A` = `1a`); nama sama di tahun ajaran lain diizinkan.
- `grade_level` 1–12 dijaga `CHECK` di database.
- Tahun ajaran dan wali kelas wajib dari sekolah yang sama. Dijaga dua lapis: repository memeriksa `WHERE school_id = $1 AND id = $2` (pesan error jelas), dan **foreign key komposit** `(school_id, academic_year_id)` serta `(school_id, homeroom_teacher_id)` menolak data lintas sekolah walau repository dilewati.
- Pola foreign key komposit ini dipakai untuk tabel berikutnya yang merujuk data milik sekolah.
- Halaman `/kelas?tahun=<id>` (default tahun aktif, atau terbaru jika belum ada yang aktif): daftar untuk `admin_tu` dan `kepala_sekolah` (hanya baca); tambah dan ubah hanya `admin_tu`. Tahun ajaran kelas tidak bisa dipindah setelah dibuat.
- Wali kelas dipilih dari guru **aktif** sekolah yang sama (`classes.ListTeachers`); service menolak pengguna lain. Wali kelas lama yang sudah dinonaktifkan tetap tampil di form ubah (bertanda "nonaktif") agar menyimpan perubahan nama tidak diam-diam menghapusnya.
- Audit: `class.create`, `class.update` (nama/tingkat), `class.assign_teacher` (wali kelas). Menyimpan tanpa perubahan tidak menambah audit.

## Siswa

Tabel `students` (migrasi 000010), fungsi di [internal/students/repo.go](internal/students/repo.go): `Create`, `GetByID`, `Update`, `Search`. Keterikatan siswa ke kelas ada di `enrollments` (T21), bukan di tabel ini.

- NIS disimpan sebagai **teks** (`007` tetap `007`), unik per sekolah. Normalisasi dengan `students.NormalizeNIS` (trim); database menolak NIS kosong atau berspasi di awal/akhir.
- `gender`: `L`, `P`, atau kosong. `status`: `aktif` (default), `pindah`, `lulus`, `keluar`.
- `Search(schoolID, query, limit)` mencocokkan nama yang **mengandung** query atau NIS yang **diawali** query, tanpa membedakan huruf besar/kecil; `%` dan `_` di query dicocokkan apa adanya. Paginasi dikerjakan di T22.
- Data siswa adalah data pribadi: jangan tulis ke log. Error database yang tidak dikenali hanya membawa kode SQLSTATE dan nama constraint, bukan pesan PostgreSQL yang bisa memuat nilai baris.
- Halaman `/siswa?q=&tahun=&kelas=&status=&page=`: 25 siswa per halaman, filter tetap terbawa di tautan halaman. Kolom kelas diambil dari enrollment aktif di tahun ajaran yang dipilih (default tahun aktif). Akses: `admin_tu` penuh, `kepala_sekolah` hanya baca, guru dan orang tua `403` (guru lewat absensi T26, orang tua lewat portal T43).
- Tambah siswa **wajib** dengan kelas tahun aktif (dipilih lewat `<datalist>`, dicocokkan tanpa membedakan huruf besar/kecil). Siswa dan enrollment awalnya (mulai hari ini) dibuat dalam satu transaksi. Jika tahun aktif sudah berakhir, penambahan ditolak dengan pesan untuk mengaktifkan tahun ajaran berjalan.
- Form ubah hanya untuk data pribadi dan status. Pindah kelas dari halaman detail (`POST /siswa/{id}/pindah-kelas`), yang selalu membuat enrollment baru. Halaman detail menampilkan riwayat kelas.
- Audit: `student.create`, `student.update` (hanya bila ada perubahan), `student.place`.
- Diukur di uji: daftar dengan 500 siswa dan pencarian selesai sekitar 10 ms per halaman.
- Index `(school_id, full_name)` membantu daftar urut nama. Untuk pencarian "mengandung", index ini tidak terpakai; bila nanti lambat, pertimbangkan `pg_trgm` (lihat T22).

## Penempatan Kelas (Enrollment)

Tabel `enrollments` (migrasi 000011), fungsi di [internal/students/enrollment_repo.go](internal/students/enrollment_repo.go) dan [enrollment_service.go](internal/students/enrollment_service.go).

- Riwayat penempatan siswa di kelas per tahun ajaran. Satu enrollment aktif (`end_date IS NULL`) per siswa per tahun ajaran, dijaga unique partial index `enrollments_one_active`.
- **Pindah kelas = enrollment baru**, bukan edit kolom: `students.Place(ctx, tx, ...)` menutup enrollment aktif dengan `end_date = tanggal - 1` lalu membuat enrollment baru mulai tanggal itu, dalam satu transaksi. Enrollment lama tetap ada sebagai riwayat.
- `Place` mengunci baris siswa (`FOR UPDATE`) agar pemindahan bersamaan tidak bertabrakan. Pindah ke kelas yang sama tidak mengubah apa pun.
- Pindah di hari yang sama dengan tanggal mulai (koreksi salah kelas) menghasilkan baris lama dengan `end_date = start_date - 1`: periode kosong, tidak dihitung di rekap, tapi riwayatnya tetap ada. Tanggal sebelum awal tahun ajaran dimajukan ke awal tahun; setelah akhir tahun ditolak.
- Foreign key komposit `(school_id, academic_year_id, class_id) → classes` menjamin kelas berasal dari sekolah **dan** tahun ajaran yang sama; `(school_id, student_id) → students` untuk siswa.
- `EnrollmentService.Place` mencatat audit `student.place` (before: enrollment yang ditutup, after: enrollment baru).
- "Hari ini" selalu menurut **Asia/Jakarta** (`clock.Today()`), bukan zona server (Railway memakai UTC). Data zona waktu di-embed ke binary.

## Orang Tua/Wali

Tabel `guardians` dan `student_guardians` (migrasi 000012), fungsi di [internal/students/guardian_repo.go](internal/students/guardian_repo.go) dan [guardian_service.go](internal/students/guardian_service.go). Belum ada halaman; data wali akan diisi lewat impor (T52–T57) dan dipakai akun portal (T24).

- Banyak-ke-banyak: satu siswa bisa punya beberapa wali, satu wali beberapa anak. Hubungan: `ayah`, `ibu`, `wali`, `lainnya`.
- Satu **penanggung jawab utama** per siswa (unique partial index `student_one_primary`). `LinkToStudent(..., isPrimary=true)` melepas penanggung jawab utama lama dalam transaksi yang sama.
- `student_guardians` menyimpan `school_id`; foreign key komposit menjamin siswa dan wali dari sekolah yang sama.
- Telepon dinormalisasi ke satu format `+62…` (`students.NormalizePhone`): `0812-3456-7890`, `+62 812 3456 7890`, dan `81234567890` semuanya menjadi `+6281234567890`. Email disimpan huruf kecil.
- Audit: `guardian.create`, `guardian.link`, `guardian.unlink`.

### Akun portal orang tua

Migrasi 000013 menambah `users.guardian_id` (unik, foreign key komposit ke `guardians`) dan tabel `activation_tokens`.

- Di detail siswa, bagian **Orang tua/wali** menampilkan status akun tiap wali. `admin_tu` bisa **Buat akun portal** (hanya wali yang punya email) atau **Kirim ulang aktivasi** untuk akun yang belum aktif ([guardian_account.go](internal/students/guardian_account.go)).
- Akun dibuat **nonaktif tanpa password** dengan peran `orang_tua`; `school_id` diambil dari wali. Satu wali satu akun. Email yang sudah dipakai akun lain di sekolah yang sama ditolak.
- Email aktivasi (lewat `MAIL_DRIVER`) berisi tautan `/aktivasi?token=…` yang berlaku **7 hari**, sekali pakai, dan memuat **kode sekolah** untuk login. Token disimpan sebagai SHA-256. Kirim ulang membatalkan tautan lama.
- Orang tua membuat password sendiri di `/aktivasi` ([activation.go](internal/auth/activation.go)). Sebelum aktivasi, login selalu gagal dengan pesan yang sama seperti kredensial salah.
- Setelah login, orang tua diarahkan ke `/portal` (parameter `next` ke halaman admin diabaikan). `/portal` hanya untuk peran `orang_tua`; semua halaman admin menolak orang tua dengan `403`, dan `/` mengalihkan orang tua ke `/portal`. Isi portal dikerjakan di T43–T46.
- Audit: `guardian_account.create`, `guardian_account.resend_activation`, `guardian_account.activate`.

## Absensi

Tabel `attendance_sessions` dan `attendance_records` (migrasi 000014), fungsi di [internal/attendance](internal/attendance/repo.go).

- Satu **sesi** per kelas per tanggal (`UNIQUE (class_id, session_date)`), tanggal lokal Asia/Jakarta. `GetOrCreateSession` memakai `INSERT … ON CONFLICT DO NOTHING` lalu `SELECT`, sehingga dua guru yang membuka absensi bersamaan tetap mendapat satu sesi. Tanggal harus di dalam tahun ajaran kelas.
- Satu **catatan** per siswa (enrollment) per sesi, status `hadir`, `sakit`, `izin`, atau `alpa` (default `hadir`).
- `EnsureRecords` memakai enrollment yang aktif **pada tanggal sesi** (`start_date <= tanggal` dan `end_date` kosong atau `>= tanggal`). Siswa yang pindah kelas hanya muncul di kelas yang benar untuk setiap tanggal. Catatan yang sudah ada tidak diubah dan tidak pernah dihapus; koreksi dengan mengubah status.
- Foreign key komposit menjamin sesi, kelas, enrollment, dan pembuat sesi berasal dari sekolah yang sama.
- **Form guru** `GET /absensi?kelas=&tanggal=` dan `POST /absensi/{sessionID}` ([handler.go](internal/attendance/handler.go)):
  - Hanya peran `guru`, dan hanya **wali kelas** dari kelas itu (`attendance.CanEditClass`). Guru lain, admin/TU, kepala sekolah, dan orang tua mendapat `403`. Belum ada penugasan guru mata pelajaran di MVP 1.
  - Tanpa `?kelas`, guru melihat kelas perwaliannya di tahun aktif (langsung dibuka jika hanya satu). Tanggal default hari ini (WIB); tanggal masa depan dan di luar tahun ajaran ditolak.
  - Semua status disimpan dengan **satu POST** dan satu `UPDATE` dalam satu transaksi; satu baris tidak valid membatalkan semuanya. Tombol "Tandai semua hadir" (Alpine) hanya mengubah pilihan di browser. Setelah simpan tampil ringkasan hadir/sakit/izin/alpa.
  - Dibuat untuk HP: tombol status 44px, tombol simpan menempel di bawah. Diukur di uji: simpan 30 siswa sekitar 10 ms.
  - **Belum diputuskan:** batas berapa hari ke belakang absensi masih boleh diubah (saat ini tidak dibatasi). Tanyakan ke sekolah saat wawancara.
- **Rekap** `GET /absensi/rekap?kelas=&dari=&sampai=` ([report.go](internal/attendance/report.go)):
  - Akses: `admin_tu` dan `kepala_sekolah` untuk semua kelas sekolahnya, `guru` hanya kelas perwaliannya (lainnya `403`).
  - `SummaryByStudent` dan `SummaryByClass` menghitung hadir/sakit/izin/alpa dengan `GROUP BY`, **hanya** untuk sesi yang tanggalnya di dalam masa enrollment siswa. Siswa yang pindah kelas terhitung di kelas yang benar untuk setiap tanggal.
  - Persentase kehadiran = hadir ÷ (hadir + sakit + izin + alpa). **Definisi ini perlu dikonfirmasi ke sekolah.**
  - Rentang default: awal tahun ajaran sampai hari ini; maksimal satu tahun ajaran kelas (di luar itu ditolak dengan pesan yang menyebut batasnya).
  - Diukur di uji: 30 siswa × 60 sesi sekitar 16 ms. Tombol unduh Excel masih placeholder (T47).

## Keuangan

Semua nominal disimpan sebagai **bilangan bulat rupiah** (`bigint`/`int64`), tidak pernah float. Isian pengguna dibaca dengan `money.ParseRupiah` ([internal/platform/money](internal/platform/money/money.go)), yang menerima `150000`, `150.000`, `Rp 150.000`, dan `150.000,-`, serta menolak pecahan dan angka negatif. Tampilan memakai `money.FormatRupiah` (`Rp150.000`).

### Jenis tagihan

Tabel `fee_types` (migrasi 000015), fungsi di [internal/billing](internal/billing/fee_type_service.go).

- Nominal default `>= 0` (nol untuk jenis tagihan gratis). Tanggal jatuh tempo default 1–28 (menghindari masalah Februari) atau kosong.
- Nama unik per sekolah tanpa membedakan huruf besar/kecil, maksimal 60 karakter.
- Jenis tagihan **tidak dihapus**, hanya dinonaktifkan; `ListActiveFeeTypes` (untuk tagihan baru) tidak mengembalikan yang nonaktif.
- Nominal yang berbeda per kelas (mis. SPP kelas 1 vs kelas 6) ditangani lewat nominal per item tagihan (T30), bukan dengan membuat banyak jenis tagihan. Mengubah nominal default tidak mengubah tagihan yang sudah dibuat.
- Audit: `fee_type.create`, `fee_type.update` (hanya bila ada perubahan), `fee_type.deactivate`, `fee_type.activate`.
- Halaman `/keuangan/jenis-tagihan`: daftar untuk `bendahara`, `admin_tu`, dan `kepala_sekolah` (hanya baca); tambah, ubah, nonaktifkan, dan aktifkan untuk `bendahara` dan `admin_tu`. Peran lain `403`. Tidak ada tombol hapus. Form ubah menampilkan catatan bahwa perubahan nominal default tidak mengubah tagihan yang sudah dibuat.

### Tagihan

Tabel `invoices` dan `invoice_items` (migrasi 000016), fungsi di [invoice_service.go](internal/billing/invoice_service.go).

- Tagihan terikat ke **enrollment** (bukan siswa) per periode `YYYY-MM`, agar tagihan kelas tetap benar saat siswa pindah kelas. Foreign key komposit menjamin tahun ajaran tagihan sama dengan tahun ajaran enrollment-nya.
- Tagihan berisi satu atau lebih item. `total_amount` **dihitung** dari jumlah item di service, dan database memeriksa ulang saat commit (constraint trigger `invoice_total_matches_items`, deferred) sehingga total tidak pernah bisa berbeda dari item.
- Satu jenis tagihan per enrollment per periode (`UNIQUE (enrollment_id, fee_type_id, period)` di item): SPP September tidak bisa tertagih dua kali. Item menyimpan salinan `enrollment_id` dan `period` yang dijaga foreign key komposit ke tagihannya.
- Nominal item diambil dari default jenis tagihan, tetapi bisa diisi lain per siswa (`ItemRequest.Amount`). Nominal tersimpan di item, jadi mengubah default jenis tagihan tidak mengubah tagihan yang sudah ada. Hanya jenis tagihan aktif yang bisa dipakai.
- Jatuh tempo default: tanggal jatuh tempo jenis tagihan pada bulan periode.
- Status tersimpan hanya `belum_bayar`, `sebagian`, `lunas`. **"Terlambat" tidak disimpan**; dihitung saat ditampilkan. Tagihan bertotal 0 langsung `lunas`.
- **Status selalu dari [status.go](internal/billing/status.go)**, fungsi murni tanpa database, context, atau jam sistem:
  - `ComputeStatus(total, paid)`: 0 → `belum_bayar`; kurang dari total → `sebagian`; sama dengan total → `lunas` (0/0 juga lunas); lebih → `ErrOverpayment`; negatif → `ErrNegativeAmount`. `paid` adalah jumlah pembayaran **aktif** (belum dibatalkan).
  - `IsOverdue(status, dueDate, today)`: belum lunas dan `today` setelah jatuh tempo (hari jatuh tempo belum terlambat). `today` dari `clock.Today()` (Asia/Jakarta).
  - Pembayaran (T35), pembatalan (T39), tunggakan (T42), dan portal (T45) wajib memakai fungsi ini.
- Audit: `invoice.create`.

### Tagihan massal

Halaman `/keuangan/tagihan-massal` (hanya `bendahara`), fungsi di [bulk_service.go](internal/billing/bulk_service.go), tabel `bulk_invoice_runs` (migrasi 000019).

- Input: jenis tagihan **aktif**, periode `YYYY-MM`, satu kelas atau semua kelas tahun ajaran aktif, jatuh tempo, dan nominal (opsional, default dari jenis tagihan). Kelas dari sekolah atau tahun ajaran lain → `404`.
- Dua langkah: **Pratinjau** (hitung yang akan dibuat, dilewati, dan total nominal per kelas; tidak menyimpan apa pun), lalu **Buat N tagihan**. Tombol buat hanya ada di halaman pratinjau.
- Sasaran: enrollment aktif dengan status siswa `aktif`. Enrollment yang sudah punya item jenis dan periode yang sama dilewati, jadi menjalankan ulang tidak menghasilkan tagihan ganda (dijaga juga oleh `UNIQUE` item).
- ≤ 50 siswa: langsung dalam satu transaksi. > 50 siswa: baris run dan job River `billing_bulk_invoice` disimpan dalam transaksi yang sama, lalu halaman hasil menampilkan progres (refresh otomatis 2 detik).
- Job memproses per 25 tagihan, tiap batch dalam transaksi sendiri. Jika gagal di tengah, job diulang (maks. 5 kali) dan menghitung ulang sasaran, sehingga hasilnya tetap idempoten. Setelah percobaan terakhir gagal, run ditandai `failed`.
- Jumlah dibuat/dilewati di hasil adalah jumlah **saat eksekusi**; enrollment yang ditambahkan setelah pratinjau ikut dihitung saat itu.
- Tagihan menyimpan `bulk_run_id`. Audit: satu baris `billing.bulk_create` berisi ringkasan, tidak ada `invoice.create` per tagihan.
- Worker ada di [bulk_job.go](internal/billing/bulk_job.go) (bukan subpaket `jobs/`) karena memakai fungsi internal paket `billing`.

### Pembayaran

Tabel `payments` (migrasi 000020), paket [internal/payments](internal/payments/service.go). Form pencatatan menyusul di T35.

- **Append-only:** trigger `payments_append_only` menolak `UPDATE`, `DELETE`, dan `TRUNCATE` untuk semua user database, termasuk pemilik tabel (pola yang sama dengan `audit_logs`). Repository hanya punya insert dan `ListByInvoice`. Pembatalan (T38) dicatat di tabel terpisah tanpa mengubah baris pembayaran.
- Database menolak nominal ≤ 0, metode selain `tunai`/`transfer`, transfer tanpa rekening tujuan, serta tagihan atau pencatat dari sekolah lain (foreign key komposit).
- `RecordPayment(ctx, tx, actor, req)` berjalan di dalam transaksi pemanggil, sehingga beberapa tagihan dalam satu form batal semua jika satu gagal:
  - tagihan dikunci (`SELECT … FOR UPDATE`) agar dua pembayaran bersamaan tidak sama-sama lolos pemeriksaan sisa;
  - nominal melebihi sisa (total − pembayaran aktif) ditolak dengan `billing.ErrOverpayment`; tagihan yang dibatalkan ditolak;
  - tanggal bayar wajib dan tidak boleh setelah hari ini (Asia/Jakarta);
  - status tagihan diperbarui dengan `billing.ComputeStatus` dalam transaksi yang sama;
  - audit `payment.create` (tanpa catatan, karena bisa berisi data pribadi).
- Rekening dan nomor referensi hanya disimpan untuk transfer. `receipt_number` masih kosong sampai T41.

Halaman (hanya `bendahara`, kecuali detail tagihan):

- `/keuangan/pembayaran/baru`: cari siswa (nama atau awalan NIS, minimal 2 huruf). Alpine.js memuat hasil dari `/keuangan/pembayaran/cari?q=` (potongan HTML) saat mengetik; tanpa JavaScript tombol **Cari** tetap berfungsi. Pencarian hanya di sekolah sendiri.
- `/keuangan/pembayaran/baru?siswa={id}`: tagihan siswa yang belum lunas (semua tahun ajaran, urut jatuh tempo) dengan sisa dan tanda terlambat. Nominal default sisa tagihan dan boleh kurang (bayar sebagian). Metode tunai/transfer; rekening tujuan wajib untuk transfer, berupa teks bebas dengan saran rekening yang pernah dipakai (pengaturan rekening sekolah belum ada). Tanggal bayar default hari ini (Asia/Jakarta).
- `POST /keuangan/pembayaran`: semua tagihan yang dicentang diproses dalam **satu transaksi**; jika satu gagal (mis. melebihi sisa), tidak ada yang tersimpan dan form tampil lagi dengan pesan di tagihan tersebut. Tagihan harus milik siswa di form (selain itu `404`). Satu tagihan → redirect ke detail tagihan; beberapa → kembali ke form siswa dengan ringkasan.
- `/keuangan/tagihan/{id}`: detail tagihan (rincian, terbayar, sisa, daftar pembayaran) untuk `bendahara`, `admin_tu`, dan `kepala_sekolah` (hanya baca). Tautan "Cetak kuitansi" menyusul di T41.

### Bukti transfer

Tabel `payment_proofs` (migrasi 000021), [proof_service.go](internal/payments/proof_service.go), storage di [internal/platform/storage](internal/platform/storage/storage.go).

- Di detail tagihan, setiap pembayaran **transfer** punya form unggah (hanya `bendahara`): `POST /keuangan/pembayaran/{id}/bukti` (multipart, field `file`). Satu pembayaran boleh punya beberapa bukti.
- Maksimal **2 MB** (dicek di browser, handler, service, dan constraint database). Tipe dideteksi dari **isi file** (`http.DetectContentType`), bukan ekstensi: hanya JPEG, PNG, PDF. File `.exe` yang diganti nama menjadi `.jpg` ditolak.
- SHA-256 dihitung saat membaca. File yang sama tidak bisa dipakai dua kali **di sekolah yang sama** (`UNIQUE (school_id, sha256)`); unik per sekolah agar sekolah lain tidak bisa mengetahui keberadaan file.
- Kunci storage: `schools/{school_id}/proofs/{uuid}.{jpg|png|pdf}` (dijaga constraint). File disimpan dulu, lalu baris dan audit `payment.proof_upload` dalam satu transaksi; jika transaksi gagal, objek dihapus lagi.
- Body request dibatasi 8 MB untuk semua route (`httpserver.LimitBody`, sebelum CSRF), karena nosurf mem-parse seluruh form multipart sebelum handler.
- Kompresi gambar belum dikerjakan (ditunda).
- **Membuka bukti** (T37): `GET /keuangan/bukti/{id}` untuk `bendahara`, `admin_tu`, `kepala_sekolah` (guru dan orang tua `403`; bukti sekolah lain `404`). Setiap akses dicatat dulu di audit `payment.proof_view` (`entity_id` = id bukti), baru redirect `302` ke presigned URL yang berlaku **5 menit**. Gambar dibuka `inline`, PDF diunduh (`attachment`). URL tidak pernah disimpan atau dirender di halaman; halaman hanya menaut ke route ini. Respons redirect memakai `Cache-Control: no-store` dan `Referrer-Policy: no-referrer`.

Storage (`STORAGE_DRIVER`):

- `r2` (wajib di production): bucket Cloudflare R2 **privat** lewat AWS SDK for Go v2 (`service/s3` v1.88.4, versi terakhir untuk Go 1.22). Akses hanya lewat presigned URL (`Presign`, dipakai T37). Variabel: `R2_ACCOUNT_ID`, `R2_BUCKET`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`.
- Uji bucket sungguhan (opsional, tidak berjalan di `go test` biasa): `railway run --service akra -- env R2_LIVE_TEST=1 go test -run R2Live -v ./internal/platform/storage`. Mengunggah objek uji, memastikan akses tanpa tanda tangan ditolak, presigned URL berfungsi, URL kedaluwarsa ditolak `403`, lalu menghapus objeknya. Tidak mencetak kredensial.
- `local` (default di luar production): disk `STORAGE_LOCAL_DIR` (default `data/storage`, di-ignore git). Presign menghasilkan URL `/storage/...` bertanda tangan HMAC (kunci acak per proses) yang kedaluwarsa, meniru R2.

## Job Latar Belakang (River)

Antrean job memakai [River](https://riverqueue.com) v0.18.0 di Postgres yang sama (tanpa Redis), dengan client dan worker dalam proses server yang sama ([internal/platform/jobs](internal/platform/jobs/client.go)). Dipakai untuk tagihan massal (T32), impor (T55), dan ekspor besar (T49).

- Versi River dikunci di v0.18.0 karena versi yang lebih baru membutuhkan Go 1.23 ke atas.
- Skema River ada di migrasi `000017_river` dan `000018_river`, **dibuat dari library River** (`rivermigrate.AllVersions`), bukan ditulis manual. Dipecah dua karena `golang-migrate` menjalankan satu file dalam satu transaksi, sedangkan nilai enum `pending` (River 004) tidak boleh dipakai di transaksi yang sama dengan pembuatannya (River 006). Uji `TestRiverMigrationsMatchLibrary` memvalidasi skema dengan `rivermigrate.Validate`. Untuk upgrade River, buat migrasi baru dari versi berikutnya dengan cara yang sama.
- Worker didaftarkan di [cmd/server/workers.go](cmd/server/workers.go). Jumlah worker dari `RIVER_WORKERS`; batas lama satu job 10 menit.
- Shutdown (`SIGTERM`): server HTTP berhenti dulu, lalu River menunggu job berjalan sampai 20 detik. Job yang belum selesai dibatalkan, errornya dicatat, dan dicoba ulang otomatis.
- River memakai koneksi dari pool aplikasi ditambah satu koneksi LISTEN/NOTIFY: pastikan `DB_MAX_CONNS` cukup untuk request HTTP dan `RIVER_WORKERS`.
- Development: `curl -X POST 'localhost:8080/dev/jobs/ping?message=halo'` memasukkan `PingJob`, lalu `curl localhost:8080/dev/jobs/<id>` menampilkan statusnya. Route `/dev` hanya terpasang saat `APP_ENV=development`.

## Audit

Paket [internal/audit](internal/audit/audit.go) mencatat perubahan data ke tabel `audit_logs`:

```go
err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
	// ... ubah data ...
	return audit.Record(ctx, tx, audit.Entry{
		SchoolID: id.SchoolID, UserID: id.UserID,
		Action: "payment.void", EntityType: "payment", EntityID: paymentID,
		Before: before, After: after, IP: ip,
	})
})
```

- `Record` wajib dipanggil di **transaksi yang sama** dengan perubahan datanya, sehingga audit ikut di-rollback bila aksi gagal.
- `school_id`, `action`, `entity_type`, dan `entity_id` wajib. `user_id` kosong berarti aksi sistem.
- `Before`/`After` di-encode ke JSON. Kunci yang mengandung `password`, `token`, `secret`, atau `api_key` ditolak agar rahasia tidak masuk ke jejak audit.
- **Append-only:** trigger database menolak `UPDATE`, `DELETE`, dan `TRUNCATE` pada `audit_logs` untuk semua user, termasuk superuser.

Batasan: aplikasi dan migrasi saat ini memakai user database yang sama (`postgres`, superuser di lokal dan Railway). `REVOKE UPDATE, DELETE, TRUNCATE ... FROM PUBLIC` di migrasi tidak berpengaruh untuk pemilik tabel atau superuser, jadi perlindungan yang berlaku sekarang adalah trigger. Superuser masih bisa menonaktifkan trigger secara sengaja (`ALTER TABLE ... DISABLE TRIGGER`); itu tindakan administrasi database, bukan lewat aplikasi. Untuk production, sebaiknya aplikasi memakai role terpisah tanpa hak DDL. Migrasi `down` untuk tabel ini menghapus seluruh jejak audit, jadi hanya untuk development.

## Deploy (Railway)

### Lingkungan staging

| Item | Nilai |
|---|---|
| Project Railway | `natural-compassion` (environment Railway `production`, dipakai sebagai **staging** dengan `APP_ENV=staging`) |
| Service | `akra` (dari repo GitHub, branch `main`, auto-deploy) dan `Postgres` (image `postgres-ssl:18`) |
| URL | https://akra-production.up.railway.app |
| Region | Singapura (`asia-southeast1-eqsg3a`), region Railway terdekat dengan Indonesia. Kebutuhan lokasi data di Indonesia dikonfirmasi ke calon sekolah sebelum pilot (risiko R11) |

Environment `production` yang sebenarnya dibuat setelah uji internal MVP 1 (T63).

### Image

[Dockerfile](Dockerfile) multi-stage: build dengan `golang:1.22.2-alpine`, runtime `alpine:3.20` sebagai user non-root `akra` (uid 10001). Alpine dipakai (bukan distroless) agar pre-deploy command punya shell. Isi image:

| Path | Fungsi |
|---|---|
| `/usr/local/bin/akra` | server (CMD) |
| `/usr/local/bin/akra-seed` | membuat sekolah dan pengguna awal ([cmd/seed](cmd/seed/main.go)) |
| `/usr/local/bin/akra-migrate` | pembungkus `migrate` untuk `$DATABASE_URL` ([scripts/akra-migrate](scripts/akra-migrate)) |
| `/usr/local/bin/migrate` | CLI golang-migrate v4.20.1 (checksum diverifikasi saat build) |
| `/app/db/migrations` | file migrasi |

```sh
docker build -t akra .
docker run --rm --network host -e DATABASE_URL=postgres://... akra
```

### Variabel service `akra`

| Variabel | Nilai |
|---|---|
| `APP_ENV` | `staging` |
| `DATABASE_URL` | `${{Postgres.DATABASE_URL}}` (referensi; host internal `postgres.railway.internal`) |
| `APP_BASE_URL` | `https://${{RAILWAY_PUBLIC_DOMAIN}}` |
| `MAIL_DRIVER` | `log` (staging; tautan reset ada di log Railway) |
| `CLIENT_IP_HEADER` | `X-Real-IP` |
| `STORAGE_DRIVER` | belum diisi → `local` (disk container, **hilang saat redeploy**); ganti ke `r2` setelah bucket R2 dibuat |

`PORT` diisi Railway otomatis; target port domain `8080`.

### Setting service (diisi di Settings Railway, bukan di repo)

`railway.json` (Config as Code) sudah deprecated dan tidak dibaca untuk service ini, jadi setting berikut diisi di service:

| Setting | Nilai |
|---|---|
| Builder | Dockerfile (`Dockerfile` di root) |
| Pre-deploy command | `akra-migrate up` |
| Healthcheck path | `/healthz` (timeout 60 detik) |
| Restart policy | On failure, maksimal 10 kali |

### Migrasi

Migrasi dijalankan otomatis oleh **pre-deploy command** `akra-migrate up` sebelum versi baru menerima traffic. Jika migrasi gagal, deploy dibatalkan dan versi lama tetap berjalan. Cek versi dari laptop:

```sh
railway ssh --service akra -- akra-migrate version
```

Setting service hanya berlaku untuk deployment **baru**; `railway redeploy` memakai ulang setting deployment lama.

### Pengguna awal

Dibuat dengan `akra-seed` di dalam container, bukan SQL manual. Tanpa `AKRA_SEED_PASSWORD`, password acak dicetak sekali:

```sh
railway ssh --service akra -- akra-seed -school-code akra-demo -school-name "'Sekolah Demo AKRA'" \
  -email admin@akra-demo.id -name "'Admin Demo'"
```

Staging berisi sekolah `akra-demo` dengan admin `admin@akra-demo.id` (peran `admin_tu`).

## Aturan Code Review

- `school_id` untuk data sekolah **selalu** diambil dari `auth.CurrentUser(ctx)`. Handler tidak boleh membaca `school_id` dari `r.URL.Query()`, `chi.URLParam`, atau form.
- Handler tidak membaca sesi langsung; gunakan `auth.CurrentUser(ctx)`.
- Setiap route baru wajib menyebut peran yang diizinkan di komentar di atas handler, misalnya `// Peran: bendahara.`
- Setiap route baru wajib punya uji otorisasi (lihat T58 dan T59).
- Setiap perubahan skema wajib punya migrasi `down`.

## Migrasi Database

Migrasi ada di `db/migrations/` dengan format `000001_nama.up.sql` dan `000001_nama.down.sql`. `DATABASE_URL` diambil dari environment.

| Perintah | Fungsi |
|---|---|
| `make migrate-up` | Menjalankan semua migrasi yang belum dijalankan |
| `make migrate-down` | Membatalkan satu migrasi terakhir |
| `make migrate-create name=add_x` | Membuat pasangan file migrasi dengan nomor berikutnya |

Aturan tim:

- Setiap perubahan skema wajib punya file `down` yang mengembalikan kondisi sebelumnya.
- Migrasi yang sudah dijalankan di staging atau production tidak boleh diedit. Buat migrasi baru.

## Pengujian

```sh
go test ./...
```

Uji yang butuh database hanya berjalan jika `TEST_DATABASE_URL` diisi:

```sh
TEST_DATABASE_URL=postgres://akra:akra@localhost:5432/akra?sslmode=disable go test ./...
```

Catatan: dependensi dipilih agar tetap kompatibel dengan Go 1.22. Jika `go get` mencoba menaikkan versi Go, jalankan dengan `GOTOOLCHAIN=local`.
