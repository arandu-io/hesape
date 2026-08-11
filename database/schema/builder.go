package schema

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/auth"
)

// ActionMigrate is the auth.Action every schema change is authorized for.
//
// One action for the whole component, not one per method: a migration run is a
// single decision taken once, by the pipeline that runs it, and RULE 16 already
// says a migration is a pipeline step rather than something a request triggers.
// A runner gets its Grant from auth.SystemGrant, which refuses to issue one
// without a tenant.
const ActionMigrate auth.Action = "schema.migrate"

// defaultStringLength answers Builder::$defaultStringLength.
//
// PHP keeps the three defaults as static properties with same-named setters.
// Go has one namespace, so the value is an unexported package variable and the
// setter keeps the Illuminate name.
var defaultStringLength = 255

// defaultTimePrecisionValue answers Builder::$defaultTimePrecision.
var defaultTimePrecisionValue = intPointer(0)

// defaultMorphKeyType answers Builder::$defaultMorphKeyType.
var defaultMorphKeyType = "int"

// DefaultStringLength answers Builder::defaultStringLength.
func DefaultStringLength(length int) { defaultStringLength = length }

// DefaultTimePrecision answers Builder::defaultTimePrecision. A nil precision
// is the PHP null: the column is declared without one.
func DefaultTimePrecision(precision *int) { defaultTimePrecisionValue = precision }

// DefaultMorphKeyType answers Builder::defaultMorphKeyType. It returns an error
// where the PHP throws InvalidArgumentException.
func DefaultMorphKeyType(typ string) error {
	switch typ {
	case "int", "uuid", "ulid":
		defaultMorphKeyType = typ
		return nil
	default:
		return fmt.Errorf("schema: morph key type must be 'int', 'uuid', or 'ulid', got %q", typ)
	}
}

// MorphUsingUUIDs answers Builder::morphUsingUuids.
func MorphUsingUUIDs() { defaultMorphKeyType = "uuid" }

// MorphUsingULIDs answers Builder::morphUsingUlids.
func MorphUsingULIDs() { defaultMorphKeyType = "ulid" }

func defaultTimePrecision() *int {
	if defaultTimePrecisionValue == nil {
		return nil
	}
	value := *defaultTimePrecisionValue
	return &value
}

func intPointer(value int) *int { return &value }

// ErrUnsupported is what a driver answers for a schema operation it does not
// have, where the PHP throws LogicException or RuntimeException.
var ErrUnsupported = errors.New("schema: this database driver does not support the operation")

// Builder answers Illuminate\Database\Schema\Builder.
//
// It is the half of this component that executes: Blueprint and Grammar build
// strings, and everything that reaches the connection is here. Every method
// that does so takes an auth.Grant and checks it against ActionMigrate before a
// statement leaves. DDL names tables rather than rows, so there is no
// auth.Tenant(g) in the SQL -- see the package documentation for why the Grant
// is required anyway.
type Builder struct {
	connection Connection
	grammar    Grammar
	resolver   func(Connection, string, func(*Blueprint)) *Blueprint
}

// NewBuilder answers Builder::__construct.
func NewBuilder(connection Connection) *Builder {
	return &Builder{connection: connection, grammar: connection.GetSchemaGrammar()}
}

// GetConnection answers Builder::getConnection.
func (b *Builder) GetConnection() Connection { return b.connection }

// BlueprintResolver answers Builder::blueprintResolver.
func (b *Builder) BlueprintResolver(resolver func(Connection, string, func(*Blueprint)) *Blueprint) {
	b.resolver = resolver
}

// CreateDatabase answers Builder::createDatabase.
func (b *Builder) CreateDatabase(ctx context.Context, g auth.Grant, name string) error {
	if err := g.Check(ActionMigrate); err != nil {
		return err
	}
	sql, err := b.grammar.CompileCreateDatabase(name)
	if err != nil {
		return err
	}
	return b.connection.Statement(ctx, sql)
}

