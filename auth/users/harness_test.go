package users_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
	"github.com/arandu-io/hesape/database/query/grammars"
	"github.com/arandu-io/hesape/database/query/processors"
	"github.com/arandu-io/hesape/hashing"
)

// The bank of mentira the two providers run against: a connection that records
// every statement and answers selects with rows the test queued.
//
// It compiles real SQL through the SQLite grammar rather than stubbing the
// builder, because half of what a provider is judged on is the statement it
// issued -- that the tenant is on it, and that the password never is.

// statement is one call the connection took.
type statement struct {
	kind     string
	sql      string
	bindings []any
}

// fakeConnection is query.Connection and users.Connection at once: the first is
// what a builder runs against, the second is what DatabaseUserProvider opens a
// table with.
type fakeConnection struct {
	// rows is queued one result set per select, in order. A select past the end
	// of the queue finds nothing, which is the "no such user" case.
	rows [][]query.Record

	statements []statement

	// affected is what an update reports, and err is what every statement fails
	// with when it is set.
	affected int64
	err      error
}

func (c *fakeConnection) Select(sql string, bindings []any, _ bool) ([]query.Record, error) {
	c.statements = append(c.statements, statement{kind: "select", sql: sql, bindings: bindings})
	if c.err != nil {
		return nil, c.err
	}
	if len(c.rows) == 0 {
		return nil, nil
	}
	rows := c.rows[0]
	c.rows = c.rows[1:]
	return rows, nil
}

func (c *fakeConnection) Insert(sql string, bindings []any) (bool, error) {
	c.statements = append(c.statements, statement{kind: "insert", sql: sql, bindings: bindings})
	return c.err == nil, c.err
}

func (c *fakeConnection) Update(sql string, bindings []any) (int64, error) {
	c.statements = append(c.statements, statement{kind: "update", sql: sql, bindings: bindings})
	return c.affected, c.err
}

func (c *fakeConnection) Delete(sql string, bindings []any) (int64, error) {
	c.statements = append(c.statements, statement{kind: "delete", sql: sql, bindings: bindings})
	return c.affected, c.err
}

func (c *fakeConnection) Statement(sql string, bindings []any) (bool, error) {
	c.statements = append(c.statements, statement{kind: "statement", sql: sql, bindings: bindings})
	return c.err == nil, c.err
}

// Table answers users.Connection: the one method DatabaseUserProvider asks a
// connection for.
func (c *fakeConnection) Table(ctx context.Context, table any, as ...string) *query.Builder {
	return query.NewBuilder(c, grammars.NewSQLiteGrammar(), processors.NewSQLiteProcessor()).From(table, as...)
}

// users is the newQuery factory ModelUserProvider is built with: a fresh
// builder on the users table.
func (c *fakeConnection) users(ctx context.Context) *query.Builder {
	return c.Table(ctx, "users")
}

// queue puts one result set at the end of the queue, so a test reads as the
// order the provider selects in.
func (c *fakeConnection) queue(rows ...query.Record) *fakeConnection {
	c.rows = append(c.rows, rows)
	return c
}

// last is the statement the connection took most recently.
func (c *fakeConnection) last(t *testing.T) statement {
	t.Helper()
	if len(c.statements) == 0 {
		t.Fatal("the provider issued no statement at all")
	}
	return c.statements[len(c.statements)-1]
}

// only fails unless exactly one statement was issued, and answers it.
func (c *fakeConnection) only(t *testing.T) statement {
	t.Helper()
	if len(c.statements) != 1 {
		t.Fatalf("the provider issued %d statements, want 1: %v", len(c.statements), c.statements)
	}
	return c.statements[0]
}

// assertScopedBy fails unless the statement filters by the tenant column and
// binds the given tenant: a read is scoped exactly as a write is.
func (s statement) assertScopedBy(t *testing.T, tenant string) {
	t.Helper()
	if !strings.Contains(s.sql, `"tenant_id"`) {
		t.Fatalf("the statement carries no tenant filter: %s", s.sql)
	}
	for _, binding := range s.bindings {
		if binding == any(tenant) {
			return
		}
	}
	t.Fatalf("the statement does not bind the tenant %q: %s %v", tenant, s.sql, s.bindings)
}

