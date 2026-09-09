package grammars_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/database/schema"
)

// limits is the identifier limit each driver reports, and the number this file
// asserts every generated name against.
var limits = map[string]int{"pgsql": 63, "mysql": 64, "sqlite": 0}

// quoted matches an identifier inside either driver's quotes, which is how a
// generated index name is read back out of a compiled statement.
var quoted = regexp.MustCompile("[\"`]([^\"`]+)[\"`]")

// namesIn returns every distinct quoted identifier in the compiled statements
// that is a name generated for table: the convention starts every one of them
// with the table's own name, and shortening keeps that prefix. The table
// itself, its columns and the tables it references are all excluded by it.
func namesIn(statements []string, table string) []string {
	var found []string
	seen := map[string]bool{}

	for _, statement := range statements {
		for _, match := range quoted.FindAllStringSubmatch(statement, -1) {
			name := match[1]
			if !strings.HasPrefix(name, table+"_") || seen[name] {
				continue
			}
			seen[name] = true
			found = append(found, name)
		}
	}

	return found
}

// sql compiles a blueprint and returns its statements together with the error,
// which the tests here read rather than fail on.
func sql(conn *fakeConnection, table string, build func(*schema.Blueprint)) ([]string, error) {
	return schema.NewBlueprint(conn, table, build).ToSQL(context.Background())
}

// billing is the column list from the report that motivated this: a composite
// index on a multitenant table, whose conventional name is 92 bytes.
var billing = []any{"tenant_id", "environment", "merchant_id", "customer_id", "effective_from", "id"}

// TestConventionalNameFitsTheDriverLimit is the whole point: a name the
// convention builds longer than the driver accepts comes out short enough,
// rather than being cut by the server after the fact.
func TestConventionalNameFitsTheDriverLimit(t *testing.T) {
	for driver, limit := range limits {
		t.Run(driver, func(t *testing.T) {
			statements, err := sql(newFake(driver), "usage_rate_assignments", func(table *schema.Blueprint) {
				table.Index(billing, "")
			})
			if err != nil {
				t.Fatalf("ToSQL: %v", err)
			}

			names := namesIn(statements, "usage_rate_assignments")
			if len(names) != 1 {
				t.Fatalf("got %d index names, want 1: %v", len(names), names)
			}

			if limit > 0 && len(names[0]) > limit {
				t.Errorf("name %q is %d bytes, over the %d-byte limit", names[0], len(names[0]), limit)
			}
			if limit == 0 && !strings.HasPrefix(names[0], "usage_rate_assignments_tenant_id_environment_merchant_id") {
				t.Errorf("a driver with no limit should keep the conventional name, got %q", names[0])
			}
		})
	}
}

// TestTheSameColumnsAlwaysProduceTheSameName fixes that the shortened name is
// derived from the columns and nothing else, so a migration re-run, or run on
// another machine, names the same index.
func TestTheSameColumnsAlwaysProduceTheSameName(t *testing.T) {
	for driver := range limits {
		t.Run(driver, func(t *testing.T) {
			build := func(table *schema.Blueprint) { table.Index(billing, "") }

			first, err := sql(newFake(driver), "usage_rate_assignments", build)
			if err != nil {
				t.Fatalf("ToSQL: %v", err)
			}
			second, err := sql(newFake(driver), "usage_rate_assignments", build)
			if err != nil {
				t.Fatalf("ToSQL: %v", err)
			}

			if got, want := namesIn(second, "usage_rate_assignments"), namesIn(first, "usage_rate_assignments"); got[0] != want[0] {
				t.Errorf("two runs named the same index differently: %q and %q", want[0], got[0])
			}
		})
	}
}

// TestNamesThatDifferOnlyPastTheLimitStayDistinct is the failure the report
// describes: two indexes on one table whose conventional names are identical
// through the first 63 bytes. Truncation makes them one name and the second
// CREATE INDEX fails with "already exists" -- on whichever database happens to
// hold both, which is rarely the one the migration was written against.
func TestNamesThatDifferOnlyPastTheLimitStayDistinct(t *testing.T) {
	effectiveFrom := []any{"tenant_id", "environment", "merchant_id", "customer_id", "effective_from", "id"}
	createdAt := []any{"tenant_id", "environment", "merchant_id", "customer_id", "created_at", "id"}

	for driver, limit := range limits {
		t.Run(driver, func(t *testing.T) {
			statements, err := sql(newFake(driver), "usage_rate_assignments", func(table *schema.Blueprint) {
				table.Index(effectiveFrom, "")
				table.Index(createdAt, "")
			})
			if err != nil {
				t.Fatalf("ToSQL: %v", err)
			}

			names := namesIn(statements, "usage_rate_assignments")
			if len(names) != 2 {
				t.Fatalf("got %d index names, want 2: %v", len(names), names)
			}
			if names[0] == names[1] {
				t.Fatalf("both indexes were named %q", names[0])
			}
			for _, name := range names {
				if limit > 0 && len(name) > limit {
					t.Errorf("name %q is %d bytes, over the %d-byte limit", name, len(name), limit)
				}
			}
		})
	}
}

