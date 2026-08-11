package schema

import (
	"context"
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// Blueprint answers Illuminate\Database\Schema\Blueprint.
//
// It is the value a migration writes against: the table under construction, its
// columns and the commands that will be run for it. It builds no SQL itself --
// ToSQL hands each command to the Grammar -- and it takes no auth.Grant,
// because deciding nothing is the point of it. Builder is the half that
// executes, and that is where the Grant is required.
type Blueprint struct {
	connection Connection
	grammar    Grammar

	table    string
	columns  []*ColumnDefinition
	commands []*Command

	engine    string
	charset   string
	collation string
	temporary bool
	after     string

	state *BlueprintState
}

// NewBlueprint answers Blueprint::__construct.
func NewBlueprint(connection Connection, table string, callback func(*Blueprint)) *Blueprint {
	b := &Blueprint{
		connection: connection,
		grammar:    connection.GetSchemaGrammar(),
		table:      table,
	}
	if callback != nil {
		callback(b)
	}
	return b
}

// Build answers Blueprint::build: it runs the blueprint against the database.
func (b *Blueprint) Build(ctx context.Context, g auth.Grant) error {
	statements, err := b.ToSQL(ctx, g)
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if err := b.connection.Statement(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// ToSQL answers Blueprint::toSql. The PHP spells it toSql; an initialism is
// upper case here.
//
// PHP takes no arguments. This takes a context and an auth.Grant because on
// SQLite a blueprint that alters a table has to read the table's current shape
// from the server before it can rewrite it, and a read is a read: RULE 17 does
// not have a schema exception. Every other driver, and SQLite creating a table
// rather than altering one, touches nothing -- but the signature cannot say
// "sometimes", so it says what the worst case does.
func (b *Blueprint) ToSQL(ctx context.Context, g auth.Grant) ([]string, error) {
	if err := g.Check(ActionMigrate); err != nil {
		return nil, err
	}
	if err := b.addImpliedCommands(ctx, g); err != nil {
		return nil, err
	}

	var statements []string

	for _, command := range b.commands {
		if command.ShouldBeSkipped {
			continue
		}
		if b.state != nil {
			b.state.Update(command)
		}
		sql, err := compileCommand(b.grammar, b, command)
		if err != nil {
			return nil, err
		}
		statements = append(statements, sql...)
	}

	return statements, nil
}

// addImpliedCommands answers Blueprint::addImpliedCommands.
func (b *Blueprint) addImpliedCommands(ctx context.Context, g auth.Grant) error {
	b.addFluentIndexes()
	b.AddFluentCommands()

	if b.Creating() {
		return nil
	}

	for _, command := range b.commands {
		if !command.isColumnDefinition {
			continue
		}
		command.isColumnDefinition = false
		if command.Column.change {
			command.Name = "change"
		} else {
			command.Name = "add"
		}
	}

	return b.AddAlterCommands(ctx, g)
}

// addFluentIndexes answers Blueprint::addFluentIndexes.
func (b *Blueprint) addFluentIndexes() {
	isMySQL := b.connection.GetDriverName() == "mysql" || b.connection.GetDriverName() == "mariadb"

	for _, column := range b.columns {
		for _, index := range []string{"primary", "unique", "index", "fulltext", "spatialIndex", "vectorIndex"} {
			marker := column.marker(index)

			// A column being changed into an auto increment column needs no
			// primary key command on MySQL: the column definition carries it.
			if index == "primary" && column.autoIncrement && column.change && isMySQL {
				break
			}

			if marker == nil {
				continue
			}

			if on, ok := marker.(bool); ok {
				if on {
					b.fluentIndex(b.indexMethod(index, column), column.name, "")
					column.setMarker(index, nil)
					break
				}
				// A false marker on a changed column drops the index the
				// conventional name would have produced.
				if column.change {
					b.dropFluentIndex(index, column.name)
					column.setMarker(index, nil)
					break
				}
				continue
			}

			if name, ok := marker.(string); ok {
				b.fluentIndex(b.indexMethod(index, column), column.name, name)
				column.setMarker(index, nil)
				break
			}
		}
	}
}

// indexMethod answers the PHP's choice between index and vectorIndex for a
// vector column that only said "index".
func (b *Blueprint) indexMethod(index string, column *ColumnDefinition) string {
	if index == "index" && column.typ == "vector" {
		return "vectorIndex"
	}
	return index
}

func (b *Blueprint) fluentIndex(method, column, name string) {
	switch method {
	case "primary":
		b.Primary(column, name)
	case "unique":
		b.Unique(column, name)
	case "index":
		b.Index(column, name)
	case "fulltext":
		b.FullText(column, name)
	case "spatialIndex":
		b.SpatialIndex(column, name)
	case "vectorIndex":
		b.VectorIndex(column, name)
	}
}

func (b *Blueprint) dropFluentIndex(index, column string) {
	switch index {
	case "primary":
		b.DropPrimary([]string{column})
	case "unique":
		b.DropUnique([]string{column})
	case "index":
		b.DropIndex([]string{column})
	case "fulltext":
		b.DropFullText([]string{column})
	case "spatialIndex":
		b.DropSpatialIndex([]string{column})
	}
}

// AddFluentCommands answers Blueprint::addFluentCommands.
func (b *Blueprint) AddFluentCommands() {
	for _, column := range b.columns {
		for _, name := range b.grammar.GetFluentCommands() {
			command := NewCommand(name)
			command.Column = column
			b.commands = append(b.commands, command)
		}
	}
}

// AddAlterCommands answers Blueprint::addAlterCommands.
//
// PHP returns early unless the grammar is the SQLite one. Here the test is
// whether the grammar declares any alter command at all, which is the same
// question: SQLite is the only driver whose GetAlterCommands answers with
// anything, because it is the only one that rebuilds the table.
//
// It takes a context and a Grant where the PHP takes nothing, because a
// blueprint that has an alter command reads the table's current shape from the
// server to build its BlueprintState.
func (b *Blueprint) AddAlterCommands(ctx context.Context, g auth.Grant) error {
	alterCommands := b.grammar.GetAlterCommands()
	if len(alterCommands) == 0 {
		return nil
	}

	var commands []*Command
	lastCommandWasAlter, hasAlterCommand := false, false

	for _, command := range b.commands {
		if contains(alterCommands, command.Name) {
			hasAlterCommand = true
			lastCommandWasAlter = true
		} else if lastCommandWasAlter {
			commands = append(commands, NewCommand("alter"))
			lastCommandWasAlter = false
		}
		commands = append(commands, command)
	}

	if lastCommandWasAlter {
		commands = append(commands, NewCommand("alter"))
	}

	if hasAlterCommand {
		state, err := NewBlueprintState(ctx, g, b, b.connection)
		if err != nil {
			return err
		}
		b.state = state
	}

	b.commands = commands
	return nil
}

// Creating answers Blueprint::creating.
func (b *Blueprint) Creating() bool {
	for _, command := range b.commands {
		if !command.isColumnDefinition && command.Name == "create" {
			return true
		}
	}
	return false
}

// Create answers Blueprint::create.
func (b *Blueprint) Create() *Command { return b.addCommand("create") }

// Engine answers Blueprint::engine.
func (b *Blueprint) Engine(engine string) { b.engine = engine }

// GetEngine answers $blueprint->engine.
func (b *Blueprint) GetEngine() string { return b.engine }

// InnoDb answers Blueprint::innoDb.
func (b *Blueprint) InnoDb() { b.Engine("InnoDB") }

// Charset answers Blueprint::charset.
func (b *Blueprint) Charset(charset string) { b.charset = charset }

// GetCharset answers $blueprint->charset.
func (b *Blueprint) GetCharset() string { return b.charset }

// Collation answers Blueprint::collation.
func (b *Blueprint) Collation(collation string) { b.collation = collation }

// GetCollation answers $blueprint->collation.
func (b *Blueprint) GetCollation() string { return b.collation }

// Temporary answers Blueprint::temporary.
func (b *Blueprint) Temporary() { b.temporary = true }

// GetTemporary answers $blueprint->temporary.
func (b *Blueprint) GetTemporary() bool { return b.temporary }

// Drop answers Blueprint::drop.
func (b *Blueprint) Drop() *Command { return b.addCommand("drop") }

// DropIfExists answers Blueprint::dropIfExists.
func (b *Blueprint) DropIfExists() *Command { return b.addCommand("dropIfExists") }

// DropColumn answers Blueprint::dropColumn.
func (b *Blueprint) DropColumn(columns ...string) *Command {
	command := b.addCommand("dropColumn")
	command.Columns = toAnyList(columns)
	return command
}

// RenameColumn answers Blueprint::renameColumn.
func (b *Blueprint) RenameColumn(from, to string) *Command {
	command := b.addCommand("renameColumn")
	command.From, command.To = from, to
	return command
}

// DropPrimary answers Blueprint::dropPrimary. The argument is the index name,
// or the columns it was built from.
func (b *Blueprint) DropPrimary(index any) *Command {
	return b.dropIndexCommand("dropPrimary", "primary", index)
}

// DropUnique answers Blueprint::dropUnique.
func (b *Blueprint) DropUnique(index any) *Command {
	return b.dropIndexCommand("dropUnique", "unique", index)
}

// DropIndex answers Blueprint::dropIndex.
func (b *Blueprint) DropIndex(index any) *Command {
	return b.dropIndexCommand("dropIndex", "index", index)
}

// DropFullText answers Blueprint::dropFullText.
func (b *Blueprint) DropFullText(index any) *Command {
	return b.dropIndexCommand("dropFullText", "fulltext", index)
}

// DropSpatialIndex answers Blueprint::dropSpatialIndex.
func (b *Blueprint) DropSpatialIndex(index any) *Command {
	return b.dropIndexCommand("dropSpatialIndex", "spatialIndex", index)
}

// DropForeign answers Blueprint::dropForeign.
func (b *Blueprint) DropForeign(index any) *Command {
	return b.dropIndexCommand("dropForeign", "foreign", index)
}

// DropConstrainedForeignID answers Blueprint::dropConstrainedForeignId.
func (b *Blueprint) DropConstrainedForeignID(column string) *Command {
	b.DropForeign([]string{column})
	return b.DropColumn(column)
}

// DropForeignIDFor answers Blueprint::dropForeignIdFor. An empty column takes
// the model's foreign key, which is the PHP null.
func (b *Blueprint) DropForeignIDFor(model Model, column string) *Command {
	if column == "" {
		column = model.GetForeignKey()
	}
	return b.DropColumn(column)
}

// DropConstrainedForeignIDFor answers Blueprint::dropConstrainedForeignIdFor.
func (b *Blueprint) DropConstrainedForeignIDFor(model Model, column string) *Command {
	if column == "" {
		column = model.GetForeignKey()
	}
	return b.DropConstrainedForeignID(column)
}

// RenameIndex answers Blueprint::renameIndex.
func (b *Blueprint) RenameIndex(from, to string) *Command {
	command := b.addCommand("renameIndex")
	command.From, command.To = from, to
	return command
}

// DropTimestamps answers Blueprint::dropTimestamps.
func (b *Blueprint) DropTimestamps() { b.DropColumn("created_at", "updated_at") }

// DropTimestampsTz answers Blueprint::dropTimestampsTz.
func (b *Blueprint) DropTimestampsTz() { b.DropTimestamps() }

// DropSoftDeletes answers Blueprint::dropSoftDeletes. An empty column is the
// PHP default, deleted_at.
func (b *Blueprint) DropSoftDeletes(column string) {
	if column == "" {
		column = "deleted_at"
	}
	b.DropColumn(column)
}

// DropSoftDeletesTz answers Blueprint::dropSoftDeletesTz.
func (b *Blueprint) DropSoftDeletesTz(column string) { b.DropSoftDeletes(column) }

// DropRememberToken answers Blueprint::dropRememberToken.
func (b *Blueprint) DropRememberToken() { b.DropColumn("remember_token") }

// DropMorphs answers Blueprint::dropMorphs.
func (b *Blueprint) DropMorphs(name string, indexName ...string) {
	index := arg(indexName, 0)
	if index == "" {
		index = b.createIndexName("index", []any{name + "_type", name + "_id"})
	}
	b.DropIndex(index)
	b.DropColumn(name+"_type", name+"_id")
}

// Rename answers Blueprint::rename.
func (b *Blueprint) Rename(to string) *Command {
	command := b.addCommand("rename")
	command.To = to
	return command
}

// Primary answers Blueprint::primary. The optional arguments are the index name
// and the algorithm.
func (b *Blueprint) Primary(columns any, args ...string) *IndexDefinition {
	return b.indexCommand("primary", columns, arg(args, 0), arg(args, 1), "")
}

// Unique answers Blueprint::unique.
func (b *Blueprint) Unique(columns any, args ...string) *IndexDefinition {
	return b.indexCommand("unique", columns, arg(args, 0), arg(args, 1), "")
}

// Index answers Blueprint::index.
func (b *Blueprint) Index(columns any, args ...string) *IndexDefinition {
	return b.indexCommand("index", columns, arg(args, 0), arg(args, 1), "")
}

// FullText answers Blueprint::fullText.
func (b *Blueprint) FullText(columns any, args ...string) *IndexDefinition {
	return b.indexCommand("fulltext", columns, arg(args, 0), arg(args, 1), "")
}

// SpatialIndex answers Blueprint::spatialIndex. The optional arguments are the
// index name and the operator class.
func (b *Blueprint) SpatialIndex(columns any, args ...string) *IndexDefinition {
	return b.indexCommand("spatialIndex", columns, arg(args, 0), "", arg(args, 1))
}

// VectorIndex answers Blueprint::vectorIndex.
func (b *Blueprint) VectorIndex(column any, name ...string) *IndexDefinition {
	return b.indexCommand("vectorIndex", column, arg(name, 0), "hnsw", "vector_cosine_ops")
}

// RawIndex answers Blueprint::rawIndex: an index over an expression rather than
// a column list.
func (b *Blueprint) RawIndex(expression, name string) *IndexDefinition {
	return b.Index([]any{query.Raw(expression)}, name)
}

// Foreign answers Blueprint::foreign.
func (b *Blueprint) Foreign(columns any, name ...string) *ForeignKeyDefinition {
	definition := b.indexCommand("foreign", columns, arg(name, 0), "", "")
	return &ForeignKeyDefinition{c: definition.c}
}

// ID answers Blueprint::id. An empty column is the PHP default, id.
func (b *Blueprint) ID(column ...string) *ColumnDefinition {
	return b.BigIncrements(defaultTo(arg(column, 0), "id"))
}

// Increments answers Blueprint::increments.
func (b *Blueprint) Increments(column string) *ColumnDefinition {
	return b.UnsignedInteger(column, true)
}

// IntegerIncrements answers Blueprint::integerIncrements.
func (b *Blueprint) IntegerIncrements(column string) *ColumnDefinition {
	return b.UnsignedInteger(column, true)
}

// TinyIncrements answers Blueprint::tinyIncrements.
func (b *Blueprint) TinyIncrements(column string) *ColumnDefinition {
	return b.UnsignedTinyInteger(column, true)
}

// SmallIncrements answers Blueprint::smallIncrements.
func (b *Blueprint) SmallIncrements(column string) *ColumnDefinition {
	return b.UnsignedSmallInteger(column, true)
}

// MediumIncrements answers Blueprint::mediumIncrements.
func (b *Blueprint) MediumIncrements(column string) *ColumnDefinition {
	return b.UnsignedMediumInteger(column, true)
}

// BigIncrements answers Blueprint::bigIncrements.
func (b *Blueprint) BigIncrements(column string) *ColumnDefinition {
	return b.UnsignedBigInteger(column, true)
}

// Char answers Blueprint::char. An omitted length takes the package default,
// which DefaultStringLength sets.
func (b *Blueprint) Char(column string, length ...int) *ColumnDefinition {
	n := defaultStringLength
	if len(length) > 0 {
		n = length[0]
	}
	c := b.AddColumn("char", column)
	c.length = &n
	return c
}

// String answers Blueprint::string.
func (b *Blueprint) String(column string, length ...int) *ColumnDefinition {
	n := defaultStringLength
	if len(length) > 0 && length[0] != 0 {
		n = length[0]
	}
	c := b.AddColumn("string", column)
	c.length = &n
	return c
}

// TinyText answers Blueprint::tinyText.
func (b *Blueprint) TinyText(column string) *ColumnDefinition {
	return b.AddColumn("tinyText", column)
}

// Text answers Blueprint::text.
func (b *Blueprint) Text(column string) *ColumnDefinition { return b.AddColumn("text", column) }

// MediumText answers Blueprint::mediumText.
func (b *Blueprint) MediumText(column string) *ColumnDefinition {
	return b.AddColumn("mediumText", column)
}

// LongText answers Blueprint::longText.
func (b *Blueprint) LongText(column string) *ColumnDefinition {
	return b.AddColumn("longText", column)
}

// Integer answers Blueprint::integer. The optional arguments are autoIncrement
// and unsigned.
func (b *Blueprint) Integer(column string, args ...bool) *ColumnDefinition {
	return b.intColumn("integer", column, args)
}

// TinyInteger answers Blueprint::tinyInteger.
func (b *Blueprint) TinyInteger(column string, args ...bool) *ColumnDefinition {
	return b.intColumn("tinyInteger", column, args)
}

// SmallInteger answers Blueprint::smallInteger.
func (b *Blueprint) SmallInteger(column string, args ...bool) *ColumnDefinition {
	return b.intColumn("smallInteger", column, args)
}

// MediumInteger answers Blueprint::mediumInteger.
func (b *Blueprint) MediumInteger(column string, args ...bool) *ColumnDefinition {
	return b.intColumn("mediumInteger", column, args)
}

// BigInteger answers Blueprint::bigInteger.
func (b *Blueprint) BigInteger(column string, args ...bool) *ColumnDefinition {
	return b.intColumn("bigInteger", column, args)
}

func (b *Blueprint) intColumn(typ, column string, args []bool) *ColumnDefinition {
	c := b.AddColumn(typ, column)
	if len(args) > 0 {
		c.autoIncrement = args[0]
	}
	if len(args) > 1 {
		c.unsigned = args[1]
	}
	return c
}

// UnsignedInteger answers Blueprint::unsignedInteger.
func (b *Blueprint) UnsignedInteger(column string, autoIncrement ...bool) *ColumnDefinition {
	return b.Integer(column, boolAt(autoIncrement, 0), true)
}

// UnsignedTinyInteger answers Blueprint::unsignedTinyInteger.
func (b *Blueprint) UnsignedTinyInteger(column string, autoIncrement ...bool) *ColumnDefinition {
	return b.TinyInteger(column, boolAt(autoIncrement, 0), true)
}

// UnsignedSmallInteger answers Blueprint::unsignedSmallInteger.
func (b *Blueprint) UnsignedSmallInteger(column string, autoIncrement ...bool) *ColumnDefinition {
	return b.SmallInteger(column, boolAt(autoIncrement, 0), true)
}

// UnsignedMediumInteger answers Blueprint::unsignedMediumInteger.
func (b *Blueprint) UnsignedMediumInteger(column string, autoIncrement ...bool) *ColumnDefinition {
	return b.MediumInteger(column, boolAt(autoIncrement, 0), true)
}

// UnsignedBigInteger answers Blueprint::unsignedBigInteger.
func (b *Blueprint) UnsignedBigInteger(column string, autoIncrement ...bool) *ColumnDefinition {
	return b.BigInteger(column, boolAt(autoIncrement, 0), true)
}

// ForeignID answers Blueprint::foreignId: an unsigned big integer meant to
// carry another table's key, which Constrained turns into a foreign key.
func (b *Blueprint) ForeignID(column string) *ForeignIDColumnDefinition {
	definition := &ForeignIDColumnDefinition{
		ColumnDefinition: &ColumnDefinition{typ: "bigInteger", name: column, unsigned: true},
		blueprint:        b,
	}
	b.addColumnDefinition(definition.ColumnDefinition)
	return definition
}

// ForeignIDFor answers Blueprint::foreignIdFor. An empty column takes the
// model's foreign key.
func (b *Blueprint) ForeignIDFor(model Model, column string) *ForeignIDColumnDefinition {
	if column == "" {
		column = model.GetForeignKey()
	}

	if model.GetKeyType() == "int" {
		definition := b.ForeignID(column)
		definition.Table(model.GetTable())
		definition.ReferencesModelColumn(model.GetKeyName())
		return definition
	}

	if ulid, ok := model.(ULIDModel); ok && ulid.UsesULIDs() {
		definition := b.ForeignULID(column, 26)
		definition.Table(model.GetTable())
		definition.ReferencesModelColumn(model.GetKeyName())
		return definition
	}

	definition := b.ForeignUUID(column)
	definition.Table(model.GetTable())
	definition.ReferencesModelColumn(model.GetKeyName())
	return definition
}

// Float answers Blueprint::float. The PHP default precision is 53.
func (b *Blueprint) Float(column string, precision ...int) *ColumnDefinition {
	n := 53
	if len(precision) > 0 {
		n = precision[0]
	}
	c := b.AddColumn("float", column)
	c.precision = &n
	return c
}

// Double answers Blueprint::double.
func (b *Blueprint) Double(column string) *ColumnDefinition { return b.AddColumn("double", column) }

// Decimal answers Blueprint::decimal. The optional arguments are the total
// digits and the decimal places; the PHP defaults are 8 and 2.
func (b *Blueprint) Decimal(column string, args ...int) *ColumnDefinition {
	total, places := 8, 2
	if len(args) > 0 {
		total = args[0]
	}
	if len(args) > 1 {
		places = args[1]
	}
	c := b.AddColumn("decimal", column)
	c.total, c.places = total, places
	return c
}

// Boolean answers Blueprint::boolean.
func (b *Blueprint) Boolean(column string) *ColumnDefinition {
	return b.AddColumn("boolean", column)
}

// Enum answers Blueprint::enum.
func (b *Blueprint) Enum(column string, allowed []string) *ColumnDefinition {
	c := b.AddColumn("enum", column)
	c.allowed = allowed
	return c
}

// Set answers Blueprint::set.
func (b *Blueprint) Set(column string, allowed []string) *ColumnDefinition {
	c := b.AddColumn("set", column)
	c.allowed = allowed
	return c
}

// JSON answers Blueprint::json. The PHP spells it json; an initialism is upper
// case here.
func (b *Blueprint) JSON(column string) *ColumnDefinition { return b.AddColumn("json", column) }

// JSONB answers Blueprint::jsonb.
func (b *Blueprint) JSONB(column string) *ColumnDefinition { return b.AddColumn("jsonb", column) }

// Date answers Blueprint::date.
func (b *Blueprint) Date(column string) *ColumnDefinition { return b.AddColumn("date", column) }

// DateTime answers Blueprint::dateTime. An omitted precision takes the package
// default, which DefaultTimePrecision sets.
func (b *Blueprint) DateTime(column string, precision ...int) *ColumnDefinition {
	return b.timeColumn("dateTime", column, precision)
}

// DateTimeTz answers Blueprint::dateTimeTz.
func (b *Blueprint) DateTimeTz(column string, precision ...int) *ColumnDefinition {
	return b.timeColumn("dateTimeTz", column, precision)
}

// Time answers Blueprint::time.
func (b *Blueprint) Time(column string, precision ...int) *ColumnDefinition {
	return b.timeColumn("time", column, precision)
}

// TimeTz answers Blueprint::timeTz.
func (b *Blueprint) TimeTz(column string, precision ...int) *ColumnDefinition {
	return b.timeColumn("timeTz", column, precision)
}

// Timestamp answers Blueprint::timestamp.
func (b *Blueprint) Timestamp(column string, precision ...int) *ColumnDefinition {
	return b.timeColumn("timestamp", column, precision)
}

// TimestampTz answers Blueprint::timestampTz.
func (b *Blueprint) TimestampTz(column string, precision ...int) *ColumnDefinition {
	return b.timeColumn("timestampTz", column, precision)
}

func (b *Blueprint) timeColumn(typ, column string, precision []int) *ColumnDefinition {
	c := b.AddColumn(typ, column)
	if len(precision) > 0 {
		n := precision[0]
		c.precision = &n
	} else {
		c.precision = defaultTimePrecision()
	}
	return c
}

// Timestamps answers Blueprint::timestamps: nullable created_at and updated_at.
func (b *Blueprint) Timestamps(precision ...int) []*ColumnDefinition {
	return []*ColumnDefinition{
		b.Timestamp("created_at", precision...).Nullable(),
		b.Timestamp("updated_at", precision...).Nullable(),
	}
}

// NullableTimestamps answers Blueprint::nullableTimestamps, an alias of
// Timestamps.
func (b *Blueprint) NullableTimestamps(precision ...int) []*ColumnDefinition {
	return b.Timestamps(precision...)
}

// TimestampsTz answers Blueprint::timestampsTz.
func (b *Blueprint) TimestampsTz(precision ...int) []*ColumnDefinition {
	return []*ColumnDefinition{
		b.TimestampTz("created_at", precision...).Nullable(),
		b.TimestampTz("updated_at", precision...).Nullable(),
	}
}

// NullableTimestampsTz answers Blueprint::nullableTimestampsTz.
func (b *Blueprint) NullableTimestampsTz(precision ...int) []*ColumnDefinition {
	return b.TimestampsTz(precision...)
}

// Datetimes answers Blueprint::datetimes.
func (b *Blueprint) Datetimes(precision ...int) []*ColumnDefinition {
	return []*ColumnDefinition{
		b.DateTime("created_at", precision...).Nullable(),
		b.DateTime("updated_at", precision...).Nullable(),
	}
}

// SoftDeletes answers Blueprint::softDeletes. An empty column is the PHP
// default, deleted_at.
func (b *Blueprint) SoftDeletes(column string, precision ...int) *ColumnDefinition {
	return b.Timestamp(defaultTo(column, "deleted_at"), precision...).Nullable()
}

// SoftDeletesTz answers Blueprint::softDeletesTz.
func (b *Blueprint) SoftDeletesTz(column string, precision ...int) *ColumnDefinition {
	return b.TimestampTz(defaultTo(column, "deleted_at"), precision...).Nullable()
}

// SoftDeletesDatetime answers Blueprint::softDeletesDatetime.
func (b *Blueprint) SoftDeletesDatetime(column string, precision ...int) *ColumnDefinition {
	return b.DateTime(defaultTo(column, "deleted_at"), precision...).Nullable()
}

// Year answers Blueprint::year.
func (b *Blueprint) Year(column string) *ColumnDefinition { return b.AddColumn("year", column) }

// Binary answers Blueprint::binary.
//
// The PHP defaults are a null length and false for fixed. Go has no default
// arguments and the two are of different types, so both are required: pass 0
// and false for the PHP defaults.
func (b *Blueprint) Binary(column string, length int, fixed bool) *ColumnDefinition {
	c := b.AddColumn("binary", column)
	if length != 0 {
		c.length = &length
	}
	c.fixed = fixed
	return c
}

// UUID answers Blueprint::uuid. An empty column is the PHP default, uuid.
func (b *Blueprint) UUID(column ...string) *ColumnDefinition {
	return b.AddColumn("uuid", defaultTo(arg(column, 0), "uuid"))
}

// ForeignUUID answers Blueprint::foreignUuid.
func (b *Blueprint) ForeignUUID(column string) *ForeignIDColumnDefinition {
	definition := &ForeignIDColumnDefinition{
		ColumnDefinition: &ColumnDefinition{typ: "uuid", name: column},
		blueprint:        b,
	}
	b.addColumnDefinition(definition.ColumnDefinition)
	return definition
}

// ULID answers Blueprint::ulid. An empty column is the PHP default, ulid, and
// an omitted length is 26.
func (b *Blueprint) ULID(column string, length ...int) *ColumnDefinition {
	n := 26
	if len(length) > 0 {
		n = length[0]
	}
	return b.Char(defaultTo(column, "ulid"), n)
}

// ForeignULID answers Blueprint::foreignUlid.
func (b *Blueprint) ForeignULID(column string, length ...int) *ForeignIDColumnDefinition {
	n := 26
	if len(length) > 0 {
		n = length[0]
	}
	definition := &ForeignIDColumnDefinition{
		ColumnDefinition: &ColumnDefinition{typ: "char", name: column, length: &n},
		blueprint:        b,
	}
	b.addColumnDefinition(definition.ColumnDefinition)
	return definition
}

// IPAddress answers Blueprint::ipAddress. An empty column is the PHP default,
// ip_address.
func (b *Blueprint) IPAddress(column ...string) *ColumnDefinition {
	return b.AddColumn("ipAddress", defaultTo(arg(column, 0), "ip_address"))
}

// MacAddress answers Blueprint::macAddress. An empty column is the PHP default,
// mac_address.
func (b *Blueprint) MacAddress(column ...string) *ColumnDefinition {
	return b.AddColumn("macAddress", defaultTo(arg(column, 0), "mac_address"))
}

// Geometry answers Blueprint::geometry. An empty subtype is the PHP null, and
// an omitted SRID is the PHP default of 0.
func (b *Blueprint) Geometry(column string, subtype string, srid ...int) *ColumnDefinition {
	c := b.AddColumn("geometry", column)
	c.subtype = subtype
	if len(srid) > 0 {
		c.srid = srid[0]
	}
	return c
}

// Geography answers Blueprint::geography. An omitted SRID is the PHP default of
// 4326, which is why it is variadic rather than a plain int.
func (b *Blueprint) Geography(column string, subtype string, srid ...int) *ColumnDefinition {
	c := b.AddColumn("geography", column)
	c.subtype = subtype
	c.srid = 4326
	if len(srid) > 0 {
		c.srid = srid[0]
	}
	return c
}

// Computed answers Blueprint::computed.
func (b *Blueprint) Computed(column, expression string) *ColumnDefinition {
	c := b.AddColumn("computed", column)
	c.expression = expression
	return c
}

// Vector answers Blueprint::vector.
func (b *Blueprint) Vector(column string, dimensions ...int) *ColumnDefinition {
	c := b.AddColumn("vector", column)
	if len(dimensions) > 0 && dimensions[0] != 0 {
		n := dimensions[0]
		c.dimensions = &n
	}
	return c
}

// Morphs answers Blueprint::morphs. The optional arguments are the index name
// and the column to place these after.
func (b *Blueprint) Morphs(name string, args ...string) {
	switch defaultMorphKeyType {
	case "uuid":
		b.UUIDMorphs(name, args...)
	case "ulid":
		b.ULIDMorphs(name, args...)
	default:
		b.NumericMorphs(name, args...)
	}
}

// NullableMorphs answers Blueprint::nullableMorphs.
func (b *Blueprint) NullableMorphs(name string, args ...string) {
	switch defaultMorphKeyType {
	case "uuid":
		b.NullableUUIDMorphs(name, args...)
	case "ulid":
		b.NullableULIDMorphs(name, args...)
	default:
		b.NullableNumericMorphs(name, args...)
	}
}

// NumericMorphs answers Blueprint::numericMorphs.
func (b *Blueprint) NumericMorphs(name string, args ...string) {
	b.morphs(name, args, false, func(column string) *ColumnDefinition {
		return b.UnsignedBigInteger(column)
	})
}

// NullableNumericMorphs answers Blueprint::nullableNumericMorphs.
func (b *Blueprint) NullableNumericMorphs(name string, args ...string) {
	b.morphs(name, args, true, func(column string) *ColumnDefinition {
		return b.UnsignedBigInteger(column)
	})
}

// UUIDMorphs answers Blueprint::uuidMorphs.
func (b *Blueprint) UUIDMorphs(name string, args ...string) {
	b.morphs(name, args, false, func(column string) *ColumnDefinition {
		return b.UUID(column)
	})
}

// NullableUUIDMorphs answers Blueprint::nullableUuidMorphs.
func (b *Blueprint) NullableUUIDMorphs(name string, args ...string) {
	b.morphs(name, args, true, func(column string) *ColumnDefinition {
		return b.UUID(column)
	})
}

// ULIDMorphs answers Blueprint::ulidMorphs.
func (b *Blueprint) ULIDMorphs(name string, args ...string) {
	b.morphs(name, args, false, func(column string) *ColumnDefinition {
		return b.ULID(column)
	})
}

// NullableULIDMorphs answers Blueprint::nullableUlidMorphs.
func (b *Blueprint) NullableULIDMorphs(name string, args ...string) {
	b.morphs(name, args, true, func(column string) *ColumnDefinition {
		return b.ULID(column)
	})
}

// morphs is the shared body of the six morphs methods: a type column, a key
// column and an index over both.
func (b *Blueprint) morphs(name string, args []string, nullable bool, key func(string) *ColumnDefinition) {
	indexName, after := arg(args, 0), arg(args, 1)

	typeColumn := b.String(name + "_type")
	if nullable {
		typeColumn.Nullable()
	}
	typeColumn.After(after)

	keyColumn := key(name + "_id")
	if nullable {
		keyColumn.Nullable()
	}
	if after != "" {
		keyColumn.After(name + "_type")
	}

	b.Index([]any{name + "_type", name + "_id"}, indexName)
}

// RememberToken answers Blueprint::rememberToken.
func (b *Blueprint) RememberToken() *ColumnDefinition {
	return b.String("remember_token", 100).Nullable()
}

// RawColumn answers Blueprint::rawColumn: a column whose definition is written
// out by hand.
func (b *Blueprint) RawColumn(column, definition string) *ColumnDefinition {
	c := b.AddColumn("raw", column)
	c.definition = definition
	return c
}

// Comment answers Blueprint::comment: a comment on the table.
func (b *Blueprint) Comment(comment string) *Command {
	command := b.addCommand("tableComment")
	command.Comment = comment
	return command
}

// indexCommand answers Blueprint::indexCommand.
func (b *Blueprint) indexCommand(typ string, columns any, index, algorithm, operatorClass string) *IndexDefinition {
	list := toColumnList(columns)

	if index == "" {
		index = b.createIndexName(typ, list)
	}

	command := b.addCommand(typ)
	command.Index = index
	command.Columns = list
	command.Algorithm = algorithm
	command.OperatorClass = operatorClass

	return &IndexDefinition{c: command}
}

// dropIndexCommand answers Blueprint::dropIndexCommand. An index given as a
// list of columns is named by the same convention that created it.
func (b *Blueprint) dropIndexCommand(command, typ string, index any) *Command {
	var columns []any
	name, isName := index.(string)

	if !isName {
		columns = toColumnList(index)
		name = b.createIndexName(typ, columns)
	}

	return b.indexCommand(command, columns, name, "", "").c
}

// createIndexName answers Blueprint::createIndexName.
func (b *Blueprint) createIndexName(typ string, columns []any) string {
	table := b.table

	if b.connection.GetConfig("prefix_indexes") != "" {
		if i := strings.LastIndex(b.table, "."); i >= 0 {
			table = b.table[:i] + "." + b.connection.GetTablePrefix() + b.table[i+1:]
		} else {
			table = b.connection.GetTablePrefix() + b.table
		}
	}

	parts := make([]string, 0, len(columns)+2)
	parts = append(parts, table)
	for _, column := range columns {
		parts = append(parts, stringify(column))
	}
	parts = append(parts, typ)

	index := strings.ToLower(strings.Join(parts, "_"))
	index = strings.ReplaceAll(index, "-", "_")
	return strings.ReplaceAll(index, ".", "_")
}

// AddColumn answers Blueprint::addColumn.
func (b *Blueprint) AddColumn(typ, name string) *ColumnDefinition {
	definition := &ColumnDefinition{typ: typ, name: name}
	b.addColumnDefinition(definition)
	return definition
}

// addColumnDefinition answers Blueprint::addColumnDefinition.
func (b *Blueprint) addColumnDefinition(definition *ColumnDefinition) *ColumnDefinition {
	b.columns = append(b.columns, definition)

	if !b.Creating() {
		b.commands = append(b.commands, &Command{Column: definition, isColumnDefinition: true})
	}

	if b.after != "" {
		definition.After(b.after)
		b.after = definition.name
	}

	return definition
}

// After answers Blueprint::after: every column the callback adds is placed
// after the given one, in the order they were added.
func (b *Blueprint) After(column string, callback func(*Blueprint)) {
	b.after = column
	callback(b)
	b.after = ""
}

// GetAfter answers $blueprint->after.
func (b *Blueprint) GetAfter() string { return b.after }

// RemoveColumn answers Blueprint::removeColumn.
func (b *Blueprint) RemoveColumn(name string) *Blueprint {
	columns := b.columns[:0]
	for _, column := range b.columns {
		if column.name != name {
			columns = append(columns, column)
		}
	}
	b.columns = columns

	commands := b.commands[:0]
	for _, command := range b.commands {
		if !command.isColumnDefinition || command.Column.name != name {
			commands = append(commands, command)
		}
	}
	b.commands = commands

	return b
}

// addCommand answers Blueprint::addCommand.
func (b *Blueprint) addCommand(name string) *Command {
	command := NewCommand(name)
	b.commands = append(b.commands, command)
	return command
}

// GetTable answers Blueprint::getTable.
func (b *Blueprint) GetTable() string { return b.table }

// GetPrefix answers Blueprint::getPrefix.
func (b *Blueprint) GetPrefix() string { return b.connection.GetTablePrefix() }

// GetColumns answers Blueprint::getColumns.
func (b *Blueprint) GetColumns() []*ColumnDefinition { return b.columns }

// GetCommands answers Blueprint::getCommands.
func (b *Blueprint) GetCommands() []*Command { return b.commands }

// GetState answers Blueprint::getState.
func (b *Blueprint) GetState() *BlueprintState { return b.state }

// GetAddedColumns answers Blueprint::getAddedColumns.
func (b *Blueprint) GetAddedColumns() []*ColumnDefinition {
	var added []*ColumnDefinition
	for _, column := range b.columns {
		if !column.change {
			added = append(added, column)
		}
	}
	return added
}

// GetChangedColumns answers Blueprint::getChangedColumns.
func (b *Blueprint) GetChangedColumns() []*ColumnDefinition {
	var changed []*ColumnDefinition
	for _, column := range b.columns {
		if column.change {
			changed = append(changed, column)
		}
	}
	return changed
}

// marker reads one of the fluent index attributes by the name the PHP loop
// uses.
func (c *ColumnDefinition) marker(index string) any {
	switch index {
	case "primary":
		return c.primary
	case "unique":
		return c.unique
	case "index":
		return c.index
	case "fulltext":
		return c.fullText
	case "spatialIndex":
		return c.spatialIndex
	case "vectorIndex":
		return c.vectorIndex
	}
	return nil
}

func (c *ColumnDefinition) setMarker(index string, value any) {
	switch index {
	case "primary":
		c.primary = value
	case "unique":
		c.unique = value
	case "index":
		c.index = value
	case "fulltext":
		c.fullText = value
	case "spatialIndex":
		c.spatialIndex = value
	case "vectorIndex":
		c.vectorIndex = value
	}
}

// toColumnList turns the string, string slice or expression slice a caller
// passes for columns into the list the command carries.
func toColumnList(columns any) []any {
	switch v := columns.(type) {
	case nil:
		return nil
	case string:
		return []any{v}
	case []string:
		return toAnyList(v)
	case []any:
		return v
	default:
		return []any{v}
	}
}

func toAnyList(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

func defaultTo(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func boolAt(values []bool, n int) bool {
	if n < len(values) {
		return values[n]
	}
	return false
}

// stringify renders a column entry, which is a string or an expression.
func stringify(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case query.Expression:
		return v.String()
	case *query.Expression:
		return v.String()
	default:
		return fmt.Sprint(value)
	}
}
