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

Middleware `auth.RequireLogin` mengalihkan request tanpa sesi ke `/login?next=<path>`. Parameter `next` hanya menerima path lokal (diawali `/`, bukan `//` atau URL absolut), selain itu diganti `/`. Route publik hanya `/healthz`, `/healthz/db`, `/login`, `/logout`, dan `/static/`. Semua route baru dipasang di grup `RequireLogin` di [cmd/server/routes.go](cmd/server/routes.go).

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
- Index `(school_id, full_name)` membantu daftar urut nama. Untuk pencarian "mengandung", index ini tidak terpakai; bila nanti lambat, pertimbangkan `pg_trgm` (lihat T22).

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
