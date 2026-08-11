package schema

// ColumnDefinition answers Illuminate\Database\Schema\ColumnDefinition.
//
// It is what every column method on the Blueprint returns, and it is what the
// migration reads like: String("email", 255).Nullable().Default("").Comment("").
//
// PHP declares it as an empty Fluent subclass and lists its methods in a
// docblock, so the attribute and the setter share a name -- $column->nullable
// is the value, nullable() is the setter. Go has one namespace for both, so
// every attribute is an unexported field with a Get prefixed getter, the same
// rule query.Builder follows for GetFrom and GetLimit.
//
// The index markers (Index, Unique, Primary, FullText, SpatialIndex,
// VectorIndex) are any because the PHP takes bool|string|null and each of the
// three means something different: nothing means "index it, name it by
// convention", a string names it, and false on a changed column drops the
// index it would otherwise have had.
type ColumnDefinition struct {
	name string
	typ  string

	length             *int
	fixed              bool
	precision          *int
	total              int
	places             int
	allowed            []string
	subtype            string
	srid               int
	dimensions         *int
	definition         string
	expression         string
	fullTypeDefinition string

	autoIncrement      bool
	unsigned           bool
	nullable           *bool
	def                any
	onUpdate           any
	useCurrent         bool
	useCurrentOnUpdate bool

	change   bool
	renameTo string
	after    *string
	first    *bool

	invisible *bool
	comment   *string
	charset   string
	collation string

	virtualAs     any
	virtualAsSet  bool
	virtualAsJSON string
	storedAs      any
	storedAsSet   bool
	storedAsJSON  string
	generatedAs   any
	always        bool

	startingValue *int
	from          *int

	instant bool
	lock    string

	index        any
	unique       any
	primary      any
	fullText     any
	spatialIndex any
	vectorIndex  any

	table                 string
	referencesModelColumn string
}

// GetName answers $column->name.
func (c *ColumnDefinition) GetName() string { return c.name }

// Type answers ColumnDefinition::type: it overrides the column's type.
func (c *ColumnDefinition) Type(typ string) *ColumnDefinition { c.typ = typ; return c }

// GetType answers $column->type.
func (c *ColumnDefinition) GetType() string { return c.typ }

// GetLength answers $column->length.
func (c *ColumnDefinition) GetLength() *int { return c.length }

// GetFixed answers $column->fixed, the binary column's fixed width flag.
func (c *ColumnDefinition) GetFixed() bool { return c.fixed }

// GetPrecision answers $column->precision.
func (c *ColumnDefinition) GetPrecision() *int { return c.precision }

// GetTotal answers $column->total, the decimal column's total digits.
func (c *ColumnDefinition) GetTotal() int { return c.total }

// GetPlaces answers $column->places, the decimal column's decimal places.
func (c *ColumnDefinition) GetPlaces() int { return c.places }

// GetAllowed answers $column->allowed, the values an enum or set accepts.
func (c *ColumnDefinition) GetAllowed() []string { return c.allowed }

// GetSubtype answers $column->subtype of a geometry or geography column.
func (c *ColumnDefinition) GetSubtype() string { return c.subtype }

// GetSRID answers $column->srid. The PHP spells it srid; an initialism is
// upper case here.
func (c *ColumnDefinition) GetSRID() int { return c.srid }

// GetDimensions answers $column->dimensions of a vector column.
func (c *ColumnDefinition) GetDimensions() *int { return c.dimensions }

// GetDefinition answers $column->definition of a raw column.
func (c *ColumnDefinition) GetDefinition() string { return c.definition }

// GetExpression answers $column->expression of a computed column.
func (c *ColumnDefinition) GetExpression() string { return c.expression }

// GetFullTypeDefinition answers $column->full_type_definition, which
// BlueprintState fills from the server so a SQLite table rebuild can repeat a
// column it did not write.
func (c *ColumnDefinition) GetFullTypeDefinition() string { return c.fullTypeDefinition }

// AutoIncrement answers ColumnDefinition::autoIncrement.
func (c *ColumnDefinition) AutoIncrement() *ColumnDefinition { c.autoIncrement = true; return c }

