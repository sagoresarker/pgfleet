//go:build integration

package audit

import (
	"context"
	"testing"
	"time"

	"github.com/sagoresarker/pgfleet/internal/testsupport"
)

func TestRecordThenListRoundTrip(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	rec := NewRecorder(pool)

	err := rec.Record(ctx, Entry{
		Actor:    "admin@example.com",
		Action:   "instance.create",
		Target:   "inst_42",
		Metadata: map[string]any{"region": "eu", "size": "small"},
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	entries, err := rec.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}

	got := entries[0]
	if got.Actor != "admin@example.com" || got.Action != "instance.create" || got.Target != "inst_42" {
		t.Errorf("entry fields = %+v", got)
	}
	if got.ID == "" {
		t.Error("ID should be populated by the database")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set by the database")
	}
	if got.Metadata["region"] != "eu" {
		t.Errorf("metadata not round-tripped: %+v", got.Metadata)
	}
}

func TestRecordValidationRejectsIncompleteEntry(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	rec := NewRecorder(pool)

	if err := rec.Record(ctx, Entry{Action: "missing.actor"}); err == nil {
		t.Error("Record should reject an entry without an actor")
	}
}

func TestListReturnsNewestFirst(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	rec := NewRecorder(pool)

	actions := []string{"first", "second", "third"}
	for _, a := range actions {
		if err := rec.Record(ctx, Entry{Actor: "system", Action: a}); err != nil {
			t.Fatalf("Record %s: %v", a, err)
		}
		time.Sleep(2 * time.Millisecond) // ensure distinct created_at ordering
	}

	entries, err := rec.List(ctx, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("len = %d, want 3", len(entries))
	}
	if entries[0].Action != "third" || entries[2].Action != "first" {
		t.Errorf("ordering wrong: %s, %s, %s", entries[0].Action, entries[1].Action, entries[2].Action)
	}
}

func TestListRespectsLimit(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	rec := NewRecorder(pool)

	for range 5 {
		if err := rec.Record(ctx, Entry{Actor: "system", Action: "tick"}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	entries, err := rec.List(ctx, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("limit not applied: len = %d, want 2", len(entries))
	}
}

func TestRecordDefaultsNilMetadataToEmptyObject(t *testing.T) {
	pool, _ := testsupport.MigratedPool(t)
	ctx := context.Background()
	rec := NewRecorder(pool)

	if err := rec.Record(ctx, Entry{Actor: "system", Action: "no.metadata"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	entries, err := rec.List(ctx, 1)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if entries[0].Metadata == nil {
		t.Error("Metadata should be a non-nil empty map, got nil")
	}
}