// DropDatabaseIfExists answers Builder::dropDatabaseIfExists.
func (b *Builder) DropDatabaseIfExists(ctx context.Context, g auth.Grant, name string) error {
	if err := g.Check(ActionMigrate); err != nil {
		return err
	}
	sql, err := b.grammar.CompileDropDatabaseIfExists(name)
	if err != nil {
		return err
	}
	return b.connection.Statement(ctx, sql)
}

// GetSchemas answers Builder::getSchemas.
func (b *Builder) GetSchemas(ctx context.Context, g auth.Grant) ([]SchemaInfo, error) {
	if err := g.Check(ActionMigrate); err != nil {
		return nil, err
	}
	sql, err := b.grammar.CompileSchemas()
	if err != nil {
		return nil, err
	}
	rows, err := b.connection.Select(ctx, sql)
	if err != nil {
		return nil, err
	}
	return b.connection.ProcessSchemas(rows), nil
}

// HasTable answers Builder::hasTable.
func (b *Builder) HasTable(ctx context.Context, g auth.Grant, table string) (bool, error) {
	if err := g.Check(ActionMigrate); err != nil {
		return false, err
	}

	schema, name, err := ParseSchemaAndTable(table)
	if err != nil {
		return false, err
	}
	name = b.connection.GetTablePrefix() + name

	if sql := b.grammar.CompileTableExists(schema, name); sql != "" {
		value, err := b.connection.Scalar(ctx, sql)
		if err != nil {
			return false, err
		}
		return truthy(value), nil
	}

	if schema == "" {
		schema = b.GetCurrentSchemaName()
	}
	tables, err := b.GetTables(ctx, g, schema)
	if err != nil {
		return false, err
	}
	for _, candidate := range tables {
		if strings.EqualFold(name, candidate.Name) {
			return true, nil
		}
	}
	return false, nil
}

// HasView answers Builder::hasView.
func (b *Builder) HasView(ctx context.Context, g auth.Grant, view string) (bool, error) {
	if err := g.Check(ActionMigrate); err != nil {
		return false, err
	}

	schema, name, err := ParseSchemaAndTable(view)
	if err != nil {
		return false, err
	}
	name = b.connection.GetTablePrefix() + name

	if schema == "" {
		schema = b.GetCurrentSchemaName()
	}
	views, err := b.GetViews(ctx, g, schema)
	if err != nil {
		return false, err
	}
	for _, candidate := range views {
		if strings.EqualFold(name, candidate.Name) {
			return true, nil
		}
	}
	return false, nil
}

// GetTables answers Builder::getTables. An empty schema list is the PHP null:
// every schema the connection can see.
func (b *Builder) GetTables(ctx context.Context, g auth.Grant, schemas ...string) ([]TableInfo, error) {
	if err := g.Check(ActionMigrate); err != nil {
		return nil, err
	}
	sql, err := b.grammar.CompileTables(nonEmpty(schemas))
	if err != nil {
		return nil, err
	}
	rows, err := b.connection.Select(ctx, sql)
	if err != nil {
		return nil, err
	}
	return b.connection.ProcessTables(rows), nil
}

// GetTableListing answers Builder::getTableListing. The first optional argument
// is the schema; schemaQualified defaults to true, as it does in PHP.
func (b *Builder) GetTableListing(ctx context.Context, g auth.Grant, schemas []string, schemaQualified ...bool) ([]string, error) {
	tables, err := b.GetTables(ctx, g, schemas...)
	if err != nil {
		return nil, err
	}
	qualified := true
	if len(schemaQualified) > 0 {
		qualified = schemaQualified[0]
	}
	names := make([]string, 0, len(tables))
	for _, table := range tables {
		if qualified {
			names = append(names, table.SchemaQualifiedName)
			continue
		}
		names = append(names, table.Name)
	}
	return names, nil
}

// GetViews answers Builder::getViews.
func (b *Builder) GetViews(ctx context.Context, g auth.Grant, schemas ...string) ([]ViewInfo, error) {
	if err := g.Check(ActionMigrate); err != nil {
		return nil, err
	}
	sql, err := b.grammar.CompileViews(nonEmpty(schemas))
	if err != nil {
		return nil, err
	}
	rows, err := b.connection.Select(ctx, sql)
	if err != nil {
		return nil, err
	}
	return b.connection.ProcessViews(rows), nil
}

