package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/pgbackrest"
)

// Container-internal paths, mirroring internal/provision.
const (
	dataPath = "/var/lib/postgresql/data"
	repoPath = "/var/lib/pgbackrest"
	confPath = "/etc/pgbackrest/pgbackrest.conf"
	pgPort   = 5432
)

// restoreTimeout bounds how long we wait for the one-shot restore container to
// finish recovery.
const restoreTimeout = 30 * time.Minute

// runRestore restores a managed instance's data from its pgBackRest repo into a
// fresh Docker volume, with NO meta database and NO control plane. It mirrors
// internal/provision/restore.go's one-shot restore container: it writes a
// pgbackrest.conf, runs `pgbackrest restore` as the postgres user, then drives
// recovery to completion with pg_ctl start/stop so the resulting volume holds a
// promoted, ready-to-run cluster.
func runRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	stanza := fs.String("stanza", "", "pgBackRest stanza / instance name (required)")
	image := fs.String("image", instance.DefaultImage, "managed Postgres+pgBackRest image to run the restore in")
	repoType := fs.String("repo-type", "s3", "repository type: s3 | local")
	dataVolume := fs.String("data-volume", "", "target Docker volume name (default pgfleet-restore-<stanza>)")
	network := fs.String("network", "", "optional Docker network to attach the one-shot container to")

	// S3 repo flags.
	endpoint := fs.String("s3-endpoint", "", "object-store endpoint, host:port or URL (repo-type s3)")
	bucket := fs.String("s3-bucket", "", "object-store bucket holding the pgBackRest repo (repo-type s3)")
	region := fs.String("s3-region", "us-east-1", "object-store region (repo-type s3)")
	s3key := fs.String("s3-key", "", "object-store access key (repo-type s3)")
	s3secret := fs.String("s3-secret", "", "object-store secret key (repo-type s3)")
	s3tls := fs.Bool("s3-tls", false, "use TLS for the object store + pgBackRest verify-tls (repo-type s3)")
	pathPrefix := fs.String("s3-path", "", "repo path prefix in the bucket (default /stanzas/<stanza>)")

	// Local repo flags.
	repoVolume := fs.String("repo-volume", "", "Docker volume holding the pgBackRest repo, mounted at "+repoPath+" (repo-type local)")
	localPath := fs.String("local-path", repoPath, "repo path inside the container (repo-type local)")

	// Recovery target.
	rType := fs.String("type", "", "recovery target type: \"\" (latest) | time | lsn | xid | name")
	target := fs.String("target", "", "recovery target value (required when --type is set)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: pgfleet restore [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Restore a managed instance's data from its pgBackRest repo into a fresh\n")
		fmt.Fprintf(os.Stderr, "Docker volume. Requires a reachable Docker daemon and the managed image.\n\n")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return err
	}
	if *stanza == "" {
		return fmt.Errorf("--stanza is required")
	}

	conf, err := buildConf(*stanza, *repoType, repoFlags{
		endpoint:   *endpoint,
		bucket:     *bucket,
		region:     *region,
		key:        *s3key,
		secret:     *s3secret,
		tls:        *s3tls,
		pathPrefix: *pathPrefix,
		localPath:  *localPath,
	})
	if err != nil {
		return err
	}

	restoreArgs, err := pgbackrest.Restore(*stanza, confPath, pgbackrest.RestoreOpts{
		Type:         *rType,
		Target:       *target,
		TargetAction: targetAction(*rType),
	})
	if err != nil {
		return err
	}

	volume := *dataVolume
	if volume == "" {
		volume = "pgfleet-restore-" + *stanza
	}

	// Mounts: the fresh data volume always; the repo volume too for local repos.
	mounts := []docker.Mount{{Volume: volume, Path: dataPath}}
	if *repoType == "local" {
		if *repoVolume == "" {
			return fmt.Errorf("--repo-volume is required for --repo-type local (it is mounted at %s)", *localPath)
		}
		mounts = append(mounts, docker.Mount{Volume: *repoVolume, Path: *localPath})
	}

	script := restoreScript(conf, restoreArgs)

	ctx := context.Background()
	rt, err := docker.NewMoby()
	if err != nil {
		return err
	}
	defer func() { _ = rt.Close() }()

	fmt.Printf("ensuring image %s ...\n", *image)
	if err := rt.EnsureImage(ctx, *image); err != nil {
		return fmt.Errorf("ensuring image: %w", err)
	}

	fmt.Printf("creating data volume %s ...\n", volume)
	if err := rt.CreateVolume(ctx, volume, map[string]string{
		docker.LabelManaged: "true",
		docker.LabelRole:    "restore",
	}); err != nil {
		return fmt.Errorf("creating volume %q: %w", volume, err)
	}

	spec := docker.ContainerSpec{
		Name:  "pgfleet-restore-" + *stanza + "-" + stamp(),
		Image: *image,
		Cmd:   []string{"bash", "-c", script},
		Labels: map[string]string{
			docker.LabelManaged: "true",
			docker.LabelRole:    "restore",
		},
		Mounts: mounts,
	}
	if *network != "" {
		spec.Networks = []string{*network}
	}

	fmt.Printf("running restore for stanza %q into volume %q ...\n", *stanza, volume)
	exitCode, logs, err := runOneShot(ctx, rt, spec)
	if err != nil {
		return fmt.Errorf("restore container: %w", err)
	}
	if exitCode != 0 {
		fmt.Fprintln(os.Stderr, "--- restore container logs ---")
		fmt.Fprintln(os.Stderr, logs)
		fmt.Fprintln(os.Stderr, "--- end logs ---")
		return fmt.Errorf("restore failed: container exited with code %d", exitCode)
	}

	printSuccess(volume, *image)
	return nil
}

