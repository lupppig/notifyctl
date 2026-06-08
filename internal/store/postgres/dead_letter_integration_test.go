//go:build integration

// Integration tests for the Postgres DeadLetterStore. Run with:
//
//	go test -tags=integration ./internal/store/postgres/...
//
// Requires a reachable DATABASE_URL; tests t.Skip otherwise.
package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lupppig/notifyctl/internal/domain"
	"github.com/lupppig/notifyctl/internal/store"
	"github.com/lupppig/notifyctl/internal/store/postgres"
	"github.com/lupppig/notifyctl/internal/store/postgres/testdb"
)

func newDLStore(t *testing.T) (*postgres.DeadLetterStore, *postgres.DB) {
	db := testdb.Open(t)
	testdb.Truncate(t, db)
	testdb.SeedService(t, db, "svc-1", "")
	testdb.SeedService(t, db, "svc-2", "")
	return postgres.NewDeadLetterStore(db), db
}

func mkDL(id, nid, svc string, failedAt time.Time) *domain.DeadLetter {
	return &domain.DeadLetter{
		ID:             id,
		NotificationID: nid,
		ServiceID:      svc,
		Payload:        []byte(`{"event":"x"}`),
		LastError:      "boom",
		AttemptCount:   5,
		FailedAt:       failedAt,
	}
}

func TestDeadLetterCreateIdempotentOnConflict(t *testing.T) {
	s, db := newDLStore(t)
	ctx := context.Background()
	now := time.Now()

	first := mkDL("dl-A", "n-1", "svc-1", now)
	if err := s.Create(ctx, first); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Same notification_id, different row id: ON CONFLICT (notification_id) DO NOTHING.
	second := mkDL("dl-B", "n-1", "svc-1", now)
	if err := s.Create(ctx, second); err != nil {
		t.Fatalf("second create should be a no-op, got: %v", err)
	}

	if n := testdb.CountDeadLetters(t, db, "n-1"); n != 1 {
		t.Fatalf("expected exactly 1 row for n-1, got %d", n)
	}
	// The first insert wins.
	got, err := s.GetByID(ctx, "dl-A")
	if err != nil {
		t.Fatalf("expected first row to persist: %v", err)
	}
	if got.ID != "dl-A" {
		t.Errorf("expected dl-A to win the conflict, got %q", got.ID)
	}
}

func TestDeadLetterGetByIDFoundRoundTrips(t *testing.T) {
	s, _ := newDLStore(t)
	ctx := context.Background()
	failedAt := time.Now().Truncate(time.Microsecond) // pg stores microsecond precision

	in := &domain.DeadLetter{
		ID: "dl-1", NotificationID: "n-1", ServiceID: "svc-1",
		Payload: []byte(`{"k":"v"}`), LastError: "HTTP 500", AttemptCount: 7, FailedAt: failedAt,
	}
	if err := s.Create(ctx, in); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetByID(ctx, "dl-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.NotificationID != "n-1" || got.ServiceID != "svc-1" || got.LastError != "HTTP 500" || got.AttemptCount != 7 {
		t.Errorf("fields mismatch: %+v", got)
	}
	if !bytes.Equal(got.Payload, []byte(`{"k":"v"}`)) {
		t.Errorf("payload mismatch: %s", got.Payload)
	}
	if !got.FailedAt.Equal(failedAt) {
		t.Errorf("failed_at mismatch: want %v got %v", failedAt, got.FailedAt)
	}
}

func TestDeadLetterGetByIDNotFound(t *testing.T) {
	s, _ := newDLStore(t)
	_, err := s.GetByID(context.Background(), "does-not-exist")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("expected store.ErrNotFound, got %v", err)
	}
}

func TestDeadLetterListOrderingFailedAtDesc(t *testing.T) {
	s, _ := newDLStore(t)
	ctx := context.Background()
	base := time.Now().Truncate(time.Second)

	_ = s.Create(ctx, mkDL("dl-old", "n-old", "svc-1", base.Add(-2*time.Hour)))
	_ = s.Create(ctx, mkDL("dl-mid", "n-mid", "svc-1", base.Add(-1*time.Hour)))
	_ = s.Create(ctx, mkDL("dl-new", "n-new", "svc-1", base))

	got, err := s.List(ctx, "svc-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3, got %d", len(got))
	}
	if got[0].ID != "dl-new" || got[1].ID != "dl-mid" || got[2].ID != "dl-old" {
		t.Errorf("expected newest-first ordering, got %s,%s,%s", got[0].ID, got[1].ID, got[2].ID)
	}
}