// GetTypes answers Builder::getTypes: the user-defined types in the schema.
func (b *Builder) GetTypes(ctx context.Context, g auth.Grant, schemas ...string) ([]Record, error) {
	if err := g.Check(ActionMigrate); err != nil {
		return nil, err
	}
	sql, err := b.grammar.CompileTypes(nonEmpty(schemas))
	if err != nil {
		return nil, err
	}
	return b.connection.Select(ctx, sql)
}

// DropAllTypes answers Builder::dropAllTypes.
//
// The base class throws LogicException; here the refusal comes from the
// grammar, which is the only thing that knows whether the driver has types to
// drop at all.
func (b *Builder) DropAllTypes(ctx context.Context, g auth.Grant, types []string) error {
	if err := g.Check(ActionMigrate); err != nil {
		return err
	}
	if len(types) == 0 {
		return nil
	}
	sql, err := b.grammar.CompileDropAllTypes(types)
	if err != nil {
		return err
	}
	return b.connection.Statement(ctx, sql)
}

// HasColumn answers Builder::hasColumn.
func (b *Builder) HasColumn(ctx context.Context, g auth.Grant, table, column string) (bool, error) {
	columns, err := b.GetColumnListing(ctx, g, table)
	if err != nil {
		return false, err
	}
	for _, candidate := range columns {
		if strings.EqualFold(candidate, column) {
			return true, nil
		}
	}
	return false, nil
}

