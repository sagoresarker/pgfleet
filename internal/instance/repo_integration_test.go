//go:build integration

package instance

import (
	"context"
	"strings"
	"testing"

	"github.com/sagoresarker/pgfleet/internal/apperr"
	"github.com/sagoresarker/pgfleet/internal/secrets"
	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func newRepo(t *testing.T) *Repository {
	pool, _ := testsupport.MigratedPool(t)
	cipher, err := secrets.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return NewRepository(pool, cipher)
}

func TestCreateAppliesDefaultsAndEncryptsPassword(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	inst, err := repo.Create(ctx, NewInstance{Name: "orders-db", RepoType: RepoS3, Password: "super-secret-pw"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if inst.Image != DefaultImage || inst.PGVersion != "16" || inst.Superuser != "postgres" {
		t.Errorf("defaults not applied: %+v", inst)
	}
	if inst.Status != StatusProvisioning {
		t.Errorf("status = %q, want provisioning", inst.Status)
	}
	if inst.Stanza != "orders-db" {
		t.Errorf("stanza = %q, want orders-db", inst.Stanza)
	}

	// Password is retrievable (decrypts correctly) but never on the struct.
	pw, err := repo.Password(ctx, inst.ID)
	if err != nil {
		t.Fatalf("Password: %v", err)
	}
	if pw != "super-secret-pw" {
		t.Errorf("decrypted password = %q", pw)
	}
}

func TestCreatePersistsParametersAndExtensions(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	in := NewInstance{
		Name: "tuned-db", RepoType: RepoLocal, Password: "super-secret-pw",
		Parameters: map[string]string{"work_mem": "8MB", "random_page_cost": "1.1"},
		Extensions: []string{"pg_trgm", "citext"},
	}
	created, err := repo.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Parameters["work_mem"] != "8MB" || created.Parameters["random_page_cost"] != "1.1" {
		t.Errorf("parameters on create = %v", created.Parameters)
	}
	if len(created.Extensions) != 2 {
		t.Errorf("extensions on create = %v", created.Extensions)
	}

	// Round-trip via Get (JSONB + TEXT[] scan).
	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Parameters["work_mem"] != "8MB" {
		t.Errorf("parameters after Get = %v", got.Parameters)
	}
	if len(got.Extensions) != 2 || got.Extensions[0] != "pg_trgm" {
		t.Errorf("extensions after Get = %v", got.Extensions)
	}

	// Empty config round-trips as non-nil empties.
	plain, err := repo.Create(ctx, NewInstance{Name: "plain-db", RepoType: RepoLocal, Password: "super-secret-pw"})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Parameters == nil || len(plain.Parameters) != 0 {
		t.Errorf("empty parameters = %v, want non-nil empty", plain.Parameters)
	}
	if plain.Extensions == nil || len(plain.Extensions) != 0 {
		t.Errorf("empty extensions = %v, want non-nil empty", plain.Extensions)
	}
}

// TestCreateRejectsInvalidConfig — platform-owned GUC and non-allowlisted
// extension are rejected at the repository boundary.
func TestCreateRejectsInvalidConfig(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	_, err := repo.Create(ctx, NewInstance{
		Name: "bad1", RepoType: RepoLocal, Password: "super-secret-pw",
		Parameters: map[string]string{"wal_level": "minimal"},
	})
	if apperr.Kind(err) != apperr.KindInvalid {
		t.Errorf("platform-owned GUC should be rejected, got %v", apperr.Kind(err))
	}

	_, err = repo.Create(ctx, NewInstance{
		Name: "bad2", RepoType: RepoLocal, Password: "super-secret-pw",
		Extensions: []string{"plpython3u"},
	})
	if apperr.Kind(err) != apperr.KindInvalid {
		t.Errorf("non-allowlisted extension should be rejected, got %v", apperr.Kind(err))
	}
}

func TestPasswordIsEncryptedAtRest(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	inst, _ := repo.Create(ctx, NewInstance{Name: "secure-db", RepoType: RepoLocal, Password: "plaintext-leak-check"})

	var blob []byte
	if err := repo.pool.QueryRow(ctx, `SELECT superuser_secret FROM instances WHERE id = $1`, inst.ID).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	if len(blob) == 0 {
		t.Fatal("superuser_secret is empty")
	}
	if strings.Contains(string(blob), "plaintext-leak-check") {
		t.Error("password stored in plaintext at rest")
	}
}

func TestCreateDuplicateNameConflicts(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	_, _ = repo.Create(ctx, NewInstance{Name: "dup", RepoType: RepoS3, Password: "password-aa"})
	_, err := repo.Create(ctx, NewInstance{Name: "dup", RepoType: RepoS3, Password: "password-bb"})
	if apperr.Kind(err) != apperr.KindConflict {
		t.Errorf("kind = %v, want KindConflict", apperr.Kind(err))
	}
}

func TestGetByNameAndList(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	_, _ = repo.Create(ctx, NewInstance{Name: "a-db", RepoType: RepoS3, Password: "password-aa"})
	_, _ = repo.Create(ctx, NewInstance{Name: "b-db", RepoType: RepoLocal, Password: "password-bb"})

	got, err := repo.GetByName(ctx, "a-db")
	if err != nil || got.Name != "a-db" {
		t.Fatalf("GetByName: %+v err %v", got, err)
	}
	all, err := repo.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Errorf("List len = %d, want 2", len(all))
	}
}

func TestSetStatusAndRuntime(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	inst, _ := repo.Create(ctx, NewInstance{Name: "lifecycle-db", RepoType: RepoS3, Password: "password-aa"})

	if err := repo.SetRuntime(ctx, inst.ID, "container-xyz", 54321); err != nil {
		t.Fatalf("SetRuntime: %v", err)
	}
	if err := repo.SetStatus(ctx, inst.ID, StatusRunning, ""); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	got, _ := repo.Get(ctx, inst.ID)
	if got.Status != StatusRunning || got.ContainerID != "container-xyz" || got.HostPort != 54321 {
		t.Errorf("runtime/status not persisted: %+v", got)
	}

	if err := repo.SetStatus(ctx, inst.ID, StatusError, "boom"); err != nil {
		t.Fatal(err)
	}
	got, _ = repo.Get(ctx, inst.ID)
	if got.Status != StatusError || got.LastError != "boom" {
		t.Errorf("error status not persisted: %+v", got)
	}
}

func TestGetMissingIsNotFound(t *testing.T) {
	repo := newRepo(t)
	missing := "00000000-0000-0000-0000-000000000000"
	if _, err := repo.Get(context.Background(), missing); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("kind = %v, want KindNotFound", apperr.Kind(err))
	}
}

func TestDelete(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	inst, _ := repo.Create(ctx, NewInstance{Name: "delete-db", RepoType: RepoS3, Password: "password-aa"})
	if err := repo.Delete(ctx, inst.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, inst.ID); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("after delete kind = %v, want NotFound", apperr.Kind(err))
	}
	if err := repo.Delete(ctx, inst.ID); apperr.Kind(err) != apperr.KindNotFound {
		t.Errorf("delete missing kind = %v, want NotFound", apperr.Kind(err))
	}
}
