package failed_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue/failed"
)

const (
	tenant = "11111111-1111-4111-8111-111111111111"
	other  = "22222222-2222-4222-8222-222222222222"
)

func grantFor(t string) auth.Grant { return auth.SystemGrant("queue:failed", t) }

func provider(t *testing.T) *failed.FileFailedJobProvider {
	t.Helper()
	return failed.NewFileFailedJobProvider(filepath.Join(t.TempDir(), "failed.json"), 100)
}

func log(t *testing.T, p *failed.FileFailedJobProvider, tenantID, name string) string {
	t.Helper()
	id, err := p.Log(context.Background(), grantFor(tenantID), failed.FailedJob{
		Connection: "database",
		Queue:      "default",
		Name:       name,
		Payload:    []byte(`{"id":"i-1"}`),
		Exception:  "the broker refused",
	})
	if err != nil {
		t.Fatalf("Log: %v", err)
	}
	return id
}

func TestFileProviderKeepsWhatFailedAndFindsItAgain(t *testing.T) {
	ctx := context.Background()
	p := provider(t)
	id := log(t, p, tenant, "invoice.send")

	found, err := p.Find(ctx, grantFor(tenant), id)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found.Name != "invoice.send" || string(found.Payload) != `{"id":"i-1"}` {
		t.Errorf("the record came back as %+v", found)
	}
	if found.FailedAt.IsZero() {
		t.Error("the record has no failure time, so nothing can prune it")
	}

	all, err := p.All(ctx, grantFor(tenant))
	if err != nil || len(all) != 1 {
		t.Fatalf("All returned %d records (%v)", len(all), err)
	}
	ids, err := p.IDs(ctx, grantFor(tenant), "default")
	if err != nil || len(ids) != 1 || ids[0] != id {
		t.Errorf("IDs returned %v (%v)", ids, err)
	}
}

// TestFileProviderIsScopedByTenant is RULE 17 at a dead letter list: a failed
// job carries a customer's payload, and listing them is a read like any other.
func TestFileProviderIsScopedByTenant(t *testing.T) {
	ctx := context.Background()
	p := provider(t)
	mine := log(t, p, tenant, "invoice.send")
	log(t, p, other, "invoice.send")

	all, err := p.All(ctx, grantFor(tenant))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || all[0].ID != mine {
		t.Fatalf("one tenant saw %d records, want only its own", len(all))
	}

	// Finding another tenant's record by its id must not work either: the
	// filter is not a listing convenience, it is the boundary.
	theirs, err := p.All(ctx, grantFor(other))
	if err != nil || len(theirs) != 1 {
		t.Fatalf("the other tenant saw %d records (%v)", len(theirs), err)
	}
	if _, err := p.Find(ctx, grantFor(tenant), theirs[0].ID); !errors.Is(err, failed.ErrNotFound) {
		t.Errorf("one tenant read another's failed job: %v", err)
	}
	if forgotten, err := p.Forget(ctx, grantFor(tenant), theirs[0].ID); err != nil || forgotten {
		t.Errorf("one tenant deleted another's failed job: %v (%v)", forgotten, err)
	}
}

func TestFileProviderRefusesAGrantWithNoTenant(t *testing.T) {
	p := provider(t)
	if _, err := p.All(context.Background(), auth.Grant{}); !errors.Is(err, failed.ErrNoTenant) {
		t.Errorf("err = %v, want ErrNoTenant", err)
	}
}

func TestFileProviderForgetsAndPrunes(t *testing.T) {
	ctx := context.Background()
	p := provider(t)
	id := log(t, p, tenant, "invoice.send")

	forgotten, err := p.Forget(ctx, grantFor(tenant), id)
	if err != nil || !forgotten {
		t.Fatalf("Forget = %v (%v)", forgotten, err)
	}
	if forgotten, _ := p.Forget(ctx, grantFor(tenant), id); forgotten {
		t.Error("forgetting the same record twice reported a second deletion")
	}

	log(t, p, tenant, "invoice.send")
	removed, err := p.Prune(ctx, grantFor(tenant), time.Now().UTC().Add(time.Hour))
	if err != nil || removed != 1 {
		t.Fatalf("Prune took %d records (%v)", removed, err)
	}

	count, err := p.Count(ctx, grantFor(tenant), "", "")
	if err != nil || count != 0 {
		t.Errorf("Count = %d (%v) after pruning everything", count, err)
	}
}

// TestFileProviderKeepsOnlyTheNewest: a dead letter list that grows without
// bound on a disk nobody is watching is an outage filed as disk-full.
func TestFileProviderKeepsOnlyTheNewest(t *testing.T) {
	ctx := context.Background()
	p := failed.NewFileFailedJobProvider(filepath.Join(t.TempDir(), "failed.json"), 2)
	for range 5 {
		log(t, p, tenant, "invoice.send")
	}

	all, err := p.All(ctx, grantFor(tenant))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("the file holds %d records, want the limit of 2", len(all))
	}
}

func TestNullProviderKeepsNothingAndSaysSo(t *testing.T) {
	ctx := context.Background()
	var p failed.NullFailedJobProvider

	if _, err := p.Log(ctx, grantFor(tenant), failed.FailedJob{Name: "invoice.send"}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if all, err := p.All(ctx, grantFor(tenant)); err != nil || len(all) != 0 {
		t.Errorf("All returned %d records (%v)", len(all), err)
	}
	if _, err := p.Find(ctx, grantFor(tenant), "j-1"); !errors.Is(err, failed.ErrNotFound) {
		t.Errorf("Find returned %v, want ErrNotFound", err)
	}
	// It still refuses a Grant with no tenant, so wiring the null provider
	// does not quietly turn off the check the others make.
	if _, err := p.Log(ctx, auth.Grant{}, failed.FailedJob{}); !errors.Is(err, failed.ErrNoTenant) {
		t.Errorf("Log with no tenant returned %v", err)
	}
}