// GetAutoIncrement answers $column->autoIncrement.
func (c *ColumnDefinition) GetAutoIncrement() bool { return c.autoIncrement }

// Unsigned answers ColumnDefinition::unsigned.
func (c *ColumnDefinition) Unsigned() *ColumnDefinition { c.unsigned = true; return c }

// GetUnsigned answers $column->unsigned.
func (c *ColumnDefinition) GetUnsigned() bool { return c.unsigned }

// Nullable answers ColumnDefinition::nullable. The PHP default argument is
// true, so Nullable() allows null and Nullable(false) forbids it.
func (c *ColumnDefinition) Nullable(value ...bool) *ColumnDefinition {
	v := true
	if len(value) > 0 {
		v = value[0]
	}
	c.nullable = &v
	return c
}

// GetNullable answers $column->nullable. It is a pointer because the PHP
// distinguishes an unset nullable from a false one: Postgres emits nothing for
// the first and "not null" for the second.
func (c *ColumnDefinition) GetNullable() *bool { return c.nullable }

// Default answers ColumnDefinition::default. A query Expression passes through
// to the SQL untouched; anything else is quoted.
func (c *ColumnDefinition) Default(value any) *ColumnDefinition { c.def = value; return c }

// GetDefault answers $column->default.
func (c *ColumnDefinition) GetDefault() any { return c.def }

// OnUpdate answers the MySQL "on update" column modifier, which typeTimestamp
// sets from UseCurrentOnUpdate.
func (c *ColumnDefinition) OnUpdate(value any) *ColumnDefinition { c.onUpdate = value; return c }

// GetOnUpdate answers $column->onUpdate.
func (c *ColumnDefinition) GetOnUpdate() any { return c.onUpdate }

// UseCurrent answers ColumnDefinition::useCurrent.
func (c *ColumnDefinition) UseCurrent() *ColumnDefinition { c.useCurrent = true; return c }

// GetUseCurrent answers $column->useCurrent.
func (c *ColumnDefinition) GetUseCurrent() bool { return c.useCurrent }

// UseCurrentOnUpdate answers ColumnDefinition::useCurrentOnUpdate.
func (c *ColumnDefinition) UseCurrentOnUpdate() *ColumnDefinition {
	c.useCurrentOnUpdate = true
	return c
}

// GetUseCurrentOnUpdate answers $column->useCurrentOnUpdate.
func (c *ColumnDefinition) GetUseCurrentOnUpdate() bool { return c.useCurrentOnUpdate }

// Change answers ColumnDefinition::change: the column already exists and this
// definition replaces it.
func (c *ColumnDefinition) Change() *ColumnDefinition { c.change = true; return c }

// GetChange answers $column->change.
func (c *ColumnDefinition) GetChange() bool { return c.change }

// GetRenameTo answers $column->renameTo, which a change command carries when
// the column is renamed in the same statement.
func (c *ColumnDefinition) GetRenameTo() string { return c.renameTo }

// After answers ColumnDefinition::after. An empty column is the PHP null and
// clears the modifier, which is what Blueprint's morphs family relies on.
func (c *ColumnDefinition) After(column string) *ColumnDefinition {
	if column == "" {
		c.after = nil
		return c
	}
	c.after = &column
	return c
}

// GetAfter answers $column->after.
func (c *ColumnDefinition) GetAfter() *string { return c.after }

// First answers ColumnDefinition::first.
func (c *ColumnDefinition) First(value ...bool) *ColumnDefinition {
	v := true
	if len(value) > 0 {
		v = value[0]
	}
	c.first = &v
	return c
}

// GetFirst answers $column->first.
func (c *ColumnDefinition) GetFirst() *bool { return c.first }

// Invisible answers ColumnDefinition::invisible: the column is left out of
// "select *" on MySQL.
func (c *ColumnDefinition) Invisible(value ...bool) *ColumnDefinition {
	v := true
	if len(value) > 0 {
		v = value[0]
	}
	c.invisible = &v
	return c
}

// GetInvisible answers $column->invisible.
func (c *ColumnDefinition) GetInvisible() *bool { return c.invisible }

