// Command pgfleet-api is the PgFleet control-plane HTTP server.
package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sagoresarker/pgfleet/internal/api"
	"github.com/sagoresarker/pgfleet/internal/audit"
	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/backup"
	"github.com/sagoresarker/pgfleet/internal/bootstrap"
	"github.com/sagoresarker/pgfleet/internal/config"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/logging"
	"github.com/sagoresarker/pgfleet/internal/objectstore"
	"github.com/sagoresarker/pgfleet/internal/provision"
	"github.com/sagoresarker/pgfleet/internal/reconcile"
	"github.com/sagoresarker/pgfleet/internal/scheduler"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/store"
	"github.com/sagoresarker/pgfleet/internal/user"
	"github.com/sagoresarker/pgfleet/internal/version"
	"github.com/sagoresarker/pgfleet/internal/ws"
)

// tokenTTL is the lifetime of issued session tokens.
const tokenTTL = 24 * time.Hour

// reconcileInterval is how often the control plane reconciles instance state
// against Docker.
const reconcileInterval = 30 * time.Second

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString("fatal: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	log := logging.New(cfg.LogLevel, os.Stdout)
	log.Info("starting pgfleet-api", "version", version.String())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := store.Open(ctx, cfg.MetaDBDSN)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := store.MigrateUp(ctx, cfg.MetaDBDSN); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return err
	}

	issuer := auth.NewIssuer([]byte(cfg.JWTSecret), tokenTTL)
	users := user.NewRepository(pool)
	recorder := audit.NewRecorder(pool)

	if created, berr := bootstrap.EnsureAdmin(ctx, users, cfg.BootstrapAdminEmail, cfg.BootstrapAdminPassword); berr != nil {
		return berr
	} else if created {
		log.Info("bootstrapped initial admin user", "email", cfg.BootstrapAdminEmail)
	}

	// Instance orchestration: secrets cipher, Docker runtime, provisioner.
	cipher, err := secrets.New(cfg.MasterKey)
	if err != nil {
		return err
	}
	rt, err := docker.NewMoby()
	if err != nil {
		return err
	}
	defer rt.Close()

	if _, nerr := rt.CreateNetwork(ctx, cfg.DockerNetwork, map[string]string{docker.LabelManaged: "true"}); nerr != nil {
		log.Info("docker network ensure (may already exist)", "network", cfg.DockerNetwork, "note", nerr.Error())
	}

	s3 := objectstore.Config{
		Endpoint:  cfg.S3Endpoint,
		Region:    cfg.S3Region,
		AccessKey: cfg.S3AccessKey,
		SecretKey: cfg.S3SecretKey,
		Bucket:    cfg.S3Bucket,
	}
	if cfg.S3Endpoint != "" && cfg.S3Bucket != "" {
		if berr := objectstore.EnsureBucket(ctx, s3); berr != nil {
			return berr
		}
		log.Info("backup bucket ensured", "bucket", cfg.S3Bucket)
	}

	instances := instance.NewRepository(pool, cipher)
	provisioner := provision.New(rt, instances, provision.Options{
		Network:      cfg.DockerNetwork,
		InstanceHost: cfg.InstanceHost,
		S3:           s3,
	})
	hub := ws.NewHub()

	// Reconcile on boot and on a loop so the control plane is not amnesiac
	// after a restart.
	reconciler := reconcile.New(rt, instances, log)
	if rerr := reconciler.Reconcile(ctx); rerr != nil {
		log.Warn("initial reconciliation failed", "err", rerr)
	}
	backups := backup.New(rt, instances, backup.NewCatalog(pool))

	sched := scheduler.New(scheduler.WithErrorHandler(func(name string, err error) {
		log.Warn("scheduled job failed", "job", name, "err", err)
	}))
	sched.Register("reconcile", reconcileInterval, reconciler.Reconcile)
	sched.Register("scheduled-backups", cfg.BackupInterval, func(ctx context.Context) error {
		return backups.RunScheduled(ctx, instances, cfg.BackupType)
	})
	sched.Start(ctx)
	defer sched.Stop()

	router := api.NewRouter(api.Deps{
		Ready:     store.Ready(pool),
		Issuer:    issuer,
		Auth:      api.NewAuthHandler(users, issuer, recorder),
		Users:     api.NewUsersHandler(users, recorder),
		Instances: api.NewInstancesHandler(instances, provisioner, hub).WithAudit(recorder),
		Backups:   api.NewBackupsHandler(backups, provisioner, recorder),
		Events:    hub,
	})

	return api.Serve(ctx, ln, router, log)
}