// HasColumns answers Builder::hasColumns.
func (b *Builder) HasColumns(ctx context.Context, g auth.Grant, table string, columns []string) (bool, error) {
	existing, err := b.GetColumnListing(ctx, g, table)
	if err != nil {
		return false, err
	}
	for _, column := range columns {
		found := false
		for _, candidate := range existing {
			if strings.EqualFold(candidate, column) {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

// WhenTableHasColumn answers Builder::whenTableHasColumn.
func (b *Builder) WhenTableHasColumn(ctx context.Context, g auth.Grant, table, column string, callback func(*Blueprint)) error {
	has, err := b.HasColumn(ctx, g, table, column)
	if err != nil || !has {
		return err
	}
	return b.Table(ctx, g, table, callback)
}

// WhenTableDoesntHaveColumn answers Builder::whenTableDoesntHaveColumn.
func (b *Builder) WhenTableDoesntHaveColumn(ctx context.Context, g auth.Grant, table, column string, callback func(*Blueprint)) error {
	has, err := b.HasColumn(ctx, g, table, column)
	if err != nil || has {
		return err
	}
	return b.Table(ctx, g, table, callback)
}

// WhenTableHasIndex answers Builder::whenTableHasIndex.
func (b *Builder) WhenTableHasIndex(ctx context.Context, g auth.Grant, table string, index any, callback func(*Blueprint), typ ...string) error {
	has, err := b.HasIndex(ctx, g, table, index, typ...)
	if err != nil || !has {
		return err
	}
	return b.Table(ctx, g, table, callback)
}

// WhenTableDoesntHaveIndex answers Builder::whenTableDoesntHaveIndex.
func (b *Builder) WhenTableDoesntHaveIndex(ctx context.Context, g auth.Grant, table string, index any, callback func(*Blueprint), typ ...string) error {
	has, err := b.HasIndex(ctx, g, table, index, typ...)
	if err != nil || has {
		return err
	}
	return b.Table(ctx, g, table, callback)
}

// GetColumnType answers Builder::getColumnType. It returns an error where the
// PHP throws InvalidArgumentException for a column the table does not have.
func (b *Builder) GetColumnType(ctx context.Context, g auth.Grant, table, column string, fullDefinition ...bool) (string, error) {
	columns, err := b.GetColumns(ctx, g, table)
	if err != nil {
		return "", err
	}
	full := len(fullDefinition) > 0 && fullDefinition[0]
	for _, candidate := range columns {
		if strings.EqualFold(candidate.Name, column) {
			if full {
				return candidate.Type, nil
			}
			return candidate.TypeName, nil
		}
	}
	return "", fmt.Errorf("schema: there is no column with name %q on table %q", column, table)
}

// GetColumnListing answers Builder::getColumnListing.
func (b *Builder) GetColumnListing(ctx context.Context, g auth.Grant, table string) ([]string, error) {
	columns, err := b.GetColumns(ctx, g, table)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name)
	}
	return names, nil
}

// GetColumns answers Builder::getColumns.
func (b *Builder) GetColumns(ctx context.Context, g auth.Grant, table string) ([]ColumnInfo, error) {
	if err := g.Check(ActionMigrate); err != nil {
		return nil, err
	}
	schema, name, err := ParseSchemaAndTable(table)
	if err != nil {
		return nil, err
	}
	sql, err := b.grammar.CompileColumns(schema, b.connection.GetTablePrefix()+name)
	if err != nil {
		return nil, err
	}
	rows, err := b.connection.Select(ctx, sql)
	if err != nil {
		return nil, err
	}
	return b.connection.ProcessColumns(rows), nil
}

// GetIndexes answers Builder::getIndexes.
func (b *Builder) GetIndexes(ctx context.Context, g auth.Grant, table string) ([]IndexInfo, error) {
	if err := g.Check(ActionMigrate); err != nil {
		return nil, err
	}
	schema, name, err := ParseSchemaAndTable(table)
	if err != nil {
		return nil, err
	}
	sql, err := b.grammar.CompileIndexes(schema, b.connection.GetTablePrefix()+name)
	if err != nil {
		return nil, err
	}
	rows, err := b.connection.Select(ctx, sql)
	if err != nil {
		return nil, err
	}
	return b.connection.ProcessIndexes(rows), nil
}

// GetIndexListing answers Builder::getIndexListing.
func (b *Builder) GetIndexListing(ctx context.Context, g auth.Grant, table string) ([]string, error) {
	indexes, err := b.GetIndexes(ctx, g, table)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(indexes))
	for _, index := range indexes {
		names = append(names, index.Name)
	}
	return names, nil
}

// HasIndex answers Builder::hasIndex. The index is a name or the list of
// columns it covers; the optional type narrows the match to primary, unique or
// a driver's own index type.
func (b *Builder) HasIndex(ctx context.Context, g auth.Grant, table string, index any, typ ...string) (bool, error) {
	indexes, err := b.GetIndexes(ctx, g, table)
	if err != nil {
		return false, err
	}

	wanted := ""
	if len(typ) > 0 {
		wanted = strings.ToLower(typ[0])
	}

	for _, candidate := range indexes {
		typeMatches := wanted == "" ||
			(wanted == "primary" && candidate.Primary) ||
			(wanted == "unique" && candidate.Unique) ||
			wanted == candidate.Type
		if !typeMatches {
			continue
		}
		switch v := index.(type) {
		case string:
			if candidate.Name == v {
				return true, nil
			}
		case []string:
			if equalStrings(candidate.Columns, v) {
				return true, nil
			}
		}
	}
	return false, nil
}

// GetForeignKeys answers Builder::getForeignKeys.
func (b *Builder) GetForeignKeys(ctx context.Context, g auth.Grant, table string) ([]ForeignKeyInfo, error) {
	if err := g.Check(ActionMigrate); err != nil {
		return nil, err
	}
	schema, name, err := ParseSchemaAndTable(table)
	if err != nil {
		return nil, err
	}
	sql, err := b.grammar.CompileForeignKeys(schema, b.connection.GetTablePrefix()+name)
	if err != nil {
		return nil, err
	}
	rows, err := b.connection.Select(ctx, sql)
	if err != nil {
		return nil, err
	}
	return b.connection.ProcessForeignKeys(rows), nil
}

// Table answers Builder::table: it modifies a table that already exists.
func (b *Builder) Table(ctx context.Context, g auth.Grant, table string, callback func(*Blueprint)) error {
	return b.build(ctx, g, b.CreateBlueprint(table, callback))
}

// Create answers Builder::create.
func (b *Builder) Create(ctx context.Context, g auth.Grant, table string, callback func(*Blueprint)) error {
	blueprint := b.CreateBlueprint(table, nil)
	blueprint.Create()
	callback(blueprint)
	return b.build(ctx, g, blueprint)
}

// Drop answers Builder::drop.
func (b *Builder) Drop(ctx context.Context, g auth.Grant, table string) error {
	blueprint := b.CreateBlueprint(table, nil)
	blueprint.Drop()
	return b.build(ctx, g, blueprint)
}

// DropIfExists answers Builder::dropIfExists.
func (b *Builder) DropIfExists(ctx context.Context, g auth.Grant, table string) error {
	blueprint := b.CreateBlueprint(table, nil)
	blueprint.DropIfExists()
	return b.build(ctx, g, blueprint)
}

// DropColumns answers Builder::dropColumns.
func (b *Builder) DropColumns(ctx context.Context, g auth.Grant, table string, columns []string) error {
	return b.Table(ctx, g, table, func(blueprint *Blueprint) {
		blueprint.DropColumn(columns...)
	})
}

// DropAllTables answers Builder::dropAllTables.
//
// The base class throws LogicException; here it is ErrUnsupported, and a driver
// whose grammar can compile the statement answers with the statement instead.
func (b *Builder) DropAllTables(ctx context.Context, g auth.Grant) error {
	if err := g.Check(ActionMigrate); err != nil {
		return err
	}
	tables, err := b.GetTables(ctx, g)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return nil
	}
	names := make([]string, 0, len(tables))
	for _, table := range tables {
		names = append(names, table.SchemaQualifiedName)
	}
	sql, err := b.grammar.CompileDropAllTables(names)
	if err != nil {
		return err
	}
	return b.connection.Statement(ctx, sql)
}