// Comment answers ColumnDefinition::comment.
func (c *ColumnDefinition) Comment(comment string) *ColumnDefinition {
	c.comment = &comment
	return c
}

// GetComment answers $column->comment. It is a pointer because Postgres writes
// "comment on column ... is NULL" to clear one, which is a different statement
// from writing no comment at all.
func (c *ColumnDefinition) GetComment() *string { return c.comment }

// Charset answers ColumnDefinition::charset.
func (c *ColumnDefinition) Charset(charset string) *ColumnDefinition { c.charset = charset; return c }

// GetCharset answers $column->charset.
func (c *ColumnDefinition) GetCharset() string { return c.charset }

// Collation answers ColumnDefinition::collation.
func (c *ColumnDefinition) Collation(collation string) *ColumnDefinition {
	c.collation = collation
	return c
}

// GetCollation answers $column->collation.
func (c *ColumnDefinition) GetCollation() string { return c.collation }

// VirtualAs answers ColumnDefinition::virtualAs: a generated column computed on
// read. The expression may be a string or a query Expression.
func (c *ColumnDefinition) VirtualAs(expression any) *ColumnDefinition {
	c.virtualAs = expression
	c.virtualAsSet = true
	return c
}

// HasVirtualAs answers array_key_exists('virtualAs', $column->getAttributes()).
//
// PHP can tell an attribute that was never mentioned from one set to null, and
// on a changed column the difference decides between leaving the generation
// expression alone and dropping it. A Go nil field cannot say which, so the
// setter records that it ran.
func (c *ColumnDefinition) HasVirtualAs() bool { return c.virtualAsSet }

// GetVirtualAs answers $column->virtualAs.
func (c *ColumnDefinition) GetVirtualAs() any { return c.virtualAs }

// GetVirtualAsJSON answers $column->virtualAsJson, the JSON selector form the
// MySQL and SQLite grammars unwrap before generating the column.
func (c *ColumnDefinition) GetVirtualAsJSON() string { return c.virtualAsJSON }

// StoredAs answers ColumnDefinition::storedAs: a generated column computed on
// write and stored.
func (c *ColumnDefinition) StoredAs(expression any) *ColumnDefinition {
	c.storedAs = expression
	c.storedAsSet = true
	return c
}

// HasStoredAs answers array_key_exists('storedAs', $column->getAttributes()).
// See HasVirtualAs for why the setter has to record that it ran.
func (c *ColumnDefinition) HasStoredAs() bool { return c.storedAsSet }

// GetStoredAs answers $column->storedAs.
func (c *ColumnDefinition) GetStoredAs() any { return c.storedAs }

// GetStoredAsJSON answers $column->storedAsJson.
func (c *ColumnDefinition) GetStoredAsJSON() string { return c.storedAsJSON }

// GeneratedAs answers ColumnDefinition::generatedAs: a Postgres identity
// column. With no argument it is the bare "generated by default as identity";
// with one it carries the sequence options.
func (c *ColumnDefinition) GeneratedAs(expression ...any) *ColumnDefinition {
	if len(expression) == 0 {
		c.generatedAs = true
		return c
	}
	c.generatedAs = expression[0]
	return c
}

// GetGeneratedAs answers $column->generatedAs.
func (c *ColumnDefinition) GetGeneratedAs() any { return c.generatedAs }

// Always answers ColumnDefinition::always: the identity column is "generated
// always" rather than "generated by default".
func (c *ColumnDefinition) Always(value ...bool) *ColumnDefinition {
	v := true
	if len(value) > 0 {
		v = value[0]
	}
	c.always = v
	return c
}

// GetAlways answers $column->always.
func (c *ColumnDefinition) GetAlways() bool { return c.always }

// StartingValue answers ColumnDefinition::startingValue.
func (c *ColumnDefinition) StartingValue(value int) *ColumnDefinition {
	c.startingValue = &value
	return c
}

// GetStartingValue answers $column->startingValue.
func (c *ColumnDefinition) GetStartingValue() *int { return c.startingValue }

