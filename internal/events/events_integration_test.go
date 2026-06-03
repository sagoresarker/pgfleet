//go:build integration

package events

import (
	"context"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func seedCluster(t *testing.T, ctx context.Context, store *Store, name string) string {
	t.Helper()
	var id string
	err := store.pool.QueryRow(ctx,
		`INSERT INTO clusters (name) VALUES ($1) RETURNING id::text`, name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed cluster: %v", err)
	}
	return id
}

func seedInstanceID(t *testing.T, ctx context.Context, store *Store, name string) string {
	t.Helper()
	var id string
	err := store.pool.QueryRow(ctx,
		`INSERT INTO instances (name, image, stanza)
		 VALUES ($1, 'postgres:16', $1) RETURNING id::text`, name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	return id
}

func TestRecordThenListRoundTrip(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	st := NewStore(pool)

	instID := seedInstanceID(t, ctx, st, "rt-instance")

	rec, err := st.Record(ctx, NewEvent{
		InstanceID: instID,
		Type:       "status_change",
		Message:    "provisioning -> running",
		Metadata:   map[string]string{"from": "provisioning", "to": "running"},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if rec.ID == "" {
		t.Error("ID should be populated by the database")
	}
	if rec.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set by the database")
	}
	if rec.InstanceID != instID {
		t.Errorf("InstanceID = %q, want %q", rec.InstanceID, instID)
	}

	got, err := st.List(ctx, Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Type != "status_change" || got[0].Message != "provisioning -> running" {
		t.Errorf("event fields = %+v", got[0])
	}
	if got[0].Metadata["from"] != "provisioning" || got[0].Metadata["to"] != "running" {
		t.Errorf("metadata not round-tripped: %+v", got[0].Metadata)
	}
}

func TestRecordValidationRejectsIncompleteEvent(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	st := NewStore(pool)

	if _, err := st.Record(ctx, NewEvent{Message: "missing type"}); err == nil {
		t.Error("Record should reject an event without a type")
	}
	if _, err := st.Record(ctx, NewEvent{Type: "lifecycle"}); err == nil {
		t.Error("Record should reject an event without a message")
	}
}

func TestFilterByInstanceClusterType(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	st := NewStore(pool)

	instA := seedInstanceID(t, ctx, st, "filter-inst-a")
	instB := seedInstanceID(t, ctx, st, "filter-inst-b")
	clus := seedCluster(t, ctx, st, "filter-cluster")

	mustRecord(t, st, NewEvent{InstanceID: instA, Type: "provisioning", Message: "a-prov"})
	mustRecord(t, st, NewEvent{InstanceID: instA, Type: "status_change", Message: "a-status"})
	mustRecord(t, st, NewEvent{InstanceID: instB, Type: "provisioning", Message: "b-prov"})
	mustRecord(t, st, NewEvent{ClusterID: clus, Type: "lifecycle", Message: "c-life"})

	// Filter by instance.
	byInst, err := st.List(ctx, Filter{InstanceID: instA})
	if err != nil {
		t.Fatalf("List by instance: %v", err)
	}
	if len(byInst) != 2 {
		t.Fatalf("by instance len = %d, want 2", len(byInst))
	}
	for _, e := range byInst {
		if e.InstanceID != instA {
			t.Errorf("unexpected instance %q in instance filter", e.InstanceID)
		}
	}

	// Filter by cluster.
	byClus, err := st.List(ctx, Filter{ClusterID: clus})
	if err != nil {
		t.Fatalf("List by cluster: %v", err)
	}
	if len(byClus) != 1 || byClus[0].Message != "c-life" {
		t.Fatalf("by cluster = %+v, want one c-life", byClus)
	}

	// Filter by type.
	byType, err := st.List(ctx, Filter{Type: "provisioning"})
	if err != nil {
		t.Fatalf("List by type: %v", err)
	}
	if len(byType) != 2 {
		t.Fatalf("by type len = %d, want 2", len(byType))
	}

	// Combined instance + type.
	combo, err := st.List(ctx, Filter{InstanceID: instA, Type: "provisioning"})
	if err != nil {
		t.Fatalf("List combined: %v", err)
	}
	if len(combo) != 1 || combo[0].Message != "a-prov" {
		t.Fatalf("combined = %+v, want one a-prov", combo)
	}
}

func TestListLimitBoundaries(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	st := NewStore(pool)

	inst := seedInstanceID(t, ctx, st, "limit-inst")
	for i := 0; i < 5; i++ {
		mustRecord(t, st, NewEvent{InstanceID: inst, Type: "tick", Message: "t"})
	}

	// 0 -> default (100), all 5 returned.
	def, err := st.List(ctx, Filter{Limit: 0})
	if err != nil {
		t.Fatalf("List default: %v", err)
	}
	if len(def) != 5 {
		t.Errorf("default limit len = %d, want 5", len(def))
	}

	// Negative -> default.
	neg, err := st.List(ctx, Filter{Limit: -10})
	if err != nil {
		t.Fatalf("List negative: %v", err)
	}
	if len(neg) != 5 {
		t.Errorf("negative limit len = %d, want 5", len(neg))
	}

	// Explicit small limit honored.
	two, err := st.List(ctx, Filter{Limit: 2})
	if err != nil {
		t.Fatalf("List limit 2: %v", err)
	}
	if len(two) != 2 {
		t.Errorf("limit 2 len = %d, want 2", len(two))
	}

	// Over-cap requests are capped without error (we cannot easily insert 500+
	// rows cheaply, so we just assert the call succeeds and returns our rows).
	capped, err := st.List(ctx, Filter{Limit: 100000})
	if err != nil {
		t.Fatalf("List over cap: %v", err)
	}
	if len(capped) != 5 {
		t.Errorf("over-cap len = %d, want 5", len(capped))
	}
}

func TestNilVsPopulatedMetadataRoundTrip(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	st := NewStore(pool)

	inst := seedInstanceID(t, ctx, st, "meta-inst")

	// Nil metadata -> stored as '{}' and read back as non-nil empty map.
	nilRec, err := st.Record(ctx, NewEvent{InstanceID: inst, Type: "lifecycle", Message: "no-meta"})
	if err != nil {
		t.Fatalf("Record nil meta: %v", err)
	}
	if nilRec.Metadata == nil {
		t.Error("Record should return non-nil empty metadata")
	}
	if len(nilRec.Metadata) != 0 {
		t.Errorf("nil metadata should be empty, got %+v", nilRec.Metadata)
	}

	// Populated metadata round-trips intact.
	popRec, err := st.Record(ctx, NewEvent{
		InstanceID: inst,
		Type:       "health_transition",
		Message:    "with-meta",
		Metadata:   map[string]string{"k1": "v1", "k2": "v2"},
	})
	if err != nil {
		t.Fatalf("Record populated meta: %v", err)
	}
	if popRec.Metadata["k1"] != "v1" || popRec.Metadata["k2"] != "v2" {
		t.Errorf("populated metadata mismatch: %+v", popRec.Metadata)
	}

	got, err := st.List(ctx, Filter{InstanceID: inst})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, e := range got {
		if e.Metadata == nil {
			t.Errorf("listed event %q has nil metadata", e.Message)
		}
	}
}

func TestListOrderingTiebreak(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	st := NewStore(pool)

	inst := seedInstanceID(t, ctx, st, "order-inst")

	// Distinct created_at: newest first.
	mustRecord(t, st, NewEvent{InstanceID: inst, Type: "lifecycle", Message: "first"})
	time.Sleep(3 * time.Millisecond)
	mustRecord(t, st, NewEvent{InstanceID: inst, Type: "lifecycle", Message: "second"})
	time.Sleep(3 * time.Millisecond)
	mustRecord(t, st, NewEvent{InstanceID: inst, Type: "lifecycle", Message: "third"})

	got, err := st.List(ctx, Filter{InstanceID: inst})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Message != "third" || got[2].Message != "first" {
		t.Errorf("ordering wrong: %s, %s, %s", got[0].Message, got[1].Message, got[2].Message)
	}

	// Tiebreak: insert many rows sharing one fixed created_at and assert the
	// result is sorted by id DESC (stable, deterministic ordering).
	fixed := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if _, err := st.pool.Exec(ctx,
			`INSERT INTO events (instance_id, type, message, created_at)
			 VALUES ($1, 'tie', $2, $3)`,
			inst, "tie", fixed,
		); err != nil {
			t.Fatalf("insert tie row: %v", err)
		}
	}
	ties, err := st.List(ctx, Filter{InstanceID: inst, Type: "tie"})
	if err != nil {
		t.Fatalf("List ties: %v", err)
	}
	if len(ties) != 5 {
		t.Fatalf("ties len = %d, want 5", len(ties))
	}
	for i := 1; i < len(ties); i++ {
		if ties[i-1].ID < ties[i].ID {
			t.Errorf("tiebreak not id DESC: %q before %q", ties[i-1].ID, ties[i].ID)
		}
	}
}

func mustRecord(t *testing.T, st *Store, ne NewEvent) Event {
	t.Helper()
	ev, err := st.Record(context.Background(), ne)
	if err != nil {
		t.Fatalf("Record(%+v): %v", ne, err)
	}
	return ev
}