// runOneShot creates, starts, waits for, and removes a one-shot container. It
// returns the container's exit code and full logs (read before removal).
func runOneShot(ctx context.Context, rt *docker.Moby, spec docker.ContainerSpec) (int, string, error) {
	cid, err := rt.CreateContainer(ctx, spec)
	if err != nil {
		return 0, "", fmt.Errorf("create container: %w", err)
	}
	defer func() { _ = rt.RemoveContainer(context.Background(), cid, true) }()

	if err := rt.StartContainer(ctx, cid); err != nil {
		return 0, "", fmt.Errorf("start container: %w", err)
	}

	deadline := time.Now().Add(restoreTimeout)
	for {
		state, err := rt.Inspect(ctx, cid)
		if err != nil {
			return 0, "", fmt.Errorf("inspect container: %w", err)
		}
		if !state.Running && state.Status == "exited" {
			return state.ExitCode, containerLogs(ctx, rt, cid), nil
		}
		if time.Now().After(deadline) {
			return 0, containerLogs(ctx, rt, cid), fmt.Errorf("restore timed out after %s", restoreTimeout)
		}
		select {
		case <-ctx.Done():
			return 0, "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// containerLogs reads up to 256 KiB of the container's combined logs.
func containerLogs(ctx context.Context, rt *docker.Moby, cid string) string {
	rc, err := rt.Logs(ctx, cid, false)
	if err != nil {
		return ""
	}
	defer func() { _ = rc.Close() }()
	data, _ := io.ReadAll(io.LimitReader(rc, 256*1024))
	return strings.TrimSpace(string(data))
}

// repoFlags carries the repository-specific flags into buildConf.
type repoFlags struct {
	endpoint   string
	bucket     string
	region     string
	key        string
	secret     string
	tls        bool
	pathPrefix string
	localPath  string
}

// buildConf renders the pgbackrest.conf for the restore, mirroring how the
// control plane builds it in internal/provision (BackrestConf, /stanzas/<stanza>
// prefix, path URI style). The render function is pgbackrest.BackrestConf.
func buildConf(stanza, repoType string, f repoFlags) (string, error) {
	c := pgbackrest.InstanceConf{
		Stanza:        stanza,
		PGDataPath:    dataPath,
		PGPort:        pgPort,
		RetentionFull: 2,
		RepoType:      repoType,
	}
	switch repoType {
	case "s3":
		if f.endpoint == "" || f.bucket == "" || f.key == "" || f.secret == "" {
			return "", fmt.Errorf("repo-type s3 requires --s3-endpoint, --s3-bucket, --s3-key and --s3-secret")
		}
		prefix := f.pathPrefix
		if prefix == "" {
			prefix = "/stanzas/" + stanza
		}
		c.S3 = pgbackrest.RepoS3{
			Endpoint:   f.endpoint,
			Bucket:     f.bucket,
			Region:     f.region,
			Key:        f.key,
			Secret:     f.secret,
			PathPrefix: prefix,
			URIStyle:   "path",
			VerifyTLS:  f.tls,
		}
	case "local":
		c.Local = pgbackrest.RepoLocal{Path: f.localPath}
	default:
		return "", fmt.Errorf("invalid --repo-type %q (want s3 or local)", repoType)
	}
	return pgbackrest.BackrestConf(c)
}

// restoreScript builds the bash script the one-shot container runs. It mirrors
// internal/provision/restore.go's runRestoreContainer: write the conf, chown the
// data/repo dirs to postgres, restore as postgres, then drive recovery to the
// target with pg_ctl start/stop so the volume ends up holding a promoted,
// ready-to-run cluster.
func restoreScript(conf string, restoreArgs []string) string {
	return strings.Join([]string{
		"set -e",
		"mkdir -p /etc/pgbackrest",
		"chown postgres:postgres " + repoPath + " " + dataPath,
		"umask 0177",
		"cat > " + confPath + " <<'PGBR_EOF'",
		conf,
		"PGBR_EOF",
		"chown postgres:postgres " + confPath,
		shellJoin(asPostgres(restoreArgs)),
		// Drive recovery to the target and promote, then shut down cleanly.
		"gosu postgres pg_ctl -D " + dataPath + " -w -t 600 start",
		"gosu postgres pg_ctl -D " + dataPath + " -m fast -w stop",
	}, "\n")
}

// asPostgres prefixes a command with gosu so it runs as the postgres user.
func asPostgres(args []string) []string {
	return append([]string{"gosu", "postgres"}, args...)
}

// shellJoin single-quotes each argument so the assembled command is safe to run
// from bash -c.
func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

// targetAction returns the recovery target action. For a targeted PITR we
// promote so the cluster opens read-write at the target.
func targetAction(restoreType string) string {
	if restoreType == "" {
		return ""
	}
	return "promote"
}

// stamp returns a short unique suffix for the throwaway container name.
func stamp() string {
	return fmt.Sprintf("%x", time.Now().UnixNano()&0xffffffff)
}

// printSuccess prints the restored volume and a ready-to-paste docker run
// command the operator can use to bring Postgres up on it.
func printSuccess(volume, image string) {
	fmt.Println()
	fmt.Println("restore complete.")
	fmt.Printf("  restored data volume: %s\n", volume)
	fmt.Println()
	fmt.Println("start Postgres on the restored volume with:")
	fmt.Printf("  docker run -d --name pgfleet-restored \\\n")
	fmt.Printf("    -v %s:%s \\\n", volume, dataPath)
	fmt.Printf("    -p 5432:5432 \\\n")
	fmt.Printf("    %s\n", image)
}
