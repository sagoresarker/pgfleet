// Package provision orchestrates the lifecycle of managed Postgres instances:
// creating their containers and volumes, wiring up WAL archiving, and creating
// and verifying the pgBackRest stanza so a "running" instance always has a
// working backup pipeline.
package provision

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
	"github.com/sagoresarker/pgfleet/internal/objectstore"
	"github.com/sagoresarker/pgfleet/internal/pgbackrest"
)

const (
	pgDataPath   = "/var/lib/postgresql/data"
	repoPath     = "/var/lib/pgbackrest"
	confPath     = "/etc/pgbackrest/pgbackrest.conf"
	pgPort       = 5432
	readyTimeout = 90 * time.Second
)

// ProgressFunc receives provisioning step updates (may be nil).
type ProgressFunc func(step, detail string)

func (f ProgressFunc) emit(step, detail string) {
	if f != nil {
		f(step, detail)
	}
}

// Options configures the Provisioner.
type Options struct {
	Network      string
	InstanceHost string
	S3           objectstore.Config
}

// store is the subset of *instance.Repository the provisioner needs.
type store interface {
	Get(ctx context.Context, id string) (instance.Instance, error)
	Password(ctx context.Context, id string) (string, error)
	SetRuntime(ctx context.Context, id, containerID string, hostPort int) error
	SetStatus(ctx context.Context, id string, status instance.Status, lastErr string) error
	Delete(ctx context.Context, id string) error
}

// Provisioner creates and manages instance containers.
type Provisioner struct {
	rt   docker.ContainerRuntime
	repo store
	opts Options
}

// New builds a Provisioner.
func New(rt docker.ContainerRuntime, repo store, opts Options) *Provisioner {
	return &Provisioner{rt: rt, repo: repo, opts: opts}
}

// Provision brings an instance from "provisioning" to a healthy "running"
// state with verified WAL archiving. On failure it records the error and marks
// the instance "error".
func (p *Provisioner) Provision(ctx context.Context, id string, progress ProgressFunc) error {
	if err := p.provision(ctx, id, progress); err != nil {
		_ = p.repo.SetStatus(ctx, id, instance.StatusError, err.Error())
		return err
	}
	return p.repo.SetStatus(ctx, id, instance.StatusRunning, "")
}

