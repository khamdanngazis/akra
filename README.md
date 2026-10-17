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

Server tidak akan start jika database tidak bisa dijangkau dalam 5 detik.

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
