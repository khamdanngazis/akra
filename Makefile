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

# Tailwind CSS standalone CLI (binary, tanpa Node). Versi dan checksum dikunci;
# binary diunduh ke bin/ (di-ignore git).
TAILWIND_VERSION := v4.3.3
TAILWIND_SHA256 := dc61b3ac6b8c9ca874c0cc4c57b2409791a64c5540404ca5f5367360babc313a
TAILWIND := bin/tailwindcss
CSS_SRC := web/tailwind/app.css
CSS_OUT := web/static/css/app.css

.PHONY: css css-watch css-check

$(TAILWIND):
	@mkdir -p bin
	curl -sSfL -o $(TAILWIND).tmp https://github.com/tailwindlabs/tailwindcss/releases/download/$(TAILWIND_VERSION)/tailwindcss-linux-x64
	@echo "$(TAILWIND_SHA256)  $(TAILWIND).tmp" | sha256sum -c - >/dev/null || (rm -f $(TAILWIND).tmp; echo "tailwindcss checksum mismatch" >&2; exit 1)
	@chmod +x $(TAILWIND).tmp && mv $(TAILWIND).tmp $(TAILWIND)

## css: build web/static/css/app.css dari web/tailwind/app.css (hasil di-commit)
css: $(TAILWIND)
	@$(TAILWIND) -i $(CSS_SRC) -o $(CSS_OUT) --minify

## css-watch: build ulang CSS setiap template atau sumber CSS berubah
css-watch: $(TAILWIND)
	$(TAILWIND) -i $(CSS_SRC) -o $(CSS_OUT) --watch

## css-check: gagal jika app.css yang di-commit tidak sesuai sumbernya
css-check: $(TAILWIND)
	@$(TAILWIND) -i $(CSS_SRC) -o bin/app.check.css --minify 2>/dev/null
	@cmp -s bin/app.check.css $(CSS_OUT) || (echo "web/static/css/app.css is stale: run make css" >&2; exit 1)
