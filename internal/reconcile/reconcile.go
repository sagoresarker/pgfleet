// Package reconcile keeps the instance table and the actual Docker containers
// in agreement, so the control plane can crash and rediscover the world on
// restart instead of being amnesiac.
package reconcile

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/sagoresarker/pgfleet/internal/docker"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// Store is the subset of the instance repository the reconciler needs.
type Store interface {
	List(ctx context.Context) ([]instance.Instance, error)
	SetStatus(ctx context.Context, id string, status instance.Status, lastErr string) error
	SetRuntime(ctx context.Context, id, containerID string, hostPort int) error
}

// Reconciler reconciles instance rows against Docker containers by label.
type Reconciler struct {
	rt  docker.ContainerRuntime
	st  Store
	log *slog.Logger
}

// New builds a Reconciler. log may be nil.
func New(rt docker.ContainerRuntime, st Store, log *slog.Logger) *Reconciler {
	if log == nil {
		log = slog.New(slog.NewTextHandler(noopWriter{}, nil))
	}
	return &Reconciler{rt: rt, st: st, log: log}
}

// Reconcile performs one reconciliation pass.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	instances, err := r.st.List(ctx)
	if err != nil {
		return err
	}
	containers, err := r.rt.ListByLabel(ctx, map[string]string{docker.LabelManaged: "true"})
	if err != nil {
		return err
	}

	byInstance := make(map[string]docker.ContainerInfo, len(containers))
	for _, c := range containers {
		if id := c.Labels[docker.LabelInstance]; id != "" {
			byInstance[id] = c
		}
	}

	for _, inst := range instances {
		r.reconcileOne(ctx, inst, byInstance)
	}
	return nil
}

func (r *Reconciler) reconcileOne(ctx context.Context, inst instance.Instance, containers map[string]docker.ContainerInfo) {
	// States owned by another in-flight operation (a provisioning or restoring
	// goroutine, a destroy, or a terminal error) are left untouched so the
	// reconciler does not race with them and clobber their status.
	switch inst.Status {
	case instance.StatusError, instance.StatusDestroying, instance.StatusRestoring, instance.StatusProvisioning:
		return
	}

	info, ok := containers[inst.ID]
	if !ok {
		// The container is gone but the DB expected it. (Provisioning/restoring/
		// destroying/error statuses already returned above, so only a Running
		// instance reaches here as "should have a container".)
		if inst.Status == instance.StatusRunning {
			r.log.Warn("instance container missing; marking error", "instance", inst.ID)
			_ = r.st.SetStatus(ctx, inst.ID, instance.StatusError, "container not found during reconciliation")
		}
		return
	}

	state, err := r.rt.Inspect(ctx, info.ID)
	if err != nil {
		r.log.Warn("inspect failed during reconciliation", "instance", inst.ID, "err", err)
		return
	}

	// Adopt the container if the DB lost track of it (control-plane restart).
	if inst.ContainerID != info.ID {
		_ = r.st.SetRuntime(ctx, inst.ID, info.ID, hostPort(state))
	}

	desired := instance.StatusStopped
	if state.Running {
		desired = instance.StatusRunning
	}
	if inst.Status != desired {
		_ = r.st.SetStatus(ctx, inst.ID, desired, "")
	}
}

func hostPort(state docker.ContainerState) int {
	hp, ok := state.Ports["5432/tcp"]
	if !ok {
		return 0
	}
	var port int
	_, _ = fmt.Sscanf(hp, "%d", &port)
	return port
}

type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }
