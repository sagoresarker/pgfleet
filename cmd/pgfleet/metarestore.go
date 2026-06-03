package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/sagoresarker/pgfleet/internal/metabackup"
	"github.com/sagoresarker/pgfleet/internal/objectstore"
)

// runMetaRestore restores the control-plane meta database from an object-store
// meta backup, without any running control plane. It shells out to pg_restore
// (via the metabackup service), so pg_restore must be on PATH.
func runMetaRestore(args []string) error {
	fs := flag.NewFlagSet("meta-restore", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	dsn := fs.String("dsn", "", "target meta DB DSN to restore into (required)")
	endpoint := fs.String("s3-endpoint", "", "object-store endpoint, host:port or URL (required)")
	bucket := fs.String("s3-bucket", "", "object-store bucket holding the meta backups (required)")
	region := fs.String("s3-region", "us-east-1", "object-store region")
	key := fs.String("s3-key", "", "object-store access key (required)")
	secret := fs.String("s3-secret", "", "object-store secret key (required)")
	tls := fs.Bool("s3-tls", false, "use TLS (https) to reach the object store")
	object := fs.String("object", "", "specific dump key to restore; empty restores the NEWEST")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: pgfleet meta-restore [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Restore the control-plane meta DB from an object-store meta backup.\n")
		fmt.Fprintf(os.Stderr, "Requires pg_restore on PATH.\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dsn == "" {
		return fmt.Errorf("--dsn is required")
	}

	store := objectstore.Config{
		Endpoint:  *endpoint,
		Region:    *region,
		AccessKey: *key,
		SecretKey: *secret,
		Bucket:    *bucket,
		UseTLS:    *tls,
	}
	if err := store.Validate(); err != nil {
		return err
	}

	ctx := context.Background()
	svc := metabackup.New(store)

	target := *object
	if target == "" {
		keys, err := svc.List(ctx)
		if err != nil {
			return fmt.Errorf("listing meta backups: %w", err)
		}
		if len(keys) == 0 {
			return fmt.Errorf("no meta backups found in bucket %q", *bucket)
		}
		// List returns keys oldest-first (lexical == chronological), so the last
		// element is the newest dump.
		target = keys[len(keys)-1]
		fmt.Printf("selected newest meta backup: %s\n", target)
	}

	fmt.Printf("restoring meta DB from %s ...\n", target)
	if err := svc.Restore(ctx, *dsn, target); err != nil {
		return fmt.Errorf("meta restore failed: %w", err)
	}

	fmt.Printf("meta DB restored successfully from %s\n", target)
	return nil
}
