package concerns

import (
	"errors"
	"reflect"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// TestRequireTenantRefusesAGrantCarryingNone.
//
// The zero Grant carries the empty tenant, and filtering on it compiles to a
// comparison against the empty string: nothing matches on a read, and a write
// lands a row no scoped read will ever return. Both look like a missing fixture
// and get debugged as one, which is why this refuses instead of filtering.
func TestRequireTenantRefusesAGrantCarryingNone(t *testing.T) {
	tenant, err := RequireTenant(auth.Grant{})
	if err == nil {
		t.Fatal("the zero Grant was accepted, so every statement after it filters on the empty string")
	}
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("RequireTenant answered %v, and a grant that authorizes nothing is a forbidden one", err)
	}
	if tenant != "" {
		t.Fatalf("RequireTenant answered the tenant %q beside an error", tenant)
	}
}

// TestRequireTenantAnswersTheGrantsTenant.
func TestRequireTenantAnswersTheGrantsTenant(t *testing.T) {
	tenant, err := RequireTenant(grant())
	if err != nil {
		t.Fatalf("RequireTenant: %v", err)
	}
	if tenant != "acme" {
		t.Fatalf("RequireTenant answered %q, and the grant carries acme", tenant)
	}
}

// TestTenantColumnForDefaultsToTheOneColumn: a model that says nothing is
// filtered, which is the direction the whole scheme rests on.
func TestTenantColumnForDefaultsToTheOneColumn(t *testing.T) {
	if got := TenantColumnFor(newFakeModel("roles")); got != TenantColumn {
		t.Fatalf("TenantColumnFor = %q, want %q for a model that declares nothing", got, TenantColumn)
	}
}

// TestTenantColumnForHonoursTheOptOut: a shared table -- a currency list, a
// country list -- says so by answering the empty string, and is read unfiltered.
func TestTenantColumnForHonoursTheOptOut(t *testing.T) {
	if got := TenantColumnFor(sharedModel{newFakeModel("currencies")}); got != "" {
		t.Fatalf("TenantColumnFor = %q for a model that opted out, want the empty string", got)
	}
}

// TestTenantColumnForHonoursANamedColumn.
func TestTenantColumnForHonoursANamedColumn(t *testing.T) {
	model := columnModel{fakeModel: newFakeModel("roles"), column: "account_id"}
	if got := TenantColumnFor(model); got != "account_id" {
		t.Fatalf("TenantColumnFor = %q, want the column the model named", got)
	}
}

// TestScopeTenantRefusesAGrantWithNoTenant, and returns no builder to carry on
// with: a caller that ignored the error and used the builder would run the read
// unfiltered.
func TestScopeTenantRefusesAGrantWithNoTenant(t *testing.T) {
	model := newFakeModel("roles")
	builder := newFakeBuilder(model, newQuery(&fakeConnection{}, "roles"))

	scoped, err := ScopeTenant(builder, model, auth.Grant{})
	if err == nil {
		t.Fatal("ScopeTenant accepted a grant with no tenant")
	}
	if scoped != nil {
		t.Fatal("ScopeTenant answered a builder beside the error, and it would be used unfiltered")
	}
	if len(builder.wheres) != 0 {
		t.Fatalf("ScopeTenant filtered %v before refusing", builder.columnsWheredOn())
	}
}

// TestScopeTenantFiltersTheModelsOwnTable, qualified, when the builder does not
// say it does that itself.
func TestScopeTenantFiltersTheModelsOwnTable(t *testing.T) {
	model := newFakeModel("roles")
	builder := newFakeBuilder(model, newQuery(&fakeConnection{}, "roles"))

	if _, err := ScopeTenant(builder, model, grant()); err != nil {
		t.Fatalf("ScopeTenant: %v", err)
	}

	if got := builder.columnsWheredOn(); !reflect.DeepEqual(got, []string{"roles.tenant_id"}) {
		t.Fatalf("ScopeTenant filtered %v, want the model's own qualified column", got)
	}
	if got := builder.wheres[0].args; len(got) != 1 || got[0] != "acme" {
		t.Fatalf("the filter compares against %v, want the grant's tenant", got)
	}
}