// DropAllViews answers Builder::dropAllViews.
func (b *Builder) DropAllViews(ctx context.Context, g auth.Grant) error {
	if err := g.Check(ActionMigrate); err != nil {
		return err
	}
	views, err := b.GetViews(ctx, g)
	if err != nil {
		return err
	}
	if len(views) == 0 {
		return nil
	}
	names := make([]string, 0, len(views))
	for _, view := range views {
		names = append(names, view.SchemaQualifiedName)
	}
	sql, err := b.grammar.CompileDropAllViews(names)
	if err != nil {
		return err
	}
	return b.connection.Statement(ctx, sql)
}

// Rename answers Builder::rename.
func (b *Builder) Rename(ctx context.Context, g auth.Grant, from, to string) error {
	blueprint := b.CreateBlueprint(from, nil)
	blueprint.Rename(to)
	return b.build(ctx, g, blueprint)
}

// EnableForeignKeyConstraints answers Builder::enableForeignKeyConstraints.
func (b *Builder) EnableForeignKeyConstraints(ctx context.Context, g auth.Grant) error {
	if err := g.Check(ActionMigrate); err != nil {
		return err
	}
	sql, err := b.grammar.CompileEnableForeignKeyConstraints()
	if err != nil {
		return err
	}
	return b.connection.Statement(ctx, sql)
}

// DisableForeignKeyConstraints answers Builder::disableForeignKeyConstraints.
func (b *Builder) DisableForeignKeyConstraints(ctx context.Context, g auth.Grant) error {
	if err := g.Check(ActionMigrate); err != nil {
		return err
	}
	sql, err := b.grammar.CompileDisableForeignKeyConstraints()
	if err != nil {
		return err
	}
	return b.connection.Statement(ctx, sql)
}

