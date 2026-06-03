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
	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/clusterctl"
	"github.com/sagoresarker/pgfleet/internal/config"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/health"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/logging"
	"github.com/sagoresarker/pgfleet/internal/metrics"
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

// metricsInterval is how often instance statistics are collected.
const metricsInterval = 15 * time.Second

// metricsRetention is how long metric samples are kept.
const metricsRetention = 7 * 24 * time.Hour

// healthInterval is how often instance health is assessed.
const healthInterval = 5 * time.Minute

// drillInterval is how often a restore drill is performed per instance.
const drillInterval = 24 * time.Hour

// asyncDrainTimeout bounds how long shutdown waits for in-flight provisioning.
const asyncDrainTimeout = 60 * time.Second

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

	issuer, err := auth.NewIssuer([]byte(cfg.JWTSecret), tokenTTL)
	if err != nil {
		return err
	}
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
		Network:       cfg.DockerNetwork,
		InstanceHost:  cfg.InstanceHost,
		S3:            s3,
		RestartPolicy: cfg.InstanceRestartPolicy,
	})
	clusters := cluster.NewRepository(pool)
	clusterSvc := clusterctl.New(clusters, instances, provisioner, rt, instance.RepoType(cfg.DefaultRepoType))
	hub := ws.NewHub()

	// Reconcile on boot and on a loop so the control plane is not amnesiac
	// after a restart.
	reconciler := reconcile.New(rt, instances, log)
	if rerr := reconciler.Reconcile(ctx); rerr != nil {
		log.Warn("initial reconciliation failed", "err", rerr)
	}
	catalog := backup.NewCatalog(pool)
	backups := backup.New(rt, instances, catalog)
	metricStore := metrics.NewStore(pool)
	collector := metrics.NewCollector()
	healthStore := health.NewStore(pool)
	healthChecker := health.NewChecker(rt, instances, catalog, health.DefaultThresholds())

	sched := scheduler.New(scheduler.WithErrorHandler(func(name string, err error) {
		log.Warn("scheduled job failed", "job", name, "err", err)
	}))
	sched.Register("reconcile", reconcileInterval, reconciler.Reconcile)
	sched.Register("scheduled-backups", cfg.BackupInterval, func(ctx context.Context) error {
		return backups.RunScheduled(ctx, instances, cfg.BackupType)
	})
	sched.Register("collect-metrics", metricsInterval, func(ctx context.Context) error {
		return collectMetrics(ctx, instances, provisioner, collector, metricStore)
	})
	sched.Register("prune-metrics", time.Hour, func(ctx context.Context) error {
		return metricStore.Prune(ctx, time.Now().Add(-metricsRetention))
	})
	sched.Register("health-checks", healthInterval, func(ctx context.Context) error {
		return runHealthChecks(ctx, instances, healthChecker, healthStore)
	})
	sched.Register("restore-drills", drillInterval, func(ctx context.Context) error {
		return runRestoreDrills(ctx, instances, provisioner, healthStore)
	})
	sched.Start(ctx)
	defer sched.Stop()

	insights := func(ctx context.Context, instanceID string, limit int) ([]metrics.QueryStat, error) {
		dsn, err := provisioner.DSN(ctx, instanceID)
		if err != nil {
			return nil, err
		}
		return collector.TopQueries(ctx, dsn, limit)
	}

	// Track async provisioning/restore work so a graceful shutdown drains it
	// before the pool/runtime close (otherwise instances/clusters can wedge in
	// "provisioning").
	async := api.NewAsync(context.Background())

	router := api.NewRouter(api.Deps{
		Ready:     store.Ready(pool),
		Issuer:    issuer,
		Auth:      api.NewAuthHandler(users, issuer, recorder),
		Users:     api.NewUsersHandler(users, recorder),
		Instances: api.NewInstancesHandler(instances, provisioner, hub).WithAudit(recorder).WithAsync(async),
		Clusters:  api.NewClustersHandler(clusterSvc, clusters, instances, cfg.InstanceHost, recorder).WithAsync(async),
		Backups:   api.NewBackupsHandler(backups, provisioner, recorder).WithAsync(async),
		Metrics:   api.NewMetricsHandler(metricStore, insights),
		Health:    api.NewHealthHandler(healthStore),
		Events:    hub,
	})

	serveErr := api.Serve(ctx, ln, router, log)
	log.Info("draining in-flight provisioning work")
	async.Wait(asyncDrainTimeout)
	return serveErr
}

// runHealthChecks assesses every instance and stores the reports.
func runHealthChecks(ctx context.Context, lister *instance.Repository, checker *health.Checker, store *health.Store) error {
	instances, err := lister.List(ctx)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		if inst.Status != instance.StatusRunning {
			continue
		}
		report, err := checker.Check(ctx, inst.ID)
		if err != nil {
			continue
		}
		_ = store.Upsert(ctx, report)
	}
	return nil
}

// runRestoreDrills performs a restore drill per running instance and records
// the outcome on its health report.
func runRestoreDrills(ctx context.Context, lister *instance.Repository, prov *provision.Provisioner, store *health.Store) error {
	instances, err := lister.List(ctx)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		if inst.Status != instance.StatusRunning {
			continue
		}
		result, err := prov.RestoreDrill(ctx, inst.ID)
		if err != nil {
			continue
		}
		_ = store.UpdateDrill(ctx, inst.ID, result.OK)
	}
	return nil
}

// collectMetrics polls every running instance and stores its samples.
func collectMetrics(ctx context.Context, lister *instance.Repository, prov *provision.Provisioner, c *metrics.Collector, store *metrics.Store) error {
	instances, err := lister.List(ctx)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		if inst.Status != instance.StatusRunning {
			continue
		}
		dsn, err := prov.DSN(ctx, inst.ID)
		if err != nil {
			continue
		}
		samples, err := c.Collect(ctx, inst.ID, dsn)
		if err != nil {
			continue
		}
		_ = store.Insert(ctx, samples)
	}
	return nil
}