// From answers ColumnDefinition::from, the other spelling of StartingValue.
func (c *ColumnDefinition) From(startingValue int) *ColumnDefinition {
	c.from = &startingValue
	return c
}

// GetFrom answers $column->from.
func (c *ColumnDefinition) GetFrom() *int { return c.from }

// Instant answers ColumnDefinition::instant: MySQL should use algorithm=instant
// for the operation.
func (c *ColumnDefinition) Instant() *ColumnDefinition { c.instant = true; return c }

// GetInstant answers $column->instant.
func (c *ColumnDefinition) GetInstant() bool { return c.instant }

// Lock answers ColumnDefinition::lock: the MySQL DDL lock mode, one of none,
// shared, default or exclusive.
func (c *ColumnDefinition) Lock(mode string) *ColumnDefinition { c.lock = mode; return c }

// GetLock answers $column->lock.
func (c *ColumnDefinition) GetLock() string { return c.lock }

// Index answers ColumnDefinition::index. With no argument the index is named by
// convention; with a string it is named; with false on a changed column the
// conventional index is dropped.
func (c *ColumnDefinition) Index(name ...any) *ColumnDefinition {
	c.index = indexMarker(name)
	return c
}

// GetIndex answers $column->index.
func (c *ColumnDefinition) GetIndex() any { return c.index }

// Unique answers ColumnDefinition::unique, on the same three argument rule as
// Index.
func (c *ColumnDefinition) Unique(name ...any) *ColumnDefinition {
	c.unique = indexMarker(name)
	return c
}

// GetUnique answers $column->unique.
func (c *ColumnDefinition) GetUnique() any { return c.unique }

// Primary answers ColumnDefinition::primary, on the same three argument rule as
// Index.
func (c *ColumnDefinition) Primary(name ...any) *ColumnDefinition {
	c.primary = indexMarker(name)
	return c
}

// GetPrimary answers $column->primary.
func (c *ColumnDefinition) GetPrimary() any { return c.primary }

// FullText answers ColumnDefinition::fulltext, on the same three argument rule
// as Index.
func (c *ColumnDefinition) FullText(name ...any) *ColumnDefinition {
	c.fullText = indexMarker(name)
	return c
}

// GetFullText answers $column->fulltext.
func (c *ColumnDefinition) GetFullText() any { return c.fullText }

// SpatialIndex answers ColumnDefinition::spatialIndex, on the same three
// argument rule as Index.
func (c *ColumnDefinition) SpatialIndex(name ...any) *ColumnDefinition {
	c.spatialIndex = indexMarker(name)
	return c
}

// GetSpatialIndex answers $column->spatialIndex.
func (c *ColumnDefinition) GetSpatialIndex() any { return c.spatialIndex }

// VectorIndex answers ColumnDefinition::vectorIndex, on the same three argument
// rule as Index.
func (c *ColumnDefinition) VectorIndex(name ...any) *ColumnDefinition {
	c.vectorIndex = indexMarker(name)
	return c
}

// GetVectorIndex answers $column->vectorIndex.
func (c *ColumnDefinition) GetVectorIndex() any { return c.vectorIndex }

// Table answers ColumnDefinition::table, which foreignIdFor sets so that
// Constrained knows the table without guessing it from the column name.
func (c *ColumnDefinition) Table(table string) *ColumnDefinition { c.table = table; return c }

// GetTable answers $column->table.
func (c *ColumnDefinition) GetTable() string { return c.table }

// ReferencesModelColumn answers ColumnDefinition::referencesModelColumn, the
// key name Constrained references when the model does not key on id.
func (c *ColumnDefinition) ReferencesModelColumn(column string) *ColumnDefinition {
	c.referencesModelColumn = column
	return c
}

// GetReferencesModelColumn answers $column->referencesModelColumn.
func (c *ColumnDefinition) GetReferencesModelColumn() string { return c.referencesModelColumn }

// indexMarker turns the variadic argument of the fluent index setters into the
// bool|string|null the PHP carries. More than one argument would be a fatal
// call error in PHP; here the first wins, because a silent panic in a migration
// helps nobody.
func indexMarker(name []any) any {
	if len(name) == 0 {
		return true
	}
	return name[0]
}
