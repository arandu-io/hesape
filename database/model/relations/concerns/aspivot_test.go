package concerns

import (
	"reflect"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
)

// newPivotRow returns an AsPivot wired to a parent and a related model, and a
// pivot row model with the two foreign keys filled in.
//
// The pivot model's own key column is left absent, which is the shape the trait
// exists for: an intermediate table has no id of its own.
func newPivotRow(t *testing.T) (*AsPivot, *fakeModel, *fakeConnection) {
	t.Helper()

	conn := &fakeConnection{}
	parent := newFakeModel("users")
	related := newFakeModel("roles")

	row := newFakeModel("role_user")
	row.attributes["user_id"] = 1
	row.attributes["role_id"] = "admin"
	row.queryFactory = func() Builder {
		return newFakeBuilder(row, newQuery(conn, "role_user"))
	}

	pivot := (&AsPivot{PivotParent: parent, PivotRelated: related}).SetPivotKeys("user_id", "role_id")

	return pivot, row, conn
}

// TestSetPivotKeysIsWhatMakesTheRowAddressable.
func TestSetPivotKeysIsWhatMakesTheRowAddressable(t *testing.T) {
	pivot, _, _ := newPivotRow(t)

	if got := pivot.GetForeignKey(); got != "user_id" {
		t.Fatalf("GetForeignKey = %q", got)
	}
	if got := pivot.GetRelatedKey(); got != "role_id" {
		t.Fatalf("GetRelatedKey = %q", got)
	}
	if pivot.GetOtherKey() != pivot.GetRelatedKey() {
		t.Fatalf("GetOtherKey = %q and GetRelatedKey = %q, and they are one method under two names",
			pivot.GetOtherKey(), pivot.GetRelatedKey())
	}
}

// TestSetRelatedModelReplacesIt, which is how a pivot read off a query learns
// what it points at.
func TestSetRelatedModelReplacesIt(t *testing.T) {
	pivot, _, _ := newPivotRow(t)

	other := newFakeModel("permissions")
	if got := pivot.SetRelatedModel(other); got != pivot {
		t.Fatal("SetRelatedModel did not answer the receiver, so it cannot be chained")
	}
	if pivot.PivotRelated != Model(other) {
		t.Fatal("SetRelatedModel did not replace the related model")
	}
}

// TestTheTimestampColumnsComeFromTheParent.
//
// A pivot has no configuration of its own, so the names come from the model the
// relation was declared on. Falling back to the conventional pair is what keeps a
// pivot built without a parent from naming an empty column.
func TestTheTimestampColumnsComeFromTheParent(t *testing.T) {
	pivot, _, _ := newPivotRow(t)

	if got := pivot.GetCreatedAtColumn(); got != "created_at" {
		t.Fatalf("GetCreatedAtColumn = %q", got)
	}
	if got := pivot.GetUpdatedAtColumn(); got != "updated_at" {
		t.Fatalf("GetUpdatedAtColumn = %q", got)
	}

	orphan := &AsPivot{}
	if got := orphan.GetCreatedAtColumn(); got != "created_at" {
		t.Fatalf("a pivot with no parent answered %q for created_at", got)
	}
	if got := orphan.GetUpdatedAtColumn(); got != "updated_at" {
		t.Fatalf("a pivot with no parent answered %q for updated_at", got)
	}
}

// TestHasTimestampAttributesLooksAtTheRowAndNotAtAFlag.
//
// A pivot table carries timestamps only when the developer wrote withTimestamps,
// so the question is answered by whether the column is in the row that came back.
func TestHasTimestampAttributesLooksAtTheRowAndNotAtAFlag(t *testing.T) {
	pivot, _, _ := newPivotRow(t)

	if pivot.HasTimestampAttributes(map[string]any{"role_id": "admin"}) {
		t.Fatal("a row with no created_at was reported as carrying timestamps")
	}
	if !pivot.HasTimestampAttributes(map[string]any{"created_at": "now"}) {
		t.Fatal("a row carrying created_at was reported as not")
	}

	// A nil value still counts: the column is there, which is the question.
	if !pivot.HasTimestampAttributes(map[string]any{"created_at": nil}) {
		t.Fatal("a null created_at was read as the column being absent")
	}
}

// TestSetKeysForSelectQueryKeysByThePairWhenThereIsNoID.
//
// This is what makes a pivot row addressable at all. Without it a save would key
// by an id column the intermediate table does not have.
func TestSetKeysForSelectQueryKeysByThePairWhenThereIsNoID(t *testing.T) {
	pivot, row, conn := newPivotRow(t)
	builder := newFakeBuilder(row, newQuery(conn, "role_user"))

	pivot.SetKeysForSelectQuery(builder, row)

	want := []string{"user_id", "role_id"}
	if got := builder.columnsWheredOn(); !reflect.DeepEqual(got, want) {
		t.Fatalf("the query is keyed on %v, want %v", got, want)
	}
	if got := builder.wheres[0].args; len(got) != 1 || got[0] != 1 {
		t.Fatalf("the foreign key is compared against %v, want the row's own value", got)
	}
	if got := builder.wheres[1].args; len(got) != 1 || got[0] != "admin" {
		t.Fatalf("the related key is compared against %v, want the row's own value", got)
	}
}

