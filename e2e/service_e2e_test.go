//go:build e2e

package e2e

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notifyv1 "github.com/lupppig/notifyctl/pkg/grpc/notify/v1"
)

// TestE2EServiceSoftDeleteRevokesAuth: after a service is deleted over gRPC, its
// API key must stop authenticating and it must vanish from ListServices, while
// the row is only soft-deleted.
func TestE2EServiceSoftDeleteRevokesAuth(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-softdelete-auth")

	// Key works before delete (an authenticated RPC succeeds).
	if _, err := h.client.ListDeadLetters(authCtx(key), &notifyv1.ListDeadLettersRequest{}); err != nil {
		t.Fatalf("authenticated call before delete should succeed: %v", err)
	}

	if _, err := h.client.DeleteService(authCtx(key), &notifyv1.DeleteServiceRequest{Id: svcID}); err != nil {
		t.Fatalf("delete service: %v", err)
	}

	// Auth is revoked: the same key now fails.
	if _, err := h.client.ListDeadLetters(authCtx(key), &notifyv1.ListDeadLettersRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Errorf("expected Unauthenticated with deleted service's key, got %v", err)
	}

	// Gone from the listing (ListServices is auth-exempt).
	resp, err := h.client.ListServices(context.Background(), &notifyv1.ListServicesRequest{})
	if err != nil {
		t.Fatalf("list services: %v", err)
	}
	for _, s := range resp.Services {
		if s.Id == svcID {
			t.Errorf("deleted service %s must not appear in ListServices", svcID)
		}
	}

	// Row is soft-deleted, not removed.
	var present bool
	if err := h.db.Pool.QueryRow(context.Background(),
		`SELECT deleted_at IS NOT NULL FROM services WHERE id = $1`, svcID).Scan(&present); err != nil {
		t.Fatalf("service row must still exist after soft delete: %v", err)
	}
	if !present {
		t.Error("expected deleted_at set on the service row")
	}
}

// TestE2EDeleteServiceWithHistorySucceeds is the regression test for the original
// bug: deleting a service that has notifications/jobs/dead-letters used to fail
// with a foreign-key violation. Soft delete must succeed and retain the history.
func TestE2EDeleteServiceWithHistorySucceeds(t *testing.T) {
	h := newHarness(t)
	svcID, key := h.registerService("e2e-delete-history")

	// Generate real history: a delivered notification that then dead-letters.
	nid := h.send(key, svcID, `{"event":"history"}`)
	h.waitFor("delivery", func() bool { return h.jobStatus(nid) == "DELIVERED" })
	h.exhaustRetries(nid)
	h.waitForDeadLetter(nid)

	// The delete that previously hit a 23503 FK violation.
	if _, err := h.client.DeleteService(authCtx(key), &notifyv1.DeleteServiceRequest{Id: svcID}); err != nil {
		t.Fatalf("delete service with history must succeed, got: %v", err)
	}

	// History is preserved (FK rows survive the soft delete).
	if n := h.deadLetterCount(nid); n != 1 {
		t.Errorf("dead letter must survive service delete, got %d", n)
	}
	var jobs int
	if err := h.db.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM notification_jobs WHERE service_id = $1`, svcID).Scan(&jobs); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if jobs != 1 {
		t.Errorf("notification_jobs must survive service delete, got %d", jobs)
	}
}

// TestE2EDeleteNonexistentServiceNotFound: deleting an unknown id returns
// NotFound rather than a silent success or an opaque Internal error.
func TestE2EDeleteNonexistentServiceNotFound(t *testing.T) {
	h := newHarness(t)
	_, key := h.registerService("e2e-delete-missing")

	if _, err := h.client.DeleteService(authCtx(key), &notifyv1.DeleteServiceRequest{Id: "no-such-service"}); status.Code(err) != codes.NotFound {
		t.Errorf("expected NotFound for unknown service id, got %v", err)
	}
}

// TestE2EReRegisterAfterDelete: a soft-deleted service's name is freed, so a new
// service can register under it (partial unique index) and gets a fresh id/key.
func TestE2EReRegisterAfterDelete(t *testing.T) {
	h := newHarness(t)
	id1, key1 := h.registerService("e2e-reuse-name")

	if _, err := h.client.DeleteService(authCtx(key1), &notifyv1.DeleteServiceRequest{Id: id1}); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Re-register the same name — must succeed with a distinct identity.
	id2, key2 := h.registerService("e2e-reuse-name")
	if id2 == id1 {
		t.Errorf("re-registered service should have a fresh id, got same %s", id1)
	}
	if key2 == key1 {
		t.Error("re-registered service should have a fresh api key")
	}

	// New key authenticates; old key remains revoked.
	if _, err := h.client.ListDeadLetters(authCtx(key2), &notifyv1.ListDeadLettersRequest{}); err != nil {
		t.Errorf("new service's key should authenticate: %v", err)
	}
	if _, err := h.client.ListDeadLetters(authCtx(key1), &notifyv1.ListDeadLettersRequest{}); status.Code(err) != codes.Unauthenticated {
		t.Errorf("old deleted key must stay revoked, got %v", err)
	}
}