// TestScopeTenantSkipsTheOwnTableWhenTheBuilderSaysItScopesIt.
//
// The question is asked of the builder rather than answered by looking at the
// wheres, and that is the part that matters: a query already carrying
// `roles.tenant_id = 'other'` because somebody wrote that where by hand looks
// exactly like a query somebody scoped.
func TestScopeTenantSkipsTheOwnTableWhenTheBuilderSaysItScopesIt(t *testing.T) {
	model := newFakeModel("roles")
	inner := newFakeBuilder(model, newQuery(&fakeConnection{}, "roles"))

	if _, err := ScopeTenant(inner.asScoper(true), model, grant()); err != nil {
		t.Fatalf("ScopeTenant: %v", err)
	}

	if got := inner.columnsWheredOn(); len(got) != 0 {
		t.Fatalf("ScopeTenant filtered %v on a builder that scopes its own table, so the clause is written twice", got)
	}
}

// TestScopeTenantFiltersTheOwnTableWhenTheBuilderSaysItDoesNot: implementing
// the interface is not the answer, saying true is.
func TestScopeTenantFiltersTheOwnTableWhenTheBuilderSaysItDoesNot(t *testing.T) {
	model := newFakeModel("roles")
	inner := newFakeBuilder(model, newQuery(&fakeConnection{}, "roles"))

	if _, err := ScopeTenant(inner.asScoper(false), model, grant()); err != nil {
		t.Fatalf("ScopeTenant: %v", err)
	}

	if got := inner.columnsWheredOn(); !reflect.DeepEqual(got, []string{"roles.tenant_id"}) {
		t.Fatalf("ScopeTenant filtered %v, and a builder answering false is a builder that does not scope", got)
	}
}

// TestScopeTenantFiltersEveryJoinedTable is the leak this function was widened
// for.
//
// A join contributes no filter of its own, so a read whose own table is scoped
// and whose joined table is not returns the right parents carrying another
// customer's children -- and nothing in the result looks wrong.
func TestScopeTenantFiltersEveryJoinedTable(t *testing.T) {
	model := newFakeModel("roles")
	base := newQuery(&fakeConnection{}, "roles").
		Join("role_user", "roles.id", "=", "role_user.role_id").
		Join("accounts", "accounts.id", "=", "role_user.account_id")
	builder := newFakeBuilder(model, base)

	if _, err := ScopeTenant(builder, model, grant()); err != nil {
		t.Fatalf("ScopeTenant: %v", err)
	}

	want := []string{"role_user.tenant_id", "accounts.tenant_id", "roles.tenant_id"}
	if got := builder.columnsWheredOn(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ScopeTenant filtered %v, want %v", got, want)
	}
}

// TestScopeTenantFiltersJoinsEvenForAModelThatOptedOut: the opt-out is the
// model's own table saying its rows are shared. It says nothing about a table
// somebody joined to it.
func TestScopeTenantFiltersJoinsEvenForAModelThatOptedOut(t *testing.T) {
	model := sharedModel{newFakeModel("currencies")}
	base := newQuery(&fakeConnection{}, "currencies").
		Join("price_lists", "price_lists.currency", "=", "currencies.code")
	builder := newFakeBuilder(model, base)

	if _, err := ScopeTenant(builder, model, grant()); err != nil {
		t.Fatalf("ScopeTenant: %v", err)
	}

	if got := builder.columnsWheredOn(); !reflect.DeepEqual(got, []string{"price_lists.tenant_id"}) {
		t.Fatalf("ScopeTenant filtered %v, want the joined table and not the shared one", got)
	}
}

// TestScopeTenantDoesNotWriteTheJoinFilterTwice: scoping a query twice is not
// wrong, but every reader afterwards has to prove the second clause redundant
// before moving on, and the one who decides which copy to delete may delete the
// other.
func TestScopeTenantDoesNotWriteTheJoinFilterTwice(t *testing.T) {
	model := newFakeModel("roles")
	base := newQuery(&fakeConnection{}, "roles").
		Join("role_user", "roles.id", "=", "role_user.role_id").
		Where("role_user.tenant_id", "acme")
	builder := newFakeBuilder(model, base)

	if _, err := ScopeTenant(builder, model, grant()); err != nil {
		t.Fatalf("ScopeTenant: %v", err)
	}

	if got := builder.columnsWheredOn(); !reflect.DeepEqual(got, []string{"roles.tenant_id"}) {
		t.Fatalf("ScopeTenant filtered %v, and role_user was already filtered", got)
	}
}

