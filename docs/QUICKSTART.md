# Quickstart — run PgFleet locally

PgFleet's control plane manages Postgres **as sibling Docker containers**, so it
runs most smoothly as a binary on a host with Docker access, alongside its
dependencies (a meta database + MinIO) in containers. This gets you a working
console with a real provisioned instance in a couple of minutes.

## Prerequisites

- Docker (Desktop or Engine) running
- Go 1.25+ and Node 20+ (only to build/run from source)

## 1. Start the dependencies

```bash
make dev-up        # meta-db (Postgres) on :5432 + MinIO on :9000/:9001
```

## 2. Build the managed-instance image

This is the `postgres:16 + pgBackRest` image every instance runs from.

```bash
make image         # builds pgfleet/postgres-pgbackrest:16
```

## 3. Configure and run the control plane

```bash
cp .env.example .env        # then edit secrets if you like
make run                    # builds + runs the API on :8080
```

On first boot it migrates the meta DB, ensures the MinIO backup bucket, and
seeds the admin user from `.env`. You should see:

```
http server listening  addr=[::]:8080
```

Check it: `curl localhost:8080/healthz` → `{"status":"ok"}`.

## 4. Start the web console

```bash
cd web
npm install
npm run dev                 # http://localhost:3000  (proxies /api to :8080)
```

Open <http://localhost:3000>, click **Open the console**, and sign in with the
`PGFLEET_BOOTSTRAP_ADMIN_*` credentials from your `.env`.

## 5. Provision your first instance

From the console: **Instances → New instance**, pick a name, choose **Local
volume** (or S3/MinIO), set a password, and create. Watch it go
`provisioning → running`, then reveal its connection string and connect with
`psql`.

For a highly-available setup, use **Clusters → New cluster** with one or more
replicas — you get a primary, streaming replicas, and a PgCat router that
splits reads and writes.

---

## One-liner script

```bash
make dev-up && make image && cp -n .env.example .env && make run
```

(then `cd web && npm install && npm run dev` in another terminal)

## Try it without the UI (API only)

```bash
TOKEN=$(curl -s localhost:8080/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@pgfleet.local","password":"change-me-please"}' | jq -r .token)

curl -s -X POST localhost:8080/api/v1/instances -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"orders-db","repo_type":"local","password":"a-good-password"}'

curl -s localhost:8080/api/v1/instances -H "Authorization: Bearer $TOKEN" | jq
```

## Production / Kubernetes

The repo ships container images for both services:

- `Dockerfile` — the control-plane API (distroless). It needs the host Docker
  socket mounted (`/var/run/docker.sock`) to manage instance containers, and
  `PGFLEET_INSTANCE_HOST` set to an address clients (and the control plane's
  own collectors) can reach the published instance ports on.
- `web/Dockerfile` — the Next.js console (standalone). Set `PGFLEET_API_URL` to
  the control-plane URL.

Build them with `docker build -t pgfleet/api .` and
`docker build -t pgfleet/web web`. See `deploy/docker-compose.yml` for the
dependency services.
