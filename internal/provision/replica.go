package provision

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// ProvisionReplica provisions a streaming-replication standby of the given
// primary: it prepares the primary (replication HBA + slot), base-backs-up the
// primary into a fresh volume as a standby, and starts the replica streaming.
// On failure it records the error and cleans up created resources.
func (p *Provisioner) ProvisionReplica(ctx context.Context, replicaID string, primary instance.Instance, progress ProgressFunc) error {
	if err := p.provisionReplica(ctx, replicaID, primary, progress); err != nil {
		_ = p.repo.SetStatus(ctx, replicaID, instance.StatusError, err.Error())
		return err
	}
	return p.repo.SetStatus(ctx, replicaID, instance.StatusRunning, "")
}

func (p *Provisioner) provisionReplica(ctx context.Context, replicaID string, primary instance.Instance, progress ProgressFunc) (err error) {
	replica, err := p.repo.Get(ctx, replicaID)
	if err != nil {
		return err
	}
	primaryPassword, err := p.repo.Password(ctx, primary.ID)
	if err != nil {
		return err
	}
	slot := slotName(replica.Name)
	primaryHost := InstanceContainerName(primary.Name)

	progress.emit("image", "ensuring image "+replica.Image)
	if err = p.rt.EnsureImage(ctx, replica.Image); err != nil {
		return err
	}

	progress.emit("primary", "preparing primary for replication")
	if err = p.preparePrimary(ctx, primary, slot); err != nil {
		return err
	}

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

	dataVol := volumeName("data", replicaID)
	if err = p.rt.CreateVolume(ctx, dataVol, instanceLabels(replicaID)); err != nil {
		return err
	}
	createdVolumes = append(createdVolumes, dataVol)
	_ = p.repo.SetDataVolume(ctx, replicaID, dataVol)

	progress.emit("basebackup", "cloning primary via pg_basebackup")
	if err = p.runBaseBackup(ctx, replica, primary, primaryPassword, primaryHost, slot, dataVol); err != nil {
		return err
	}

	progress.emit("container", "starting replica")
	containerID, err := p.rt.CreateContainer(ctx, p.replicaSpec(replica, dataVol))
	if err != nil {
		return err
	}
	createdContainer = containerID
	_ = p.repo.SetRuntime(ctx, replicaID, containerID, 0)
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
	if err = p.repo.SetRuntime(ctx, replicaID, containerID, hostPort); err != nil {
		return err
	}

	progress.emit("waiting", "waiting for replica to accept connections")
	if err = p.waitReady(ctx, containerID, replica.Superuser); err != nil {
		return err
	}

	progress.emit("streaming", "verifying replication is streaming")
	if err = p.verifyStreaming(ctx, primary.ContainerID, replica.Name); err != nil {
		return err
	}
	progress.emit("ready", "replica is streaming")
	return nil
}

// preparePrimary ensures the primary allows replication connections and has a
// physical replication slot for the replica. Idempotent.
func (p *Provisioner) preparePrimary(ctx context.Context, primary instance.Instance, slot string) error {
	hba := strings.Join([]string{
		"set -e",
		// Add a network replication HBA line once.
		`grep -q 'pgfleet-replication' "$PGDATA/pg_hba.conf" || ` +
			`echo 'host replication all 0.0.0.0/0 scram-sha-256 # pgfleet-replication' >> "$PGDATA/pg_hba.conf"`,
		`gosu postgres psql -U ` + primary.Superuser + ` -d postgres -c 'SELECT pg_reload_conf()'`,
	}, "\n")
	if err := p.execOK(ctx, primary.ContainerID, []string{"bash", "-c", hba}); err != nil {
		return err
	}

	createSlot := fmt.Sprintf(
		`DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name='%s') `+
			`THEN PERFORM pg_create_physical_replication_slot('%s'); END IF; END $$;`, slot, slot)
	return p.execOK(ctx, primary.ContainerID,
		asPostgres([]string{"psql", "-U", primary.Superuser, "-d", "postgres", "-c", createSlot}))
}