// TestSetKeysForSelectQueryPrefersAnIDColumnWhenTheTableHasOne.
//
// A pivot table with a primary key of its own is addressable the ordinary way,
// and keying by the pair there would find the row by two columns that may not be
// unique together.
func TestSetKeysForSelectQueryPrefersAnIDColumnWhenTheTableHasOne(t *testing.T) {
	pivot, row, conn := newPivotRow(t)
	row.attributes["id"] = 42
	builder := newFakeBuilder(row, newQuery(conn, "role_user"))

	pivot.SetKeysForSelectQuery(builder, row)

	if got := builder.columnsWheredOn(); !reflect.DeepEqual(got, []string{"id"}) {
		t.Fatalf("the query is keyed on %v, and the row has an id", got)
	}
}

// TestSetKeysForSaveQueryIsTheSelectQuery: the PHP has two methods and one body,
// and a save keyed differently from the select that found the row would write to
// a different row.
func TestSetKeysForSaveQueryIsTheSelectQuery(t *testing.T) {
	pivot, row, conn := newPivotRow(t)

	forSelect := newFakeBuilder(row, newQuery(conn, "role_user"))
	forSave := newFakeBuilder(row, newQuery(conn, "role_user"))

	pivot.SetKeysForSelectQuery(forSelect, row)
	pivot.SetKeysForSaveQuery(forSave, row)

	if !reflect.DeepEqual(forSelect.columnsWheredOn(), forSave.columnsWheredOn()) {
		t.Fatalf("select keys on %v and save keys on %v",
			forSelect.columnsWheredOn(), forSave.columnsWheredOn())
	}
}

// TestGetQueueableIDWritesThePairWhenThereIsNoID, in the shape
// NewQueryForRestoration reads back.
func TestGetQueueableIDWritesThePairWhenThereIsNoID(t *testing.T) {
	pivot, row, _ := newPivotRow(t)

	if got := pivot.GetQueueableID(row); got != "user_id:1:role_id:admin" {
		t.Fatalf("GetQueueableID = %#v, want the pair a restoration can split", got)
	}

	row.attributes["id"] = 42
	if got := pivot.GetQueueableID(row); got != 42 {
		t.Fatalf("GetQueueableID = %#v for a row with an id, want the id", got)
	}
}

// TestNewQueryForRestorationFiltersByTenant is the leak.
//
// A job that serialized "user_id:1:role_id:admin" used to restore it with a
// query carrying that pair and nothing else, over a table every customer shares:
// the row that came back was whichever the database returned first. Save and
// Delete on the same pivot were scoped; only the way back from a queue was not.
func TestNewQueryForRestorationFiltersByTenant(t *testing.T) {
	pivot, row, _ := newPivotRow(t)

	q, err := pivot.NewQueryForRestoration(row, grant(), "user_id:1:role_id:admin")
	if err != nil {
		t.Fatalf("NewQueryForRestoration: %v", err)
	}

	columns := q.(*fakeBuilder).columnsWheredOn()
	if len(columns) == 0 || columns[0] != "role_user.tenant_id" {
		t.Fatalf("the restoration query filters on %v, and the tenant has to come first", columns)
	}
	if !reflect.DeepEqual(columns, []string{"role_user.tenant_id", "user_id", "role_id"}) {
		t.Fatalf("the restoration query filters on %v", columns)
	}
}

// TestNewQueryForRestorationRefusesAGrantWithNoTenant, and answers no query: a
// caller that ignored the error would restore across every customer.
func TestNewQueryForRestorationRefusesAGrantWithNoTenant(t *testing.T) {
	pivot, row, _ := newPivotRow(t)

	q, err := pivot.NewQueryForRestoration(row, auth.Grant{}, "user_id:1:role_id:admin")
	if err == nil {
		t.Fatal("a restoration was built under a grant carrying no tenant")
	}
	if q != nil {
		t.Fatal("NewQueryForRestoration answered a query beside the error")
	}
}

// TestNewQueryForRestorationOfAPlainIDGoesThroughTheKey, which is the ordinary
// Model::newQueryForRestoration and takes the whole list at once.
func TestNewQueryForRestorationOfAPlainIDGoesThroughTheKey(t *testing.T) {
	pivot, row, _ := newPivotRow(t)

	q, err := pivot.NewQueryForRestoration(row, grant(), 42, 43)
	if err != nil {
		t.Fatalf("NewQueryForRestoration: %v", err)
	}

	sql := q.GetQuery().ToSQL()
	if !strings.Contains(sql, `"role_user"."id" in (?, ?)`) {
		t.Fatalf("the restoration query is %q, want both keys in one clause", sql)
	}
	if !strings.Contains(sql, "tenant_id") {
		t.Fatalf("the restoration query is %q, and it carries no tenant", sql)
	}
}