// WithoutForeignKeyConstraints answers Builder::withoutForeignKeyConstraints.
//
// The constraints are put back whether the callback succeeded or not, which is
// what the PHP's finally does. A failure to put them back is reported even when
// the callback already failed, joined to it, because a connection left with its
// foreign keys off is the more dangerous of the two.
func (b *Builder) WithoutForeignKeyConstraints(ctx context.Context, g auth.Grant, callback func() error) error {
	if err := b.DisableForeignKeyConstraints(ctx, g); err != nil {
		return err
	}
	err := callback()
	return errors.Join(err, b.EnableForeignKeyConstraints(ctx, g))
}

// EnsureVectorExtensionExists answers Builder::ensureVectorExtensionExists.
func (b *Builder) EnsureVectorExtensionExists(ctx context.Context, g auth.Grant, schema ...string) error {
	return b.EnsureExtensionExists(ctx, g, "vector", schema...)
}

// EnsureExtensionExists answers Builder::ensureExtensionExists. It returns an
// error where the PHP throws RuntimeException on a driver that is not Postgres.
func (b *Builder) EnsureExtensionExists(ctx context.Context, g auth.Grant, name string, schema ...string) error {
	if err := g.Check(ActionMigrate); err != nil {
		return err
	}
	if b.connection.GetDriverName() != "pgsql" {
		return fmt.Errorf("%w: extensions are only supported by Postgres", ErrUnsupported)
	}
	sql := "create extension if not exists " + b.grammar.Wrap(name)
	if len(schema) > 0 && schema[0] != "" {
		sql += " schema " + b.grammar.Wrap(schema[0])
	}
	return b.connection.Statement(ctx, sql)
}

// build answers Builder::build.
func (b *Builder) build(ctx context.Context, g auth.Grant, blueprint *Blueprint) error {
	return blueprint.Build(ctx, g)
}

// CreateBlueprint answers Builder::createBlueprint.
func (b *Builder) CreateBlueprint(table string, callback func(*Blueprint)) *Blueprint {
	if b.resolver != nil {
		return b.resolver(b.connection, table, callback)
	}
	return NewBlueprint(b.connection, table, callback)
}

// GetCurrentSchemaListing answers Builder::getCurrentSchemaListing.
func (b *Builder) GetCurrentSchemaListing() []string { return nil }

// GetCurrentSchemaName answers Builder::getCurrentSchemaName.
func (b *Builder) GetCurrentSchemaName() string {
	listing := b.GetCurrentSchemaListing()
	if len(listing) == 0 {
		return ""
	}
	return listing[0]
}

// ParseSchemaAndTable answers Builder::parseSchemaAndTable.
//
// It is a package function rather than only a method because the SQLite and
// Postgres grammars call it while compiling, where they have a blueprint but no
// builder. The PHP reaches it through the connection's schema builder for the
// same use; one implementation is one behaviour, and the method below is the
// Illuminate name for it.
func ParseSchemaAndTable(reference string) (schema, table string, err error) {
	segments := strings.Split(reference, ".")
	if len(segments) > 2 {
		return "", "", fmt.Errorf("schema: using three-part references is not supported, name the connection instead of %q", reference)
	}
	if len(segments) == 2 {
		return segments[0], segments[1], nil
	}
	return "", segments[0], nil
}

// ParseSchemaAndTable answers Builder::parseSchemaAndTable. The optional
// argument is the PHP's $withDefaultSchema: a schema name to fall back on, or
// the empty string to fall back on the connection's current schema.
func (b *Builder) ParseSchemaAndTable(reference string, withDefaultSchema ...string) (schema, table string, err error) {
	schema, table, err = ParseSchemaAndTable(reference)
	if err != nil || schema != "" || len(withDefaultSchema) == 0 {
		return schema, table, err
	}
	if withDefaultSchema[0] != "" {
		return withDefaultSchema[0], table, nil
	}
	return b.GetCurrentSchemaName(), table, nil
}

func nonEmpty(values []string) []string {
	out := values[:0:0]
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// truthy reads the value a driver returns for an "exists" query, which is a
// bool on one engine, an int on another and a string on a third.
func truthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case []byte:
		return len(v) > 0 && string(v) != "0"
	case string:
		return v != "" && v != "0"
	default:
		return false
	}
}
