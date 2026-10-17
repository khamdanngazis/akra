MIGRATIONS_DIR := db/migrations

.PHONY: migrate-up migrate-down migrate-create check-db-url

## migrate-up: apply all pending migrations
migrate-up: check-db-url
	@migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

## migrate-down: roll back the most recent migration
migrate-down: check-db-url
	@migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1

## migrate-create name=<name>: create the next numbered up/down migration pair
migrate-create:
	@test -n "$(name)" || (echo "usage: make migrate-create name=<name>" >&2; exit 1)
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq -digits 6 $(name)

# DATABASE_URL must come from the environment; it is never hard-coded here.
# migrate commands are not echoed so the password in DATABASE_URL stays out of logs.
check-db-url:
	@test -n "$(DATABASE_URL)" || (echo "DATABASE_URL is not set" >&2; exit 1)