// TestIndexUniqueAndForeignAllFollowTheRule keeps the three families that put a
// generated name into a statement on the same convention. Primary is checked by
// TestAnExplicitNameOverTheLimitIsRefused instead: no driver here emits a name
// for it.
func TestIndexUniqueAndForeignAllFollowTheRule(t *testing.T) {
	for driver, limit := range limits {
		if limit == 0 {
			continue
		}
		t.Run(driver, func(t *testing.T) {
			statements, err := sql(newFake(driver), "usage_rate_assignments", func(table *schema.Blueprint) {
				table.Index(billing, "")
				table.Unique(billing, "")
				table.Foreign(billing).References("id").On("usage_rates")
			})
			if err != nil {
				t.Fatalf("ToSQL: %v", err)
			}

			names := namesIn(statements, "usage_rate_assignments")
			if len(names) != 3 {
				t.Fatalf("got %d distinct names for index, unique and foreign, want 3: %v", len(names), names)
			}
			for _, name := range names {
				if len(name) > limit {
					t.Errorf("name %q is %d bytes, over the %d-byte limit", name, len(name), limit)
				}
			}
		})
	}
}

// TestAnExplicitNameOverTheLimitIsRefused covers the other half of the ask: a
// name the caller wrote is used verbatim, and a verbatim name the driver would
// cut is refused here rather than silently changed by the server.
func TestAnExplicitNameOverTheLimitIsRefused(t *testing.T) {
	long := strings.Repeat("a", 70)

	cases := map[string]func(*schema.Blueprint){
		"index":       func(table *schema.Blueprint) { table.Index(billing, long) },
		"unique":      func(table *schema.Blueprint) { table.Unique(billing, long) },
		"primary":     func(table *schema.Blueprint) { table.Primary(billing, long) },
		"foreign":     func(table *schema.Blueprint) { table.Foreign(billing, long).References("id").On("usage_rates") },
		"dropIndex":   func(table *schema.Blueprint) { table.DropIndex(long) },
		"renameIndex": func(table *schema.Blueprint) { table.RenameIndex("usage_rate_assignments_id_index", long) },
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			for driver, limit := range limits {
				_, err := sql(newFake(driver), "usage_rate_assignments", build)

				var tooLong *schema.IdentifierTooLongError
				switch {
				case limit == 0:
					if errors.As(err, &tooLong) {
						t.Errorf("%s: a driver with no limit refused %d bytes", driver, len(long))
					}
				case !errors.As(err, &tooLong):
					t.Errorf("%s: got %v, want IdentifierTooLongError", driver, err)
				case tooLong.Limit != limit || tooLong.Name != long:
					t.Errorf("%s: got %+v, want name %q and limit %d", driver, tooLong, long, limit)
				}
			}
		})
	}
}

// TestAnExplicitNameWithinTheLimitIsUsedVerbatim fixes that the check refuses
// only what the driver would refuse.
func TestAnExplicitNameWithinTheLimitIsUsedVerbatim(t *testing.T) {
	for driver := range limits {
		t.Run(driver, func(t *testing.T) {
			statements, err := sql(newFake(driver), "usage_rate_assignments", func(table *schema.Blueprint) {
				table.Index(billing, "usage_rate_assignments_lookup")
			})
			if err != nil {
				t.Fatalf("ToSQL: %v", err)
			}
			if names := namesIn(statements, "usage_rate_assignments"); len(names) != 1 || names[0] != "usage_rate_assignments_lookup" {
				t.Errorf("got %v, want the name written by the caller", names)
			}
		})
	}
}

// TestNamesWithinTheLimitDoNotMove guards the migration already applied. The
// correction changes only names the convention built past the driver's limit;
// a name that fit before has to be byte for byte what it was, or a DROP INDEX
// written against the old name would stop finding the index.
func TestNamesWithinTheLimitDoNotMove(t *testing.T) {
	for driver := range limits {
		t.Run(driver, func(t *testing.T) {
			statements, err := sql(newFake(driver), "users", func(table *schema.Blueprint) {
				table.Index([]any{"team_id", "email"}, "")
			})
			if err != nil {
				t.Fatalf("ToSQL: %v", err)
			}
			if names := namesIn(statements, "users"); len(names) != 1 || names[0] != "users_team_id_email_index" {
				t.Errorf("got %v, want users_team_id_email_index", names)
			}
		})
	}
}
