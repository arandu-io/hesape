package migrations

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arandu-io/hesape/str"
)

// MigrationCreator answers
// Illuminate\Database\Migrations\MigrationCreator: what `aru make:migration`
// writes the file with.
//
// The stub it fills is Go rather than PHP, and everything else is the PHP's:
// the date prefix, the three stubs chosen by whether a table was named and
// whether it is being created, the custom stub directory that wins over the
// built-in one, and the post-create hooks.
type MigrationCreator struct {
	// customStubPath is MigrationCreator::$customStubPath: a project directory
	// whose stubs win over the built-in ones.
	customStubPath string

	// postCreate is MigrationCreator::$postCreate.
	postCreate []func(table, path string)

	// now is where the date prefix comes from. PHP calls date() directly; a
	// variable is what makes the prefix testable without freezing the process
	// clock.
	now func() time.Time
}

// NewMigrationCreator answers MigrationCreator::__construct.
//
// The PHP's first argument is a Filesystem, which this does without: the file
// operations involved are two calls to the standard library, and an injected
// filesystem exists in Laravel so the container can swap it.
func NewMigrationCreator(customStubPath string) *MigrationCreator {
	return &MigrationCreator{customStubPath: customStubPath, now: time.Now}
}

// Create answers MigrationCreator::create: write the migration and answer the
// path it was written to.
//
// table empty is the PHP's null: a migration with no table in mind. create says
// whether the named table is being created or altered, which is the difference
// between the two table-shaped stubs.
func (c *MigrationCreator) Create(name, path, table string, create bool) (string, error) {
	if err := c.ensureMigrationDoesntAlreadyExist(name, path); err != nil {
		return "", err
	}

	stub, err := c.getStub(table, create)
	if err != nil {
		return "", err
	}

	fullPath := c.GetPath(name, path)

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", fmt.Errorf("creating the migration directory: %w", err)
	}

	if err := os.WriteFile(fullPath, []byte(c.populateStub(stub, name, table)), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", fullPath, err)
	}

	c.firePostCreateHooks(table, fullPath)

	return fullPath, nil
}

// ensureMigrationDoesntAlreadyExist answers the protected method of the same
// name.
//
// The PHP requires every file in the directory and then asks class_exists,
// because two migrations declaring the same class is a fatal error at load. Go
// finds that at compile time, so the check that is left is the one the compiler
// cannot make: a second file whose name differs only by its date prefix, which
// compiles and then applies twice under two names.
func (c *MigrationCreator) ensureMigrationDoesntAlreadyExist(name, migrationPath string) error {
	if migrationPath == "" {
		return nil
	}

	entries, err := os.ReadDir(migrationPath)
	if err != nil {
		// A directory that is not there yet is not a conflict; Create makes it.
		return nil
	}

	suffix := "_" + name + ".go"
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), suffix) {
			return fmt.Errorf("a %s migration already exists, in %s", name, filepath.Join(migrationPath, entry.Name()))
		}
	}
	return nil
}

// getStub answers the protected MigrationCreator::getStub: the custom stub when
// the project has one, the built-in otherwise.
func (c *MigrationCreator) getStub(table string, create bool) (string, error) {
	var name, builtin string
	switch {
	case table == "":
		name, builtin = "migration.stub", blankStub
	case create:
		name, builtin = "migration.create.stub", createStub
	default:
		name, builtin = "migration.update.stub", updateStub
	}

	if c.customStubPath != "" {
		custom := filepath.Join(c.customStubPath, name)
		if contents, err := os.ReadFile(custom); err == nil {
			return string(contents), nil
		}
	}
	return builtin, nil
}

// populateStub answers the protected MigrationCreator::populateStub.
//
// The PHP replaces DummyTable, {{ table }} and {{table}}. This replaces the
// same three plus the two a Go stub needs and a PHP one does not: the type name
// and the migration's own name, which is its identity here because there is no
// file path to read it back off.
func (c *MigrationCreator) populateStub(stub, name, table string) string {
	replacements := []string{
		"{{ class }}", c.GetClassName(name),
		"{{class}}", c.GetClassName(name),
		"{{ name }}", c.migrationName(name),
		"{{name}}", c.migrationName(name),
	}
	if table != "" {
		replacements = append(replacements,
			"DummyTable", table,
			"{{ table }}", table,
			"{{table}}", table,
		)
	}
	return strings.NewReplacer(replacements...).Replace(stub)
}

// migrationName is the identity the generated migration answers from GetName:
// the date prefix and the name, which is exactly the file's base name.
func (c *MigrationCreator) migrationName(name string) string {
	return c.GetDatePrefix() + "_" + name
}

// GetClassName answers the protected MigrationCreator::getClassName.
//
// It is exported here because a caller with no filesystem access -- a test, a
// generator that prints rather than writes -- has no other way to ask, and Go
// has no protected.
func (c *MigrationCreator) GetClassName(name string) string { return str.Studly(name) }

