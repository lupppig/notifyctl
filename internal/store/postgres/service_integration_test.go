//go:build integration

// Integration tests for the Postgres ServiceStore soft-delete behavior. Run with:
//
//	go test -tags=integration ./internal/store/postgres/...
//
// Requires a reachable DATABASE_URL; tests t.Skip otherwise.
package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/store"
	"github.com/lupppig/notifyctl/internal/store/postgres"
	"github.com/lupppig/notifyctl/internal/store/postgres/testdb"
)

func newServiceStore(t *testing.T) (*postgres.ServiceStore, *postgres.DB) {
	db := testdb.Open(t)
	testdb.Truncate(t, db)
	return postgres.NewServiceStore(db), db
}

func mkService(id, name, apiKey string) *domain.Service {
	return &domain.Service{
		ID:         id,
		Name:       name,
		WebhookURL: "http://example.test/hook",
		Secret:     "secret-" + id,
		APIKey:     apiKey,
	}
}

// TestServiceSoftDeleteKeepsRow verifies Delete marks deleted_at instead of
// removing the row.
func TestServiceSoftDeleteKeepsRow(t *testing.T) {
	s, db := newServiceStore(t)
	ctx := context.Background()

	if err := s.Create(ctx, mkService("svc-1", "acme", "key-1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Delete(ctx, "svc-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// The physical row must still exist with deleted_at set.
	var deleted bool
	if err := db.Pool.QueryRow(ctx,
		`SELECT deleted_at IS NOT NULL FROM services WHERE id = $1`, "svc-1").Scan(&deleted); err != nil {
		t.Fatalf("row must still exist after soft delete: %v", err)
	}
	if !deleted {
		t.Error("expected deleted_at to be set after Delete")
	}
}

// TestServiceListExcludesDeleted verifies List filters out soft-deleted rows.
func TestServiceListExcludesDeleted(t *testing.T) {
	s, _ := newServiceStore(t)
	ctx := context.Background()

	if err := s.Create(ctx, mkService("svc-live", "live", "key-live")); err != nil {
		t.Fatalf("create live: %v", err)
	}
	if err := s.Create(ctx, mkService("svc-dead", "dead", "key-dead")); err != nil {
		t.Fatalf("create dead: %v", err)
	}
	if err := s.Delete(ctx, "svc-dead"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	got, err := s.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "svc-live" {
		t.Fatalf("expected only svc-live in list, got %+v", got)
	}
}

// TestServiceSoftDeleteRevokesAuth is the security invariant: a deleted
// service's API key must no longer resolve via GetByAPIKeyHash.
func TestServiceSoftDeleteRevokesAuth(t *testing.T) {
	s, _ := newServiceStore(t)
	ctx := context.Background()

	if err := s.Create(ctx, mkService("svc-1", "acme", "hash-1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Resolvable while live.
	if got, err := s.GetByAPIKeyHash(ctx, "hash-1"); err != nil || got == nil {
		t.Fatalf("expected key to resolve while live, got %v / %v", got, err)
	}

	if err := s.Delete(ctx, "svc-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Revoked after delete: lookup must fail (no row).
	if _, err := s.GetByAPIKeyHash(ctx, "hash-1"); err == nil {
		t.Error("deleted service's API key must not resolve")
	}
}

// TestServiceNameReuseAfterDelete verifies the partial unique index frees a
// soft-deleted name/api_key for reuse, while a live duplicate still conflicts.
func TestServiceNameReuseAfterDelete(t *testing.T) {
	s, _ := newServiceStore(t)
	ctx := context.Background()

	if err := s.Create(ctx, mkService("svc-1", "acme", "key-1")); err != nil {
		t.Fatalf("create first: %v", err)
	}

	// Live duplicate name is rejected.
	err := s.Create(ctx, mkService("svc-dup", "acme", "key-dup"))
	if !errors.Is(err, store.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists for live duplicate name, got %v", err)
	}

	// After soft delete, the name (and api_key) are free for a fresh service.
	if err := s.Delete(ctx, "svc-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := s.Create(ctx, mkService("svc-2", "acme", "key-1")); err != nil {
		t.Fatalf("re-create with reused name+key after delete should succeed, got %v", err)
	}

	// And only the new live one is listed.
	got, err := s.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "svc-2" {
		t.Fatalf("expected only svc-2 live, got %+v", got)
	}
}

// TestServiceDeleteUnknownIDNotFound verifies deleting an unknown or
// already-deleted id reports ErrNotFound (the old hard DELETE silently
// succeeded).
func TestServiceDeleteUnknownIDNotFound(t *testing.T) {
	s, _ := newServiceStore(t)
	ctx := context.Background()

	if err := s.Delete(ctx, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown id, got %v", err)
	}

	if err := s.Create(ctx, mkService("svc-1", "acme", "key-1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Delete(ctx, "svc-1"); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	// Second delete of an already-deleted service is also NotFound.
	if err := s.Delete(ctx, "svc-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second delete, got %v", err)
	}
}

// TestServiceMigrationIdempotentSoftDelete runs Migrate several extra times
// (ADD COLUMN / DROP CONSTRAINT / CREATE INDEX must all be idempotent) and
// asserts the partial unique indexes exist.
func TestServiceMigrationIdempotentSoftDelete(t *testing.T) {
	_, db := newServiceStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := db.Migrate(ctx); err != nil {
			t.Fatalf("migrate round %d: %v", i, err)
		}
	}

	for _, idx := range []string{"idx_services_name_live", "idx_services_api_key_live"} {
		var exists bool
		if err := db.Pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE indexname = $1)`, idx).Scan(&exists); err != nil {
			t.Fatalf("index lookup %s: %v", idx, err)
		}
		if !exists {
			t.Errorf("expected partial unique index %s to exist", idx)
		}
	}
}
