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
	"github.com/sagoresarker/pgfleet/internal/bootstrap"
	"github.com/sagoresarker/pgfleet/internal/config"
	"github.com/sagoresarker/pgfleet/internal/logging"
	"github.com/sagoresarker/pgfleet/internal/store"
	"github.com/sagoresarker/pgfleet/internal/user"
	"github.com/sagoresarker/pgfleet/internal/version"
)

// tokenTTL is the lifetime of issued session tokens.
const tokenTTL = 24 * time.Hour

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

	router := api.NewRouter(api.Deps{
		Ready:  store.Ready(pool),
		Issuer: issuer,
		Auth:   api.NewAuthHandler(users, issuer, recorder),
		Users:  api.NewUsersHandler(users, recorder),
	})

	return api.Serve(ctx, ln, router, log)
}
