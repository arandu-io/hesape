package database_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// invoice is the smallest entity a repository can own.
type invoice struct {
	ID     string
	Tenant string
}

// invoiceRepo is a repository over a slice, and it is here to prove two things
// at once: that the contract can be satisfied at all, and that the only way to
// call it is with a Grant.
type invoiceRepo struct {
	rows []invoice
}

func (r *invoiceRepo) Find(_ context.Context, g auth.Grant, id string) (invoice, error) {
	if err := g.Check("invoice.view"); err != nil {
		return invoice{}, err
	}
	for _, row := range r.rows {
		if row.ID == id && row.Tenant == auth.Tenant(g) {
			return row, nil
		}
	}
	return invoice{}, fmt.Errorf("%w: invoice %s", database.ErrNotFound, id)
}

func (r *invoiceRepo) List(_ context.Context, g auth.Grant, q database.Query) (database.Page[invoice], error) {
	if err := g.Check("invoice.view"); err != nil {
		return database.Page[invoice]{}, err
	}

	var page database.Page[invoice]
	start := 0
	if q.Cursor != "" {
		for i, row := range r.rows {
			if row.ID == q.Cursor {
				start = i + 1
				break
			}
		}
	}
	for _, row := range r.rows[start:] {
		if row.Tenant != auth.Tenant(g) {
			continue
		}
		if q.Limit > 0 && len(page.Items) == q.Limit {
			page.Next = page.Items[len(page.Items)-1].ID
			return page, nil
		}
		page.Items = append(page.Items, row)
	}
	return page, nil
}

func (r *invoiceRepo) Create(_ context.Context, _ auth.Grant, e invoice) (invoice, error) {
	return e, nil
}

func (r *invoiceRepo) Update(_ context.Context, _ auth.Grant, e invoice) (invoice, error) {
	return e, nil
}

func (r *invoiceRepo) Delete(_ context.Context, _ auth.Grant, _ string) error { return nil }

// The compile-time proof that the interface is implementable as written. A
// contract nothing satisfies is a contract nobody checked.
var _ database.Repository[invoice, string] = (*invoiceRepo)(nil)

func fixture() *invoiceRepo {
	return &invoiceRepo{rows: []invoice{
		{ID: "a", Tenant: "acme"},
		{ID: "b", Tenant: "acme"},
		{ID: "c", Tenant: "other"},
		{ID: "d", Tenant: "acme"},
	}}
}

// TestFindReportsErrNotFound: the sentinel is what the exception classifier
// turns into a 404, so a module gets the right status without writing one.
func TestFindReportsErrNotFound(t *testing.T) {
	repo := fixture()
	g := auth.SystemGrant("invoice.view", "acme")

	if _, err := repo.Find(context.Background(), g, "a"); err != nil {
		t.Fatalf("Find: %v", err)
	}

	_, err := repo.Find(context.Background(), g, "zzz")
	if !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	// Wrapped, so errors.Is works and the message still says which row.
	if got := err.Error(); got == database.ErrNotFound.Error() {
		t.Errorf("the error does not name the row: %q", got)
	}
}

// TestAMissRespectsTheTenant: a row belonging to somebody else must be
// indistinguishable from a row that is not there, on the read path as much as
// on the write path (RULE 17).
func TestAMissRespectsTheTenant(t *testing.T) {
	repo := fixture()

	_, err := repo.Find(context.Background(), auth.SystemGrant("invoice.view", "acme"), "c")
	if !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("another tenant's row was reachable: %v", err)
	}
}

// TestPageCarriesWhereToResume is what replaced a bare []T. The caller used to
// infer "is there more" from len(items) == q.Limit, which is wrong exactly once
// per result set -- on the page whose last row is the last row.
func TestPageCarriesWhereToResume(t *testing.T) {
	repo := fixture()
	g := auth.SystemGrant("invoice.view", "acme")

	var seen []string
	q := database.Query{Limit: 2}
	for range 10 {
		page, err := repo.List(context.Background(), g, q)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, row := range page.Items {
			seen = append(seen, row.ID)
		}
		if page.Next == "" {
			break
		}
		q.Cursor = page.Next
	}

	if len(seen) != 3 || seen[0] != "a" || seen[1] != "b" || seen[2] != "d" {
		t.Fatalf("walked %v, want every row of the tenant exactly once", seen)
	}
}

// TestTheLastPageSaysSo: an empty Next is the end of the walk, and it is the
// whole of the "is there more" question.
func TestTheLastPageSaysSo(t *testing.T) {
	repo := fixture()

	page, err := repo.List(context.Background(), auth.SystemGrant("invoice.view", "acme"), database.Query{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Next != "" {
		t.Errorf("Next = %q on a page holding everything", page.Next)
	}
	if len(page.Items) != 3 {
		t.Errorf("items = %+v, want the three rows of the tenant", page.Items)
	}
}

// TestAZeroGrantReachesNothing: the zero value is the only Grant a caller
// outside the auth package can build, and it must fail before any row is read.
func TestAZeroGrantReachesNothing(t *testing.T) {
	repo := fixture()

	if _, err := repo.Find(context.Background(), auth.Grant{}, "a"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("Find with the zero Grant returned %v, want ErrForbidden", err)
	}
	if _, err := repo.List(context.Background(), auth.Grant{}, database.Query{}); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("List with the zero Grant returned %v, want ErrForbidden", err)
	}
}
