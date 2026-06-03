// Command pgfleet is a control-plane-INDEPENDENT disaster-recovery CLI. It does
// not talk to the PgFleet API or the meta database: it restores directly from
// the backup repository (object store / pgBackRest) using only repo credentials
// and a stanza. This is the break-glass tool you reach for when the control
// plane itself is gone.
//
// Subcommands:
//
//	pgfleet meta-restore  — restore the control-plane meta DB from an object-store
//	                        meta backup (needs pg_restore on PATH).
//	pgfleet restore       — restore a managed instance's data from its pgBackRest
//	                        repo into a fresh Docker volume (needs Docker + the
//	                        managed image).
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("a subcommand is required")
	}

	switch args[0] {
	case "meta-restore":
		return runMetaRestore(args[1:])
	case "restore":
		return runRestore(args[1:])
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `pgfleet — control-plane-independent disaster-recovery CLI

Usage:
  pgfleet <subcommand> [flags]

Subcommands:
  meta-restore   Restore the control-plane meta DB from an object-store meta backup.
                 Requires pg_restore on PATH.
  restore        Restore a managed instance's data from its pgBackRest repo into a
                 fresh Docker volume. Requires a reachable Docker daemon and the
                 managed Postgres+pgBackRest image.

Run "pgfleet <subcommand> -h" for subcommand flags.
`)
}