// assertNeverBinds fails when a value reached the statement. It is how "the
// password is never in the SQL" is checked.
func (s statement) assertNeverBinds(t *testing.T, value string) {
	t.Helper()
	if strings.Contains(s.sql, value) {
		t.Fatalf("the statement carries %q in its text: %s", value, s.sql)
	}
	for _, binding := range s.bindings {
		if binding == any(value) {
			t.Fatalf("the statement binds %q: %s %v", value, s.sql, s.bindings)
		}
	}
}

// testUser is the application's own user type: the seven Authenticatable
// methods, plus the two a provider asks for by assertion -- SetRawAttributes to
// be filled from a row, and ForceFill to take a rehashed password.
//
// A user type that embeds *eloquent.Model[T] gets all nine by embedding it; this
// writes them out so the test depends on nothing but the contracts.
type testUser struct {
	attributes map[string]any
}

var (
	_ auth.Authenticatable = (*testUser)(nil)
)

// newTestUser is the model constructor ModelUserProvider is built with.
func newTestUser() auth.Authenticatable { return &testUser{attributes: map[string]any{}} }

func (u *testUser) GetAuthIdentifierName() string { return "id" }
func (u *testUser) GetAuthIdentifier() any        { return u.attributes["id"] }
func (u *testUser) GetAuthPasswordName() string   { return "password" }
func (u *testUser) GetRememberTokenName() string  { return "remember_token" }

func (u *testUser) GetAuthPassword() string {
	value, _ := u.attributes["password"].(string)
	return value
}

func (u *testUser) GetRememberToken() string {
	value, _ := u.attributes["remember_token"].(string)
	return value
}

func (u *testUser) SetRememberToken(token string) {
	if u.attributes == nil {
		u.attributes = map[string]any{}
	}
	u.attributes["remember_token"] = token
}

// SetRawAttributes answers users.Hydratable: the row becomes the user.
func (u *testUser) SetRawAttributes(attributes map[string]any, _ bool) error {
	u.attributes = attributes
	return nil
}

// ForceFill answers users.Fillable: merge into the row rather than replace it.
func (u *testUser) ForceFill(attributes map[string]any) error {
	if u.attributes == nil {
		u.attributes = map[string]any{}
	}
	for key, value := range attributes {
		u.attributes[key] = value
	}
	return nil
}

// plainUser is a user type with the seven contract methods and neither of the
// two extras, which is what ErrNotHydratable is for.
type plainUser struct{ testUser }

func newPlainUser() auth.Authenticatable { return &plainUser{} }

// SetRawAttributes is declared with the wrong shape on purpose, so plainUser is
// an Authenticatable that is not a users.Hydratable. Embedding testUser would
// otherwise promote the real one.
func (u *plainUser) SetRawAttributes(map[string]any) {}

// countingHasher is a real hasher with a counter in front of it, so a test can
// say that the hasher was reached -- which is the whole claim about an empty
// password.
type countingHasher struct {
	inner  auth.Hasher
	checks int
	makes  int
}

func newCountingHasher() *countingHasher {
	return &countingHasher{inner: cheapHasher()}
}

func (h *countingHasher) Make(value string) (string, error) {
	h.makes++
	return h.inner.Make(value)
}

func (h *countingHasher) Check(value, hashedValue string) bool {
	h.checks++
	return h.inner.Check(value, hashedValue)
}

func (h *countingHasher) NeedsRehash(hashedValue string) bool {
	return h.inner.NeedsRehash(hashedValue)
}

// cheapHasher is the production hasher these tests authenticate with: bcrypt
// behind the adapter, at the lowest cost the algorithm accepts so that a test
// file hashing a dozen passwords stays under a second.
//
// It is deliberately not a fake. The bug that started this file was that no real
// hasher satisfied auth.Hasher at all, so a test that authenticates through a
// double proves nothing about the thing that was broken.
func cheapHasher() auth.Hasher {
	return hashing.ForAuth(hashing.NewBcryptHasher(hashing.Options{Rounds: 4}))
}

// hasherAt is cheapHasher at a named cost, for the rehash tests: a hash written
// at one cost and a hasher configured for another is what needsRehash is about.
func hasherAt(rounds int) auth.Hasher {
	return hashing.ForAuth(hashing.NewBcryptHasher(hashing.Options{Rounds: rounds}))
}

// mustHash hashes with the cheap hasher or fails the test.
func mustHash(t *testing.T, hasher auth.Hasher, plain string) string {
	t.Helper()
	hashed, err := hasher.Make(plain)
	if err != nil {
		t.Fatalf("hashing %q: %v", plain, err)
	}
	return hashed
}
