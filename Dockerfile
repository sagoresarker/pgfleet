# PgFleet control-plane API.
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /pgfleet-api ./cmd/pgfleet-api

# Runs as root: the control plane talks to the mounted Docker socket to manage
# instance containers. debian-slim (not distroless) so the control plane can
# shell out to pg_dump/pg_restore to back up + restore its own meta DB (Area
# 4.2). postgresql-client-17 dumps any PG 13-17 server (client >= server).
FROM debian:bookworm-slim
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates gnupg wget; \
    wget --quiet -O /etc/apt/trusted.gpg.d/pgdg.asc https://www.postgresql.org/media/keys/ACCC4CF8.asc; \
    echo "deb https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" > /etc/apt/sources.list.d/pgdg.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends postgresql-client-17; \
    apt-get purge -y gnupg wget; apt-get autoremove -y; \
    rm -rf /var/lib/apt/lists/*
COPY --from=build /pgfleet-api /pgfleet-api
EXPOSE 8080
ENTRYPOINT ["/pgfleet-api"]
