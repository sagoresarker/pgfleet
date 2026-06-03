# Wiring the Remote Backup & Restore ("migrate-in") feature

This feature is implemented by:

- `internal/remotebackup/` — capture/catalog/restore engine (`*remotebackup.Service`,
  `*remotebackup.Repository`, `*remotebackup.ObjectStoreAdapter`).
- `internal/api/remote.go` — HTTP handlers (`*api.RemoteHandler`).
- migration `internal/store/migrations/00016_remote_sources.sql` (tables
  `remote_dumps` + `remote_source_secrets`).

The handler depends on two collaborators it does NOT own:

- `api.RemoteService` — satisfied directly by `*remotebackup.Service`.
- `api.RemoteTargetProvisioner` — an adapter over the existing instance/cluster
  provisioning. A complete, drop-in implementation is given below.

`cmd/pgfleet-api/main.go` is NOT edited by the feature worker; the parent wires it
using the snippet below.

---

## 1. Add a `Remote` field to `api.Deps` (router.go) and mount the routes

The router currently has no `Remote` field. Add one to `Deps` and mount the
routes (RBAC-gated). Capture/restore are write-level operations that create new
managed targets, so they are gated at `auth.ActionInstanceWrite`; the list is a
read at `auth.ActionInstanceRead`.

```go
// in Deps:
// Remote serves the migrate-in (remote backup & restore) endpoints (optional).
Remote *RemoteHandler

// in NewRouter, inside the authenticated pr group:
if deps.Remote != nil {
    mountRemoteRoutes(pr, deps.Remote)
}

// new helper next to mountBackupRoutes:
func mountRemoteRoutes(pr chi.Router, h *RemoteHandler) {
    pr.Group(func(rr chi.Router) {
        rr.Use(auth.RequireAction(auth.ActionInstanceRead))
        rr.Get("/remote/backups", h.List)
    })
    pr.Group(func(wr chi.Router) {
        // Capturing a remote dump and restoring it into a freshly provisioned
        // target both create/own managed resources, so they require the same
        // write privilege as creating an instance/cluster.
        wr.Use(auth.RequireAction(auth.ActionInstanceWrite))
        wr.Post("/remote/backups", h.Capture)
        wr.Post("/remote/backups/{id}/restore", h.Restore)
    })
}
```

### HTTP routes (all under `/api/v1`, all bearer-authenticated)

| Method | Path                              | RBAC Action            | Description                                  |
|--------|-----------------------------------|------------------------|----------------------------------------------|
| POST   | `/remote/backups`                 | `instance.write`       | Connect to a remote PG, pg_dump → store, catalog. Returns the catalog entry (no password). |
| GET    | `/remote/backups`                 | `instance.read`        | List captured remote dumps (password-free).  |
| POST   | `/remote/backups/{id}/restore`    | `instance.write`       | Provision a fresh instance/cluster and restore the dump into it. 202 + `{target,id}`. |

---

## 2. Construct the service + handler in `run(...)` (main.go)

Place this AFTER `instances`, `provisioner`, `clusters`, `clusterSvc`, `cipher`,
`s3`, and `async` are constructed (they already exist around the
`api.NewRouter(...)` call). It only CALLS exported APIs of other packages.

```go
// --- Remote backup & restore ("migrate-in") ---
// remotebackup persists its catalog/sealed source secrets in the meta DB and
// streams captured dumps to the same object store as the rest of the fleet.
remoteRepo := remotebackup.NewRepository(pool, cipher)
remoteStore := remotebackup.NewObjectStore(s3)
remoteSvc := remotebackup.New(remoteStore, remoteRepo)
remoteProv := newRemoteTargetProvisioner(instances, provisioner, clusterSvc, clusters, cfg.InstanceHost)
remoteHandler := api.NewRemoteHandler(remoteSvc, remoteProv).WithAudit(recorder).WithAsync(async)
```

Then add to the `api.Deps{...}` literal:

```go
Remote: remoteHandler,
```

Add these imports to main.go if not already present:

```go
"github.com/sagoresarker/pgfleet/internal/remotebackup"
```

---

## 3. The `RemoteTargetProvisioner` adapter (drop into main.go or a new
`cmd/pgfleet-api/remote_target.go`)

This bridges `api.RemoteTargetProvisioner` to the existing provisioning. It:

- creates an instance/cluster record up front (so the handler can return its id),
- runs the (synchronous) provisioner in `WaitReady` and resolves the fresh
  superuser DSN once the target is running,
- for a cluster, restores into the PRIMARY instance's DSN (not the router) so
  pg_restore talks straight to Postgres,
