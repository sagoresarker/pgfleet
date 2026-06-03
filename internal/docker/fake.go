package docker

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/sagoresarker/pgfleet/internal/apperr"
)

type fakeContainer struct {
	spec     ContainerSpec
	status   string
	running  bool
	exitCode int
	ports    map[string]string // "5432/tcp" -> host port
}

// Fake is an in-memory ContainerRuntime for unit tests. It models container
// state transitions (created → running → exited) and supports scriptable Exec
// via ExecFunc.
type Fake struct {
	mu         sync.Mutex
	seq        int
	containers map[string]*fakeContainer
	volumes    map[string]map[string]string
	networks   map[string]map[string]string

	// ExecFunc, if set, produces the result of Exec for a running container.
	ExecFunc func(id string, cmd []string) (ExecResult, error)
	// EnsureImageErr, if set, is returned by EnsureImage.
	EnsureImageErr error
	// OnStart, if set, is invoked after a container starts (outside the fake's
	// lock). Tests use it to model one-shot containers that exit on their own,
	// e.g. by calling MarkExited from inside the hook.
	OnStart func(f *Fake, id string)
}

// MarkExited transitions a container to the exited state with the given exit
// code. It is a test affordance for modelling one-shot containers.
func (f *Fake) MarkExited(id string, code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.containers[id]; ok {
		c.running = false
		c.status = "exited"
		c.exitCode = code
	}
}

// NewFake creates an empty in-memory runtime.
func NewFake() *Fake {
	return &Fake{
		containers: map[string]*fakeContainer{},
		volumes:    map[string]map[string]string{},
		networks:   map[string]map[string]string{},
	}
}

func (f *Fake) EnsureImage(_ context.Context, _ string) error {
	return f.EnsureImageErr
}

func (f *Fake) CreateContainer(_ context.Context, spec ContainerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := fmt.Sprintf("fake-%d", f.seq)
	ports := map[string]string{}
	for _, p := range spec.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		host := p.HostPort
		if host == 0 {
			host = 30000 + f.seq // deterministic Docker-assigned-style port
		}
		ports[fmt.Sprintf("%d/%s", p.ContainerPort, proto)] = fmt.Sprintf("%d", host)
	}
	f.containers[id] = &fakeContainer{spec: spec, status: "created", ports: ports}
	return id, nil
}

func (f *Fake) StartContainer(_ context.Context, id string) error {
	f.mu.Lock()
	c, err := f.get(id)
	if err != nil {
		f.mu.Unlock()
		return err
	}
	c.running = true
	c.status = "running"
	f.mu.Unlock()
	if f.OnStart != nil {
		f.OnStart(f, id)
	}
	return nil
}

func (f *Fake) StopContainer(_ context.Context, id string, _ *int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.get(id)
	if err != nil {
		return err
	}
	c.running = false
	c.status = "exited"
	return nil
}

func (f *Fake) RemoveContainer(_ context.Context, id string, force bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.get(id)
	if err != nil {
		return err
	}
	if c.running && !force {
		return apperr.New(apperr.KindConflict, "container is running; use force")
	}
	delete(f.containers, id)
	return nil
}

func (f *Fake) Inspect(_ context.Context, id string) (ContainerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.get(id)
	if err != nil {
		return ContainerState{}, err
	}
	return ContainerState{
		ID:       id,
		Name:     c.spec.Name,
		Status:   c.status,
		Running:  c.running,
		ExitCode: c.exitCode,
		Ports:    c.ports,
	}, nil
}

func (f *Fake) Exec(_ context.Context, id string, cmd []string) (ExecResult, error) {
	f.mu.Lock()
	c, err := f.get(id)
	if err != nil {
		f.mu.Unlock()
		return ExecResult{}, err
	}
	running := c.running
	f.mu.Unlock()

	if !running {
		return ExecResult{}, apperr.New(apperr.KindConflict, "container is not running")
	}
	if f.ExecFunc != nil {
		return f.ExecFunc(id, cmd)
	}
	return ExecResult{ExitCode: 0}, nil
}

func (f *Fake) Logs(_ context.Context, id string, _ bool) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.get(id); err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader("")), nil
}

func (f *Fake) ListByLabel(_ context.Context, labels map[string]string) ([]ContainerInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []ContainerInfo
	for id, c := range f.containers {
		if labelsMatch(c.spec.Labels, labels) {
			out = append(out, ContainerInfo{
				ID:     id,
				Name:   c.spec.Name,
				State:  c.status,
				Labels: c.spec.Labels,
			})
		}
	}
	return out, nil
}

func (f *Fake) CreateVolume(_ context.Context, name string, labels map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volumes[name] = labels
	return nil
}

func (f *Fake) RemoveVolume(_ context.Context, name string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.volumes, name)
	return nil
}

func (f *Fake) ListVolumesByLabel(_ context.Context, labels map[string]string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for name, have := range f.volumes {
		if labelsMatch(have, labels) {
			out = append(out, name)
		}
	}
	return out, nil
}

func (f *Fake) CreateNetwork(_ context.Context, name string, labels map[string]string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.networks[name] = labels
	return name, nil
}

func (f *Fake) RemoveNetwork(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.networks, id)
	return nil
}

// get returns the container or a NotFound error. Caller holds the lock.
func (f *Fake) get(id string) (*fakeContainer, error) {
	c, ok := f.containers[id]
	if !ok {
		return nil, apperr.New(apperr.KindNotFound, "container not found: "+id)
	}
	return c, nil
}