// TestNewQueryForRestorationGroupsEachCompositeID.
//
// Written flat, the clauses of one row would filter the rows of the next and the
// query would match nothing: user_id = 1 and role_id = 'admin' and user_id = 2.
func TestNewQueryForRestorationGroupsEachCompositeID(t *testing.T) {
	pivot, row, _ := newPivotRow(t)

	q, err := pivot.NewQueryForRestoration(row, grant(),
		"user_id:1:role_id:admin", "user_id:2:role_id:editor")
	if err != nil {
		t.Fatalf("NewQueryForRestoration: %v", err)
	}

	sql := q.GetQuery().ToSQL()
	if strings.Count(sql, "(") < 2 {
		t.Fatalf("the restoration query is %q, want a parenthesised group per identifier", sql)
	}
	if !strings.Contains(sql, `or ("user_id" = ? and "role_id" = ?)`) {
		t.Fatalf("the restoration query is %q, want the identifiers ored as groups", sql)
	}

	bindings := q.GetQuery().GetBindings()
	for _, want := range []any{"acme", "1", "admin", "2", "editor"} {
		if !containsValue(bindings, want) {
			t.Fatalf("the restoration query carries %#v, and %#v is missing", bindings, want)
		}
	}
}

// TestNewQueryForRestorationRefusesAnIdentifierItDidNotWrite.
//
// The PHP indexes into the exploded array blindly. A malformed identifier here
// would build a query keyed on nothing, which restores the wrong row rather than
// failing.
func TestNewQueryForRestorationRefusesAnIdentifierItDidNotWrite(t *testing.T) {
	pivot, row, _ := newPivotRow(t)

	for _, c := range []struct {
		name string
		ids  []any
		want string
	}{
		{"nothing at all", nil, "stored nothing"},
		{"too few segments", []any{"user_id:1:role_id"}, "3 segments"},
		{"too many segments", []any{"user_id:1:role_id:admin:extra:x"}, "6 segments"},
		{"a mixed list", []any{"user_id:1:role_id:admin", 42}, "all keys or all pairs"},
	} {
		t.Run(c.name, func(t *testing.T) {
			q, err := pivot.NewQueryForRestoration(row, grant(), c.ids...)
			if err == nil {
				t.Fatalf("%s was accepted, and the query it built is keyed on nothing", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("NewQueryForRestoration: %v, want a message containing %q", err, c.want)
			}
			if q != nil {
				t.Fatal("NewQueryForRestoration answered a query beside the error")
			}
		})
	}
}

// TestNewQueryForRestorationSaysSoWhenThePivotHasNoQuery rather than
// dereferencing the nil four frames further in.
func TestNewQueryForRestorationSaysSoWhenThePivotHasNoQuery(t *testing.T) {
	pivot, row, _ := newPivotRow(t)
	row.queryFactory = nil
	row.query = nil

	if _, err := pivot.NewQueryForRestoration(row, grant(), "user_id:1:role_id:admin"); err == nil {
		t.Fatal("a pivot with no query built a restoration query anyway")
	} else if !strings.Contains(err.Error(), "without a query") {
		t.Fatalf("NewQueryForRestoration: %v", err)
	}
}

// TestIsCompositeIDReadsTheSeparatorAndNothingElse.
func TestIsCompositeIDReadsTheSeparatorAndNothingElse(t *testing.T) {
	for _, c := range []struct {
		in   any
		want bool
	}{
		{"user_id:1:role_id:admin", true},
		{"a:b", true},
		{"admin", false},
		{42, false},
		{nil, false},
	} {
		if got := isCompositeID(c.in); got != c.want {
			t.Errorf("isCompositeID(%#v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestUnsetRelationsDropsBothModels.
//
// A pivot holds the parent and the related model as ordinary references, so a
// serialized pivot would drag both whole models into the payload with it.
func TestUnsetRelationsDropsBothModels(t *testing.T) {
	pivot, _, _ := newPivotRow(t)

	pivot.UnsetRelations()

	if pivot.PivotParent != nil || pivot.PivotRelated != nil {
		t.Fatalf("UnsetRelations left parent=%v related=%v", pivot.PivotParent, pivot.PivotRelated)
	}

	// And the timestamp columns fall back rather than panicking on the nil.
	if got := pivot.GetCreatedAtColumn(); got != "created_at" {
		t.Fatalf("after UnsetRelations, GetCreatedAtColumn = %q", got)
	}
}