// runBaseBackup clones the primary into the replica's data volume and writes
// standby recovery settings. Secrets are passed via env, never the script body.
func (p *Provisioner) runBaseBackup(ctx context.Context, replica, primary instance.Instance, password, primaryHost, slot, dataVol string) error {
	// The password is delivered to the standby's walreceiver via a .pgpass file
	// (PGPASSFILE points at it in the replica container). This avoids the
	// fragile double-quoting of a password inside a single-quoted GUC inside a
	// single-quoted libpq conninfo, and works for any password (spaces, quotes,
	// etc.). Only ':' and '\' need escaping in .pgpass.
	script := strings.Join([]string{
		"set -e",
		`chown postgres:postgres "$PGDATA_DIR"`,
		`gosu postgres pg_basebackup -h "$PRIMARY_HOST" -p 5432 -U "$PRIMARY_USER" -D "$PGDATA_DIR" -X stream -S "$SLOT" -w -c fast`,
		`touch "$PGDATA_DIR/standby.signal"`,
		`printf "primary_conninfo = 'host=%s port=5432 user=%s application_name=%s passfile=%s'\nprimary_slot_name = '%s'\n" ` +
			`"$PRIMARY_HOST" "$PRIMARY_USER" "$APP_NAME" "$PGDATA_DIR/.pgpass" "$SLOT" >> "$PGDATA_DIR/postgresql.auto.conf"`,
		`ESC_PW=$(printf '%s' "$PGPASSWORD" | sed 's/\\/\\\\/g; s/:/\\:/g')`,
		`printf '%s:5432:*:%s:%s\n' "$PRIMARY_HOST" "$PRIMARY_USER" "$ESC_PW" > "$PGDATA_DIR/.pgpass"`,
		`chmod 600 "$PGDATA_DIR/.pgpass"`,
		`chown -R postgres:postgres "$PGDATA_DIR"`,
	}, "\n")

	spec := docker.ContainerSpec{
		Name:  "pgfleet-basebackup-" + replica.Name + "-" + shortStamp(),
		Image: replica.Image,
		Cmd:   []string{"bash", "-c", script},
		Env: map[string]string{
			"PGPASSWORD":   password,
			"PRIMARY_HOST": primaryHost,
			"PRIMARY_USER": primary.Superuser,
			"APP_NAME":     replica.Name,
			"SLOT":         slot,
			"PGDATA_DIR":   pgDataPath,
		},
		Labels: map[string]string{
			docker.LabelManaged:  "true",
			docker.LabelInstance: replica.ID,
			docker.LabelRole:     "basebackup",
		},
		Mounts: []docker.Mount{{Volume: dataVol, Path: pgDataPath}},
	}
	if p.opts.Network != "" {
		spec.Networks = []string{p.opts.Network}
	}

	cid, err := p.rt.CreateContainer(ctx, spec)
	if err != nil {
		return err
	}
	defer func() { _ = p.rt.RemoveContainer(context.Background(), cid, true) }()
	if err := p.rt.StartContainer(ctx, cid); err != nil {
		return err
	}
	return p.waitRestoreExit(ctx, cid)
}

// replicaSpec builds the standby container spec (no archiving; recovery
// settings come from the base backup).
func (p *Provisioner) replicaSpec(replica instance.Instance, dataVol string) docker.ContainerSpec {
	spec := docker.ContainerSpec{
		Name:   InstanceContainerName(replica.Name),
		Image:  replica.Image,
		Cmd:    []string{"postgres", "-c", "hot_standby=on"},
		Labels: instanceLabels(replica.ID),
		Ports:  []docker.PortMapping{{ContainerPort: pgPort, HostPort: 0}},
		Mounts: []docker.Mount{{Volume: dataVol, Path: pgDataPath}},
	}
	if p.opts.Network != "" {
		spec.Networks = []string{p.opts.Network}
	}
	return spec
}

// verifyStreaming polls the primary until it reports the replica as streaming.
func (p *Provisioner) verifyStreaming(ctx context.Context, primaryContainerID, appName string) error {
	q := fmt.Sprintf("SELECT count(*) FROM pg_stat_replication WHERE application_name='%s' AND state='streaming'", appName)
	deadline := time.Now().Add(60 * time.Second)
	for {
		res, err := p.rt.Exec(ctx, primaryContainerID, asPostgres([]string{"psql", "-tAc", q}))
		if err == nil && res.ExitCode == 0 && strings.TrimSpace(res.Stdout) != "0" {
			return nil
		}
		if time.Now().After(deadline) {
			return apperr.New(apperr.KindInternal, "provision: replica did not start streaming in time")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// slotName derives a valid replication slot name from a replica name.
func slotName(replicaName string) string {
	return "pgfleet_" + strings.ReplaceAll(replicaName, "-", "_")
}