// GetPath answers the protected MigrationCreator::getPath.
func (c *MigrationCreator) GetPath(name, path string) string {
	return filepath.Join(path, c.migrationName(name)+".go")
}

// firePostCreateHooks answers the protected
// MigrationCreator::firePostCreateHooks.
func (c *MigrationCreator) firePostCreateHooks(table, path string) {
	for _, callback := range c.postCreate {
		callback(table, path)
	}
}

// AfterCreate answers MigrationCreator::afterCreate.
func (c *MigrationCreator) AfterCreate(callback func(table, path string)) {
	c.postCreate = append(c.postCreate, callback)
}

// GetDatePrefix answers the protected MigrationCreator::getDatePrefix.
//
// The PHP's date('Y_m_d_His') is "2006_01_02_150405" in Go's reference-time
// layout. It is UTC rather than local, which the PHP's is not: a team spread
// over two time zones otherwise generates prefixes that sort against the order
// the migrations were actually written in.
func (c *MigrationCreator) GetDatePrefix() string {
	now := time.Now
	if c.now != nil {
		now = c.now
	}
	return now().UTC().Format("2006_01_02_150405")
}

// StubPath answers MigrationCreator::stubPath.
//
// The PHP answers __DIR__.'/stubs', a directory it reads at run time. The
// built-in stubs here are string constants compiled into the binary, so this
// answers the custom directory -- the only one there is a path to.
func (c *MigrationCreator) StubPath() string { return c.customStubPath }

// The three built-in stubs, in the Go a migration is written in.
//
// They are constants rather than files because a binary that had to find its
// own source directory to generate a file is a binary that works from one
// working directory. The PHP reads them off disk because PHP is already there.
const (
	blankStub = `package migrations

import (
	"context"

	"github.com/arandu-io/hesape/database/migrations"
)

func init() { migrations.Register({{ class }}{}) }

// {{ class }} is a schema change.
//
// It is compatible with the binary that is still running while a rollout
// finishes (RULE 16): a new column is nullable or has a default, and removing
// one takes two releases.
type {{ class }} struct{ migrations.BaseMigration }

// GetName is this migration's identity. The date prefix carries the order.
func ({{ class }}) GetName() string { return "{{ name }}" }

// Up applies the change.
func ({{ class }}) Up(ctx context.Context, conn migrations.Connection) error {
	_, err := conn.Statement(ctx, ` + "`" + `` + "`" + `, nil)
	return err
}

// Down reverses it.
func ({{ class }}) Down(ctx context.Context, conn migrations.Connection) error {
	_, err := conn.Statement(ctx, ` + "`" + `` + "`" + `, nil)
	return err
}
`

	createStub = `package migrations

import (
	"context"

	"github.com/arandu-io/hesape/database/migrations"
)

func init() { migrations.Register({{ class }}{}) }

// {{ class }} creates the {{ table }} table.
type {{ class }} struct{ migrations.BaseMigration }

// GetName is this migration's identity. The date prefix carries the order.
func ({{ class }}) GetName() string { return "{{ name }}" }

// Up creates the table.
func ({{ class }}) Up(ctx context.Context, conn migrations.Connection) error {
	_, err := conn.Statement(ctx, ` + "`" + `CREATE TABLE {{ table }} (
	id         VARCHAR(255) NOT NULL PRIMARY KEY,
	tenant_id  VARCHAR(255) NOT NULL,
	created_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL
)` + "`" + `, nil)
	return err
}

// Down drops it.
func ({{ class }}) Down(ctx context.Context, conn migrations.Connection) error {
	_, err := conn.Statement(ctx, ` + "`" + `DROP TABLE {{ table }}` + "`" + `, nil)
	return err
}
`

	updateStub = `package migrations

import (
	"context"

	"github.com/arandu-io/hesape/database/migrations"
)

func init() { migrations.Register({{ class }}{}) }

// {{ class }} alters the {{ table }} table.
//
// A column added here is nullable or has a default, so the binary of the
// previous release keeps working while the rollout finishes (RULE 16).
type {{ class }} struct{ migrations.BaseMigration }

// GetName is this migration's identity. The date prefix carries the order.
func ({{ class }}) GetName() string { return "{{ name }}" }

// Up applies the change.
func ({{ class }}) Up(ctx context.Context, conn migrations.Connection) error {
	_, err := conn.Statement(ctx, ` + "`" + `ALTER TABLE {{ table }} ` + "`" + `, nil)
	return err
}

// Down reverses it.
func ({{ class }}) Down(ctx context.Context, conn migrations.Connection) error {
	_, err := conn.Statement(ctx, ` + "`" + `ALTER TABLE {{ table }} ` + "`" + `, nil)
	return err
}
`
)
