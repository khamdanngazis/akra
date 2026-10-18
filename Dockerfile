# syntax=docker/dockerfile:1

# ---- Build ----
FROM golang:1.22.2-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/akra ./cmd/server && \
    go build -trimpath -ldflags="-s -w" -o /out/akra-seed ./cmd/seed && \
    go build -trimpath -ldflags="-s -w" -o /out/akra-demoseed ./cmd/demoseed

# ---- CLI golang-migrate (versi sama dengan README) ----
FROM alpine:3.20 AS migrate
ARG MIGRATE_VERSION=v4.20.1
ARG MIGRATE_SHA256=b1e13121d4178b7804a9fb29dca62d57b732390f7e5ae33f2cd65e9b192e1bd4
RUN wget -q -O /tmp/migrate.tar.gz \
      "https://github.com/golang-migrate/migrate/releases/download/${MIGRATE_VERSION}/migrate.linux-amd64.tar.gz" && \
    echo "${MIGRATE_SHA256}  /tmp/migrate.tar.gz" | sha256sum -c - && \
    tar -xzf /tmp/migrate.tar.gz -C /usr/local/bin migrate

# ---- Runtime ----
# Alpine (bukan distroless) supaya pre-deploy command Railway punya shell
# untuk membaca $DATABASE_URL.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -H -u 10001 akra
COPY --from=build /out/akra /out/akra-seed /out/akra-demoseed /usr/local/bin/
COPY --from=migrate /usr/local/bin/migrate /usr/local/bin/migrate
COPY --chmod=0755 scripts/akra-migrate /usr/local/bin/akra-migrate
COPY db/migrations /app/db/migrations
ENV PORT=8080
EXPOSE 8080
USER akra
# CMD, bukan ENTRYPOINT: pre-deploy command Railway menggantikan CMD, sedangkan
# ENTRYPOINT akan menjadikannya argumen server.
CMD ["/usr/local/bin/akra"]
