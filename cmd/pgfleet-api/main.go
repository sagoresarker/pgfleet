// Command pgfleet-api is the PgFleet control-plane HTTP server.
package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sagoresarker/pgfleet/internal/alerts"
	"github.com/sagoresarker/pgfleet/internal/api"
	"github.com/sagoresarker/pgfleet/internal/audit"
	"github.com/sagoresarker/pgfleet/internal/auth"
	"github.com/sagoresarker/pgfleet/internal/backup"
	"github.com/sagoresarker/pgfleet/internal/bootstrap"
	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/clusterctl"
	"github.com/sagoresarker/pgfleet/internal/config"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/events"
	"github.com/sagoresarker/pgfleet/internal/failover"
	"github.com/sagoresarker/pgfleet/internal/health"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/logging"
	"github.com/sagoresarker/pgfleet/internal/metabackup"
	"github.com/sagoresarker/pgfleet/internal/metrics"
	"github.com/sagoresarker/pgfleet/internal/objectstore"
	"github.com/sagoresarker/pgfleet/internal/provision"
	"github.com/sagoresarker/pgfleet/internal/reconcile"
	"github.com/sagoresarker/pgfleet/internal/remotebackup"
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

const alertsInterval = 1 * time.Minute

// metaBackupInterval is how often the control plane dumps its own meta DB to the
// object store; metaBackupRetain is how many dumps it keeps.
const metaBackupInterval = 6 * time.Hour
const metaBackupRetain = 28 // ~1 week at 6h cadence

