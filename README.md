# AKRA

AKRA - One Platform for Every School.

Sistem manajemen sekolah (SchoolOS) untuk sekolah swasta di Indonesia. Rancangan ada di [doc/software-architecture.md](doc/software-architecture.md), rencana MVP di [doc/rencana-project.md](doc/rencana-project.md), dan daftar task di [doc/tasks_schoolos/00-index.md](doc/tasks_schoolos/00-index.md).

## Prasyarat

- Go 1.22.2 atau lebih baru

## Menjalankan

```sh
go run ./cmd/server
```

Build binary:

```sh
go build -o bin/server ./cmd/server
```

Konfigurasi environment dan database akan ditambahkan di T02 dan T03.