// TestScopeTenantSkipsADerivedTable: `(select ...) as alias` has no tenant
// column of its own, and the query inside it is scoped where it was built.
// Filtering it would name a column the engine refuses.
func TestScopeTenantSkipsADerivedTable(t *testing.T) {
	model := newFakeModel("roles")
	base := newQuery(&fakeConnection{}, "roles").
		Join(query.Raw(`(select 1) as "totals"`), "totals.role_id", "=", "roles.id")
	builder := newFakeBuilder(model, base)

	if _, err := ScopeTenant(builder, model, grant()); err != nil {
		t.Fatalf("ScopeTenant: %v", err)
	}

	if got := builder.columnsWheredOn(); !reflect.DeepEqual(got, []string{"roles.tenant_id"}) {
		t.Fatalf("ScopeTenant filtered %v, and a derived table has no tenant column", got)
	}
}

// TestJoinedTableNameReadsTheAliasWhenThereIsOne.
//
// A self-join renames the table, and the columns of `users as alias` are
// qualified by the alias: the real name resolves to nothing, so a filter written
// on it is a statement the engine refuses.
func TestJoinedTableNameReadsTheAliasWhenThereIsOne(t *testing.T) {
	for _, c := range []struct {
		table any
		want  string
		ok    bool
	}{
		{"role_user", "role_user", true},
		{"users as arandu_reserved_0", "arandu_reserved_0", true},
		{"users AS Alias", "Alias", true},
		{"  role_user  ", "role_user", true},
		{query.Raw("(select 1) as totals"), "", false},
		{42, "", false},
	} {
		got, ok := joinedTableName(c.table)
		if ok != c.ok || got != c.want {
			t.Errorf("joinedTableName(%#v) = %q, %v; want %q, %v", c.table, got, ok, c.want, c.ok)
		}
	}
}

// TestHasFilterOnReadsOnlyABasicComparison.
//
// A nested or raw where says nothing about which column it constrains, so
// treating one as a tenant filter would skip the filter on the strength of a
// clause nobody read.
func TestHasFilterOnReadsOnlyABasicComparison(t *testing.T) {
	q := newQuery(&fakeConnection{}, "roles").Where("roles.tenant_id", "acme")

	if !hasFilterOn(q, "roles.tenant_id") {
		t.Fatal("a basic comparison on the column was not seen")
	}
	if hasFilterOn(q, "role_user.tenant_id") {
		t.Fatal("a filter was reported for a column nothing compares")
	}

	raw := newQuery(&fakeConnection{}, "roles").WhereRaw("roles.tenant_id = 'acme'")
	if hasFilterOn(raw, "roles.tenant_id") {
		t.Fatal("a raw clause was read as a filter, so the real one would be skipped")
	}
}

// TestScopeTenantQueryFiltersTheNamedTable: the pivot table is reached with the
// base builder rather than a typed one, and it needs the filter just as much --
// a pivot row is what says this customer's user has that customer's role.
func TestScopeTenantQueryFiltersTheNamedTable(t *testing.T) {
	q, err := ScopeTenantQuery(newQuery(&fakeConnection{}, "role_user"), "role_user", grant())
	if err != nil {
		t.Fatalf("ScopeTenantQuery: %v", err)
	}

	if len(q.Wheres) != 1 {
		t.Fatalf("ScopeTenantQuery wrote %d wheres: %#v", len(q.Wheres), q.Wheres)
	}
	if column := q.Wheres[0].Column; column != "role_user.tenant_id" {
		t.Fatalf("ScopeTenantQuery filtered on %v, want the qualified tenant column", column)
	}
	if bindings := q.GetBindings(); len(bindings) != 1 || bindings[0] != "acme" {
		t.Fatalf("the filter compares against %#v, want the grant's tenant", bindings)
	}
}

// TestScopeTenantQueryRefusesAGrantWithNoTenant, and answers no query: a caller
// that ignored the error would run the pivot statement across every tenant.
func TestScopeTenantQueryRefusesAGrantWithNoTenant(t *testing.T) {
	q, err := ScopeTenantQuery(newQuery(&fakeConnection{}, "role_user"), "role_user", auth.Grant{})
	if err == nil {
		t.Fatal("ScopeTenantQuery accepted a grant with no tenant")
	}
	if q != nil {
		t.Fatal("ScopeTenantQuery answered a query beside the error")
	}
}