// failoverInterval is how often clusters are checked; failoverThreshold is the
// number of consecutive failed primary checks before promoting (conservative to
// avoid reacting to transient blips: ~90s here).
const failoverInterval = 30 * time.Second
const failoverThreshold = 3

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
		Network:          cfg.DockerNetwork,
		InstanceHost:     cfg.InstanceHost,
		S3:               s3,
		RestartPolicy:    cfg.InstanceRestartPolicy,
		BindAddress:      cfg.InstanceBindAddress,
		MasterKey:        cfg.MasterKey,
		BackupEncryption: cfg.BackupEncryption,
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
	eventStore := events.NewStore(pool)
	catalog := backup.NewCatalog(pool)
	// WithEvents so both scheduled and API-triggered backups/deletes emit durable
	// events into the timeline.
	backups := backup.New(rt, instances, catalog).WithEvents(eventStore)
	metricStore := metrics.NewStore(pool)
	collector := metrics.NewCollector()
	resourceCollector := metrics.NewResourceCollector(rt)
	healthStore := health.NewStore(pool)
	healthChecker := health.NewChecker(rt, instances, catalog, health.DefaultThresholds())
	alertStore := alerts.NewStore(pool)
	alertNotifier := alerts.NewWebhookNotifier(cfg.AlertWebhookURL, 5*time.Second)

	sched := scheduler.New(scheduler.WithErrorHandler(func(name string, err error) {
		log.Warn("scheduled job failed", "job", name, "err", err)
	}))
	sched.Register("reconcile", reconcileInterval, reconciler.Reconcile)
	sched.Register("scheduled-backups", cfg.BackupInterval, func(ctx context.Context) error {
		return backups.RunScheduled(ctx, instances, cfg.BackupType)
	})
	sched.Register("collect-metrics", metricsInterval, func(ctx context.Context) error {
		return collectMetrics(ctx, instances, provisioner, collector, resourceCollector, metricStore)
	})
	sched.Register("prune-metrics", time.Hour, func(ctx context.Context) error {
		return metricStore.Prune(ctx, time.Now().Add(-metricsRetention))
	})
	sched.Register("health-checks", healthInterval, func(ctx context.Context) error {
		return runHealthChecks(ctx, instances, healthChecker, healthStore)
	})
	sched.Register("evaluate-alerts", alertsInterval, func(ctx context.Context) error {
		return evaluateAlerts(ctx, instances, metricStore, healthStore, alertStore, alertNotifier, eventStore)
	})
	sched.Register("restore-drills", drillInterval, func(ctx context.Context) error {
		return runRestoreDrills(ctx, instances, provisioner, healthStore)
	})
	// Meta-DB self-backup: dump the control plane's own state to the external
	// object store so it can be reconstructed after a meta-DB loss. Only when an
	// object store is configured (a local-only meta backup would defeat the
	// purpose).
	if cfg.S3Endpoint != "" && cfg.S3Bucket != "" {
		metaBak := metabackup.New(s3)
		sched.Register("meta-db-backup", metaBackupInterval, func(ctx context.Context) error {
			if _, err := metaBak.Backup(ctx, cfg.MetaDBDSN); err != nil {
				return err
			}
			return metaBak.Prune(ctx, metaBackupRetain)
		})
	}
	// Automatic cluster failover: detect a dead primary, fence it, promote the
	// most-caught-up replica, reattach the others, and repoint the router.
	if cfg.AutoFailover {
		foController := failover.New(clusters, instances, provisioner, rt, eventStore, failoverThreshold, log)
		sched.Register("auto-failover", failoverInterval, foController.Run)
	}
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

	// Remote backup & restore ("migrate-in"): capture a logical dump of an
	// external Postgres the operator supplies credentials for, stream it to the
	// same object store as the rest of the fleet, and restore it into a freshly
	// provisioned instance or cluster. Sealed source secrets live in the meta DB.
	remoteSvc := remotebackup.New(remotebackup.NewObjectStore(s3), remotebackup.NewRepository(pool, cipher))
	remoteProv := newRemoteTargetProvisioner(instances, provisioner, clusterSvc, clusters)
	remoteHandler := api.NewRemoteHandler(remoteSvc, remoteProv).WithAudit(recorder).WithAsync(async)

	// Trusted-header single sign-on: when an Authelia/OIDC forward-auth proxy is
	// configured (PGFLEET_SSO_EMAIL_HEADER set), exchange the proxy-verified
	// identity for a PgFleet token. Mounted only when configured.
	ssoCfg := api.SSOConfig{
		EmailHeader:   cfg.SSOEmailHeader,
		GroupsHeader:  cfg.SSOGroupsHeader,
		AutoProvision: cfg.SSOAutoProvision,
		AdminGroup:    cfg.SSOAdminGroup,
		OperatorGroup: cfg.SSOOperatorGroup,
	}
	var ssoHandler *api.SSOHandler
	if ssoCfg.Enabled() {
		ssoHandler = api.NewSSOHandler(users, issuer, ssoCfg).WithAudit(recorder)
		log.Info("trusted-header SSO enabled", "email_header", cfg.SSOEmailHeader, "auto_provision", cfg.SSOAutoProvision)
	}

	router := api.NewRouter(api.Deps{
		Ready:     store.Ready(pool),
		Issuer:    issuer,
		Auth:      api.NewAuthHandler(users, issuer, recorder),
		SSO:       ssoHandler,
		Users:     api.NewUsersHandler(users, recorder),
		Audit:     api.NewAuditHandler(recorder),
		Instances: api.NewInstancesHandler(instances, provisioner, hub).WithAudit(recorder).WithAsync(async).WithCloneBackup(backups),
		Clusters:  api.NewClustersHandler(clusterSvc, clusters, instances, cfg.InstanceHost, recorder).WithAsync(async),
		Backups:   api.NewBackupsHandler(backups, provisioner, recorder).WithAsync(async),
		Metrics:   api.NewMetricsHandler(metricStore, insights),
		Health:    api.NewHealthHandler(healthStore),
		Events:    hub,

		Timescale:     api.NewTimescaleHandler(instances, provisioner.DSN),
		Alerts:        api.NewAlertsHandler(alertStore),
		EventsHistory: api.NewEventsHistoryHandler(eventStore),
		Logs:          api.NewLogsHandler(instances, rt),
		Prometheus:    api.NewPrometheusHandler(instances, metricStore),
		SQL:           api.NewSQLHandler(provisioner.DSN).WithAudit(recorder),
		Exec:          api.NewExecHandler(instances, rt).WithAudit(recorder),
		Dump:          api.NewDumpHandler(instances, provisioner.DSN).WithLogger(log).WithAudit(recorder),
		Remote:        remoteHandler,
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
func collectMetrics(ctx context.Context, lister *instance.Repository, prov *provision.Provisioner, c *metrics.Collector, rc *metrics.ResourceCollector, store *metrics.Store) error {
	instances, err := lister.List(ctx)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		if inst.Status != instance.StatusRunning {
			continue
		}
		// Postgres-internal stats (via a SQL connection).
		if dsn, err := prov.DSN(ctx, inst.ID); err == nil {
			if samples, err := c.Collect(ctx, inst.ID, dsn); err == nil {
				_ = store.Insert(ctx, samples)
			}
		}
		// Container resource stats incl. the data-volume disk-free %.
		if inst.ContainerID != "" {
			if rs, err := rc.Collect(ctx, inst.ID, inst.ContainerID); err == nil {
				_ = store.Insert(ctx, rs)
			}
		}
	}
	return nil
}

// evaluateAlerts builds a per-instance snapshot from the latest metrics + health
// report, evaluates the alert thresholds, persists firing/resolved state, and
// fires the webhook + records an event on each transition.
func evaluateAlerts(ctx context.Context, lister *instance.Repository, metricStore *metrics.Store, healthStore *health.Store, alertStore *alerts.Store, notifier *alerts.WebhookNotifier, eventStore *events.Store) error {
	instances, err := lister.List(ctx)
	if err != nil {
		return err
	}
	reports, _ := healthStore.List(ctx)
	backupAge := make(map[string]float64, len(reports))
	for _, rep := range reports {
		if rep.LastBackupAge > 0 {
			backupAge[rep.InstanceID] = rep.LastBackupAge.Seconds()
		}
	}

	th := alerts.DefaultThresholds()
	for _, inst := range instances {
		if inst.Status != instance.StatusRunning {
			continue
		}
		latest, err := metricStore.Latest(ctx, inst.ID)
		if err != nil {
			continue
		}
		snap := alerts.Snapshot{InstanceID: inst.ID}
		if s, ok := latest["disk_free_percent"]; ok {
			v := s.Value
			snap.DiskFreePercent = &v
		}
		if s, ok := latest["replication_lag_seconds"]; ok {
			v := s.Value
			snap.ReplicationLagSeconds = &v
		}
		if s, ok := latest["connection_utilization"]; ok {
			v := s.Value
			snap.ConnectionUtilizationPercent = &v
		}
		if age, ok := backupAge[inst.ID]; ok {
			snap.BackupAgeSeconds = &age
		}

		transitions, err := alertStore.Sync(ctx, inst.ID, alerts.Evaluate(snap, th))
		if err != nil || len(transitions) == 0 {
			continue
		}
		_ = notifier.Notify(ctx, transitions)
		for _, tr := range transitions {
			_, _ = eventStore.Record(ctx, events.NewEvent{
				InstanceID: inst.ID,
				Type:       "alert",
				Message:    tr.Message,
				Metadata:   map[string]string{"kind": tr.Kind, "state": tr.State, "severity": tr.Severity},
			})
		}
	}
	return nil
}