func TestDeadLetterListLimit100(t *testing.T) {
	s, _ := newDLStore(t)
	ctx := context.Background()
	base := time.Now()
	for i := 0; i < 101; i++ {
		dl := mkDL(idN("dl", i), idN("n", i), "svc-1", base.Add(time.Duration(i)*time.Second))
		if err := s.Create(ctx, dl); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	got, err := s.List(ctx, "svc-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 100 {
		t.Errorf("expected List capped at 100, got %d", len(got))
	}
}

func TestDeadLetterListServiceScoping(t *testing.T) {
	s, _ := newDLStore(t)
	ctx := context.Background()
	now := time.Now()
	_ = s.Create(ctx, mkDL("dl-1", "n-1", "svc-1", now))
	_ = s.Create(ctx, mkDL("dl-2", "n-2", "svc-2", now))

	only1, _ := s.List(ctx, "svc-1")
	if len(only1) != 1 || only1[0].ServiceID != "svc-1" {
		t.Errorf("expected only svc-1 rows, got %+v", only1)
	}
	all, _ := s.List(ctx, "")
	if len(all) != 2 {
		t.Errorf("expected empty filter to return all, got %d", len(all))
	}
}

func TestDeadLetterListNullServiceIDAndLastError(t *testing.T) {
	_, db := newDLStore(t)
	s := postgres.NewDeadLetterStore(db)
	ctx := context.Background()

	// Insert a row with NULL service_id and NULL last_error directly.
	testdb.InsertDeadLetter(t, db, &domain.DeadLetter{
		ID: "dl-null", NotificationID: "n-null", ServiceID: "", LastError: "",
		Payload: []byte(`{}`), AttemptCount: 1, FailedAt: time.Now(),
	})

	got, err := s.GetByID(ctx, "dl-null")
	if err != nil {
		t.Fatalf("get with NULL fields: %v", err)
	}
	if got.ServiceID != "" || got.LastError != "" {
		t.Errorf("expected empty strings for NULL columns, got service=%q lastErr=%q", got.ServiceID, got.LastError)
	}
	// List with empty filter must include it without panicking on NULL scans.
	all, err := s.List(ctx, "")
	if err != nil {
		t.Fatalf("list with NULL rows: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 row, got %d", len(all))
	}
}

func TestDeadLetterDeleteByNotificationID(t *testing.T) {
	s, db := newDLStore(t)
	ctx := context.Background()
	now := time.Now()
	_ = s.Create(ctx, mkDL("dl-1", "n-1", "svc-1", now))
	_ = s.Create(ctx, mkDL("dl-2", "n-2", "svc-1", now))

	if err := s.DeleteByNotificationID(ctx, "n-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if testdb.CountDeadLetters(t, db, "n-1") != 0 {
		t.Error("expected n-1 deleted")
	}
	if testdb.CountDeadLetters(t, db, "n-2") != 1 {
		t.Error("expected n-2 to remain")
	}
	// Deleting a missing notification_id is a no-op, not an error.
	if err := s.DeleteByNotificationID(ctx, "nonexistent"); err != nil {
		t.Errorf("delete of missing id should be a no-op, got %v", err)
	}
}

func TestDeadLetterEmptyAndLargePayload(t *testing.T) {
	s, _ := newDLStore(t)
	ctx := context.Background()
	now := time.Now()

	empty := &domain.DeadLetter{ID: "dl-empty", NotificationID: "n-empty", ServiceID: "svc-1", Payload: []byte{}, AttemptCount: 0, FailedAt: now}
	if err := s.Create(ctx, empty); err != nil {
		t.Fatalf("create empty payload: %v", err)
	}
	gotEmpty, err := s.GetByID(ctx, "dl-empty")
	if err != nil {
		t.Fatalf("get empty: %v", err)
	}
	if len(gotEmpty.Payload) != 0 {
		t.Errorf("expected empty payload, got %d bytes", len(gotEmpty.Payload))
	}

	big := bytes.Repeat([]byte("a"), 1<<20) // 1 MiB
	large := &domain.DeadLetter{ID: "dl-large", NotificationID: "n-large", ServiceID: "svc-1", Payload: big, AttemptCount: 0, FailedAt: now}
	if err := s.Create(ctx, large); err != nil {
		t.Fatalf("create large payload: %v", err)
	}
	gotLarge, err := s.GetByID(ctx, "dl-large")
	if err != nil {
		t.Fatalf("get large: %v", err)
	}
	if !bytes.Equal(gotLarge.Payload, big) {
		t.Errorf("large payload not byte-exact (got %d bytes)", len(gotLarge.Payload))
	}
}

func TestDeadLetterUnicodeLastError(t *testing.T) {
	s, _ := newDLStore(t)
	ctx := context.Background()
	special := `错误 💥 "quoted" \backslash 中文`
	dl := &domain.DeadLetter{ID: "dl-u", NotificationID: "n-u", ServiceID: "svc-1", Payload: []byte(`{}`), LastError: special, AttemptCount: 3, FailedAt: time.Now()}
	if err := s.Create(ctx, dl); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.GetByID(ctx, "dl-u")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.LastError != special {
		t.Errorf("unicode last_error mismatch: %q", got.LastError)
	}
}

// idN returns prefix + zero-padded index for stable distinct IDs.
func idN(prefix string, i int) string {
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteByte('-')
	// simple base-10 without fmt to keep imports lean
	if i == 0 {
		b.WriteByte('0')
	} else {
		var digits []byte
		for i > 0 {
			digits = append([]byte{byte('0' + i%10)}, digits...)
			i /= 10
		}
		b.Write(digits)
	}
	return b.String()
}
