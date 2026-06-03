package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/sagoresarker/pgfleet/internal/cluster"
	"github.com/sagoresarker/pgfleet/internal/composegen"
	"github.com/sagoresarker/pgfleet/internal/instance"
)

// composeInstanceReader fetches a single instance by id. Satisfied by the
// instance repository (its Get method).
type composeInstanceReader interface {
	Get(ctx context.Context, id string) (instance.Instance, error)
}

// composeClusterReader fetches a cluster and its member instances by cluster id.
// Get is satisfied by the cluster repository; ListByCluster by the instance
// repository — so a small adapter (or a struct embedding both) wires them.
// clusterctl.Service does not expose these directly, so the API layer composes
// the two repos.
type composeClusterReader interface {
	Get(ctx context.Context, id string) (cluster.Cluster, error)
	ListByCluster(ctx context.Context, clusterID string) ([]instance.Instance, error)
}

// ComposeHandler serves downloadable docker-compose YAML reproducing how an
// instance or cluster is run.
type ComposeHandler struct {
	instances composeInstanceReader
	clusters  composeClusterReader
}

// NewComposeHandler builds a ComposeHandler from the instance + cluster readers.
func NewComposeHandler(instances composeInstanceReader, clusters composeClusterReader) *ComposeHandler {
	return &ComposeHandler{instances: instances, clusters: clusters}
}

// GetInstance returns a docker-compose document for a single instance.
func (h *ComposeHandler) GetInstance(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	inst, err := h.instances.Get(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	yaml := composegen.InstanceCompose(composegen.InstanceComposeInput{
		Name:      inst.Name,
		PGVersion: inst.PGVersion,
		Image:     inst.Image,
		Port:      5432,
		RepoType:  string(inst.RepoType),
		Superuser: inst.Superuser,
	})
	writeCompose(w, inst.Name, yaml)
}

// GetCluster returns a docker-compose document for a whole cluster (primary +
// replicas + router).
func (h *ComposeHandler) GetCluster(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	cl, err := h.clusters.Get(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}
	members, err := h.clusters.ListByCluster(r.Context(), id)
	if err != nil {
		respondError(w, err)
		return
	}

	// ListByCluster returns the primary first; pick the member flagged primary
	// (falling back to the first member), then treat the rest as replicas.
	in := composegen.ClusterComposeInput{
		Name:       cl.Name,
		Port:       5432,
		PgCatImage: "ghcr.io/postgresml/pgcat:latest",
		RouterPort: 6432,
	}
	primaryIdx := -1
	for i, m := range members {
		if m.Role == instance.RolePrimary {
			primaryIdx = i
			break
		}
	}
	if primaryIdx < 0 && len(members) > 0 {
		primaryIdx = 0
	}
	if primaryIdx >= 0 {
		p := members[primaryIdx]
		in.PrimaryName = p.Name
		in.PGVersion = p.PGVersion
		in.Image = p.Image
		in.RepoType = string(p.RepoType)
		in.Superuser = p.Superuser
	}
	for i, m := range members {
		if i == primaryIdx {
			continue
		}
		in.Replicas = append(in.Replicas, m.Name)
	}

	writeCompose(w, cl.Name, composegen.ClusterCompose(in))
}

// writeCompose writes the YAML body with download headers naming the file after
// the subject (<name>-compose.yml).
func writeCompose(w http.ResponseWriter, name, yaml string) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`-compose.yml"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(yaml))
}
