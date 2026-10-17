# AKRA

AKRA - One Platform for Every School.

Sistem manajemen sekolah (SchoolOS) untuk sekolah swasta di Indonesia. Rancangan ada di [doc/software-architecture.md](doc/software-architecture.md), rencana MVP di [doc/rencana-project.md](doc/rencana-project.md), dan daftar task di [doc/tasks_schoolos/00-index.md](doc/tasks_schoolos/00-index.md).

## Prasyarat

- Go 1.22.2 atau lebih baru

## Menjalankan

```sh
DATABASE_URL=postgres://localhost/akra go run ./cmd/server
```

Cek server:

```sh
curl -i localhost:8080/healthz
```

Build binary:

```sh
go build -o bin/server ./cmd/server
```

## Konfigurasi

| Variabel | Wajib | Default | Keterangan |
|---|---|---|---|
| `DATABASE_URL` | ya | | Connection string PostgreSQL (koneksi dibuat di T03) |
| `PORT` | tidak | `8080` | Port HTTP |
| `APP_ENV` | tidak | `development` | `development`, `staging`, atau `production` |

## Pengujian

```sh
go test ./...
```

Catatan: dependensi dipilih agar tetap kompatibel dengan Go 1.22. Jika `go get` mencoba menaikkan versi Go, jalankan dengan `GOTOOLCHAIN=local`.
