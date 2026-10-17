MIGRATIONS_DIR := db/migrations

.PHONY: migrate-up migrate-down migrate-create check-db-url

## migrate-up: jalankan semua migrasi yang belum dijalankan
migrate-up: check-db-url
	@migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

## migrate-down: batalkan satu migrasi terakhir
migrate-down: check-db-url
	@migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1

## migrate-create name=<nama>: buat pasangan file up/down dengan nomor berikutnya
migrate-create:
	@test -n "$(name)" || (echo "usage: make migrate-create name=<name>" >&2; exit 1)
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq -digits 6 $(name)

# DATABASE_URL wajib dari environment dan tidak pernah ditulis di sini.
# Perintah migrate tidak ditampilkan agar password di DATABASE_URL tidak masuk log.
check-db-url:
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is not set" >&2; exit 1)
