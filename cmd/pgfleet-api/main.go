// Command pgfleet-api is the PgFleet control-plane HTTP server.
package main

import (
	"context"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/sagoresarker/pgfleet/internal/api"
	"github.com/sagoresarker/pgfleet/internal/config"
	"github.com/sagoresarker/pgfleet/internal/logging"
	"github.com/sagoresarker/pgfleet/internal/store"
	"github.com/sagoresarker/pgfleet/internal/version"
)

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

	router := api.NewRouter(api.Deps{
		Ready: store.Ready(pool),
	})

	return api.Serve(ctx, ln, router, log)
}
