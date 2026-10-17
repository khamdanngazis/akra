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
go build -o bin/server ./cmd/server
```

## Konfigurasi

| Variabel | Wajib | Default | Keterangan |
|---|---|---|---|
| `DATABASE_URL` | ya | | Connection string PostgreSQL |
| `DB_MAX_CONNS` | tidak | `10` | Batas koneksi pool (`MinConns` = 2) |
| `PORT` | tidak | `8080` | Port HTTP |
| `APP_ENV` | tidak | `development` | `development`, `staging`, atau `production` |
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
- Tidak ada aset dari CDN; semua dimuat dari `/static/`. Uji `TestTemplates_NoExternalResources` gagal jika template atau CSS memuat URL eksternal.
- Pesan sekali tampil memakai `auth.SetFlash` / `auth.PopFlash` (disimpan di sesi), bukan query string.
- Tabel lebar dibungkus `<div class="table-wrap">` agar halaman tidak scroll horizontal di layar 360px.

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
