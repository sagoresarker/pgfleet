package backup

import (
	"context"

	"github.com/sagoresarker/pgfleet/internal/instance"
)

// InstanceLister lists instances to back up on a schedule.
type InstanceLister interface {
	List(ctx context.Context) ([]instance.Instance, error)
}

// RunScheduled takes a backup of the given type for every running instance and
// then enforces retention (expire). Per-instance failures are logged via the
// returned error but do not stop the sweep; the first error is returned.
func (s *Service) RunScheduled(ctx context.Context, lister InstanceLister, backupType string) error {
	instances, err := lister.List(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, inst := range instances {
		if inst.Status != instance.StatusRunning {
			continue
		}
		if err := s.Run(ctx, inst.ID, backupType); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := s.Expire(ctx, inst.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