func (p *Provisioner) provision(ctx context.Context, id string, progress ProgressFunc) (err error) {
	inst, err := p.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	password, err := p.repo.Password(ctx, id)
	if err != nil {
		return err
	}

	progress.emit("image", "ensuring image "+inst.Image)
	if err := p.rt.EnsureImage(ctx, inst.Image); err != nil {
		return err
	}

	// Track resources so a failure midway doesn't leak Docker objects. Cleanup
	// uses a background context so it runs even if ctx was cancelled.
	var createdVolumes []string
	var createdContainer string
	defer func() {
		if err == nil {
			return
		}
		bg := context.Background()
		if createdContainer != "" {
			_ = p.rt.RemoveContainer(bg, createdContainer, true)
		}
		for _, v := range createdVolumes {
			_ = p.rt.RemoveVolume(bg, v, true)
		}
	}()

	dataVol := volumeName("data", id)
	progress.emit("volume", "creating data volume")
	if err = p.rt.CreateVolume(ctx, dataVol, instanceLabels(id)); err != nil {
		return err
	}
	createdVolumes = append(createdVolumes, dataVol)
	mounts := []docker.Mount{{Volume: dataVol, Path: pgDataPath}}
	if inst.RepoType == instance.RepoLocal {
		repoVol := volumeName("repo", id)
		if err = p.rt.CreateVolume(ctx, repoVol, instanceLabels(id)); err != nil {
			return err
		}
		createdVolumes = append(createdVolumes, repoVol)
		mounts = append(mounts, docker.Mount{Volume: repoVol, Path: repoPath})
	}

	progress.emit("container", "creating container")
	containerID, err := p.rt.CreateContainer(ctx, p.containerSpec(inst, password, mounts))
	if err != nil {
		return err
	}
	createdContainer = containerID
	// Persist the container id immediately so a later Destroy can find it even
	// if provisioning fails before reaching SetRuntime below.
	_ = p.repo.SetRuntime(ctx, id, containerID, 0)

	if err = p.rt.StartContainer(ctx, containerID); err != nil {
		return err
	}

	state, err := p.rt.Inspect(ctx, containerID)
	if err != nil {
		return err
	}
	hostPort, err := assignedPort(state)
	if err != nil {
		return err
	}
	if err = p.repo.SetRuntime(ctx, id, containerID, hostPort); err != nil {
		return err
	}

	progress.emit("waiting", "waiting for postgres to accept connections")
	if err := p.waitReady(ctx, containerID, inst.Superuser); err != nil {
		return err
	}

	progress.emit("extensions", "enabling pg_stat_statements")
	// Best-effort: query insights are optional, so a failure here is not fatal.
	_ = p.execOK(ctx, containerID, asPostgres([]string{
		"psql", "-U", inst.Superuser, "-d", "postgres", "-c",
		"CREATE EXTENSION IF NOT EXISTS pg_stat_statements",
	}))

	progress.emit("config", "writing pgbackrest configuration")
	if err := p.writeConfig(ctx, containerID, inst); err != nil {
		return err
	}

	progress.emit("stanza", "creating pgBackRest stanza")
	if err := p.execOK(ctx, containerID, asPostgres(pgbackrest.StanzaCreate(inst.Stanza, confPath))); err != nil {
		return err
	}

	progress.emit("check", "verifying WAL archiving")
	if err := p.execOK(ctx, containerID, asPostgres(pgbackrest.Check(inst.Stanza, confPath))); err != nil {
		return err
	}

	progress.emit("ready", "instance is healthy")
	return nil
}

func (p *Provisioner) containerSpec(inst instance.Instance, password string, mounts []docker.Mount) docker.ContainerSpec {
	archiveCmd := fmt.Sprintf("pgbackrest --stanza=%s archive-push %%p", inst.Stanza)
	cmd := []string{
		"postgres",
		"-c", "archive_mode=on",
		"-c", "archive_command=" + archiveCmd,
		"-c", "archive_timeout=60",
		"-c", "wal_level=replica",
		"-c", "max_wal_senders=3",
		"-c", "shared_preload_libraries=pg_stat_statements",
	}
	spec := docker.ContainerSpec{
		Name:  "pgfleet-pg-" + inst.Name,
		Image: inst.Image,
		Cmd:   cmd,
		Env: map[string]string{
			"POSTGRES_USER":     inst.Superuser,
			"POSTGRES_PASSWORD": password,
			"POSTGRES_DB":       "postgres",
		},
		Labels: instanceLabels(inst.ID),
		Ports:  []docker.PortMapping{{ContainerPort: pgPort, HostPort: 0}},
		Mounts: mounts,
	}
	if p.opts.Network != "" {
		spec.Networks = []string{p.opts.Network}
	}
	return spec
}

// writeConfig fixes repo-volume ownership and writes pgbackrest.conf (mode 0600,
// owned by postgres so its embedded S3 secret is not world-readable).
func (p *Provisioner) writeConfig(ctx context.Context, containerID string, inst instance.Instance) error {
	conf, err := p.backrestConf(inst)
	if err != nil {
		return err
	}
	script := strings.Join([]string{
		"set -e",
		"chown -R postgres:postgres " + repoPath,
		"umask 0177",
		"cat > " + confPath + " <<'PGBR_EOF'",
		conf,
		"PGBR_EOF",
		"chown postgres:postgres " + confPath,
	}, "\n")
	return p.execOK(ctx, containerID, []string{"sh", "-c", script})
}

func (p *Provisioner) backrestConf(inst instance.Instance) (string, error) {
	c := pgbackrest.InstanceConf{
		Stanza:        inst.Stanza,
		PGDataPath:    pgDataPath,
		PGPort:        pgPort,
		RetentionFull: 2,
		RepoType:      string(inst.RepoType),
	}
	switch inst.RepoType {
	case instance.RepoS3:
		c.S3 = pgbackrest.RepoS3{
			Endpoint:   p.opts.S3.Endpoint,
			Bucket:     p.opts.S3.Bucket,
			Region:     p.opts.S3.Region,
			Key:        p.opts.S3.AccessKey,
			Secret:     p.opts.S3.SecretKey,
			PathPrefix: "/stanzas/" + inst.Stanza,
			URIStyle:   "path",
			VerifyTLS:  p.opts.S3.UseTLS,
		}
	case instance.RepoLocal:
		c.Local = pgbackrest.RepoLocal{Path: repoPath}
	}
	return pgbackrest.BackrestConf(c)
}

func (p *Provisioner) waitReady(ctx context.Context, containerID, superuser string) error {
	deadline := time.Now().Add(readyTimeout)
	for {
		res, err := p.rt.Exec(ctx, containerID, []string{"pg_isready", "-U", superuser, "-h", "127.0.0.1"})
		if err == nil && res.ExitCode == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return apperr.New(apperr.KindInternal, "provision: postgres did not become ready in time")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// execOK runs a command and returns an error if it exits non-zero. The command
// is NOT included in the error: some commands embed secrets (e.g. the
// pgbackrest.conf heredoc carries the S3 key), and this error can surface in an
// instance's last_error. Only the first token (the program name) is named.
func (p *Provisioner) execOK(ctx context.Context, containerID string, cmd []string) error {
	res, err := p.rt.Exec(ctx, containerID, cmd)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		prog := "command"
		if len(cmd) > 0 {
			prog = cmd[0]
		}
		return apperr.New(apperr.KindInternal, fmt.Sprintf("provision: %s failed (exit %d): %s",
			prog, res.ExitCode, redactSecrets(strings.TrimSpace(res.Stderr+res.Stdout))))
	}
	return nil
}

// redactSecrets scrubs pgBackRest S3 secret values from command output before
// it is persisted or returned to clients.
func redactSecrets(s string) string {
	for _, key := range []string{"repo1-s3-key-secret", "repo1-s3-key", "repo1-cipher-pass"} {
		for {
			i := strings.Index(s, key+"=")
			if i < 0 {
				break
			}
			end := strings.IndexByte(s[i:], '\n')
			if end < 0 {
				end = len(s) - i
			}
			s = s[:i] + key + "=***" + s[i+end:]
		}
	}
	return s
}

func assignedPort(state docker.ContainerState) (int, error) {
	hp, ok := state.Ports[fmt.Sprintf("%d/tcp", pgPort)]
	if !ok || hp == "" {
		return 0, apperr.New(apperr.KindInternal, "provision: no host port assigned")
	}
	var port int
	if _, err := fmt.Sscanf(hp, "%d", &port); err != nil {
		return 0, apperr.Wrap(apperr.KindInternal, "provision: parse host port", err)
	}
	return port, nil
}

func instanceLabels(id string) map[string]string {
	return map[string]string{
		docker.LabelManaged:  "true",
		docker.LabelInstance: id,
		docker.LabelRole:     "postgres",
	}
}

func volumeName(kind, id string) string { return "pgfleet-" + kind + "-" + id }

// asPostgres wraps a command so it runs as the postgres OS user. pgBackRest
// must run as postgres so it connects to the cluster as the postgres role and
// shares ownership of PGDATA/pg_wal. The official image ships gosu.
func asPostgres(cmd []string) []string {
	return append([]string{"gosu", "postgres"}, cmd...)
}

// PgBackRestExec returns a runnable pgBackRest command (as the postgres user)
// for the given stanza, for callers that exec against the container directly.
func PgBackRestExec(cmd []string) []string { return asPostgres(cmd) }
