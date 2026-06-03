# PgFleet control-plane API.
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /pgfleet-api ./cmd/pgfleet-api

# Runs as root: the control plane talks to the mounted Docker socket to manage
# instance containers.
FROM gcr.io/distroless/static-debian12
COPY --from=build /pgfleet-api /pgfleet-api
EXPOSE 8080
ENTRYPOINT ["/pgfleet-api"]