- marks the target errored on failure.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/sagoresarker/pgfleet/internal/api"
    "github.com/sagoresarker/pgfleet/internal/cluster"
    "github.com/sagoresarker/pgfleet/internal/clusterctl"
    "github.com/sagoresarker/pgfleet/internal/instance"
    "github.com/sagoresarker/pgfleet/internal/provision"
)

// remoteTargetProvisioner adapts the existing instance/cluster provisioning to
// the api.RemoteTargetProvisioner interface used by the migrate-in restore flow.
type remoteTargetProvisioner struct {
    instances  *instance.Repository
    prov       *provision.Provisioner
    clusterSvc *clusterctl.Service
    clusters   *cluster.Repository
    host       string
}

func newRemoteTargetProvisioner(
    instances *instance.Repository,
    prov *provision.Provisioner,
    clusterSvc *clusterctl.Service,
    clusters *cluster.Repository,
    host string,
) *remoteTargetProvisioner {
    return &remoteTargetProvisioner{instances: instances, prov: prov, clusterSvc: clusterSvc, clusters: clusters, host: host}
}

// ProvisionInstance creates the instance record (status: provisioning) and
// returns its id. Actual container provisioning happens in WaitReady so the
// handler can return the id immediately.
func (a *remoteTargetProvisioner) ProvisionInstance(ctx context.Context, spec api.RemoteTargetSpec) (string, error) {
    inst, err := a.instances.Create(ctx, instance.NewInstance{
        Name:       spec.Name,
        PGVersion:  spec.PGVersion,
        RepoType:   instance.RepoType(spec.RepoType),
        Password:   spec.Password,
        Parameters: spec.Parameters,
        Extensions: spec.Extensions,
    })
    if err != nil {
        return "", err
    }
    return inst.ID, nil
}

// ProvisionCluster creates the cluster record (and its member records) and
// returns the cluster id. Provisioning happens in WaitReady.
func (a *remoteTargetProvisioner) ProvisionCluster(ctx context.Context, spec api.RemoteTargetSpec) (string, error) {
    c, err := a.clusterSvc.Create(ctx, clusterctl.Input{
        Name:       spec.Name,
        Replicas:   spec.Replicas,
        Password:   spec.Password,
        RepoType:   instance.RepoType(spec.RepoType),
        Version:    spec.PGVersion,
        Parameters: spec.Parameters,
        Extensions: spec.Extensions,
    })
    if err != nil {
        return "", err
    }
    return c.ID, nil
}

// WaitReady runs the (synchronous) provisioner and, on success, returns the
// superuser DSN to restore into. provision.Provision / clusterctl.Provision set
// the target's status to running on success and error on failure, so no extra
// polling is needed.
func (a *remoteTargetProvisioner) WaitReady(ctx context.Context, kind, id string) (string, error) {
    switch kind {
    case "instance":
        if err := a.prov.Provision(ctx, id, nil); err != nil {
            return "", err
        }
        return a.prov.DSN(ctx, id)
    case "cluster":
        if err := a.clusterSvc.Provision(ctx, id, nil); err != nil {
            return "", err
        }
        // Restore into the PRIMARY instance directly (not the router), so
        // pg_restore speaks straight Postgres.
        c, err := a.clusters.Get(ctx, id)
        if err != nil {
            return "", err
        }
        if c.PrimaryInstanceID == "" {
            return "", fmt.Errorf("cluster %s has no primary after provisioning", id)
        }
        return a.prov.DSN(ctx, c.PrimaryInstanceID)
    default:
        return "", fmt.Errorf("unknown target kind %q", kind)
    }
}

// MarkError records that the migrate-in restore failed so the target is not left
// silently half-built.
func (a *remoteTargetProvisioner) MarkError(ctx context.Context, kind, id, reason string) {
    // Bound the status write so a cancelled request context cannot wedge it.
    ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
    defer cancel()
    switch kind {
    case "instance":
        _ = a.instances.SetStatus(ctx, id, instance.StatusError, reason)
    case "cluster":
        _ = a.clusters.SetStatus(ctx, id, cluster.StatusError, reason)
    }
}
```

---

## Notes / honesty

- The adapter above is the parent's integration glue and lives in `cmd/`, which
  the feature worker does not touch; it is therefore NOT covered by the feature
  worker's unit tests. The `api.RemoteTargetProvisioner` *interface* and the
  handler's use of it (provision → wait → restore → mark-error) ARE unit-tested
  in `internal/api/remote_test.go` with a fake.
- `WaitReady` runs the synchronous provisioner; for a real deployment you may
  prefer to launch provisioning eagerly in `ProvisionInstance`/`ProvisionCluster`
  and poll status in `WaitReady`. Both approaches satisfy the interface; the
  synchronous form is simpler and is what the snippet uses.
- A logical `pg_restore` of a custom-format dump replays into the primary; the
  replica(s) receive the data via streaming replication, so no separate restore
  per replica is needed.
