package docker

import (
	"context"
	"sync"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

func TestFakeNotFound(t *testing.T) {
	f := NewFake()
	if _, err := f.Inspect(context.Background(), "nope"); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("Inspect of unknown id should be NotFound, got %v", apperr.Kind(err))
	}
	if err := f.StopContainer(context.Background(), "nope", nil); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("Stop of unknown id should be NotFound, got %v", apperr.Kind(err))
	}
}

func TestFakeHostPortAssignedDeterministically(t *testing.T) {
	f := NewFake()
	id, _ := f.CreateContainer(context.Background(), ContainerSpec{
		Name:  "c",
		Ports: []PortMapping{{ContainerPort: 5432}},
	})
	state, err := f.Inspect(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if state.Ports["5432/tcp"] == "" {
		t.Error("a published port should get a host port")
	}
}

// TestFakeConcurrentOpsRaceFree — the fake must be safe under concurrent use
// (run with -race).
func TestFakeConcurrentOpsRaceFree(_ *testing.T) {
	f := NewFake()
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, _ := f.CreateContainer(context.Background(), ContainerSpec{
				Name:   "c",
				Labels: map[string]string{LabelManaged: "true"},
			})
			_ = f.StartContainer(context.Background(), id)
			_, _ = f.Inspect(context.Background(), id)
			_, _ = f.ListByLabel(context.Background(), map[string]string{LabelManaged: "true"})
			_ = f.RemoveContainer(context.Background(), id, true)
		}()
	}
	wg.Wait()
}
