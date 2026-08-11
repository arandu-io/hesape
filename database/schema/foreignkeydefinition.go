package schema

// ForeignKeyDefinition answers Illuminate\Database\Schema\ForeignKeyDefinition.
//
// Like IndexDefinition it is a typed handle over the blueprint command, because
// in PHP the definition is the command: Blueprint::foreign replaces the Fluent
// it just appended with this subclass and hands it back.
type ForeignKeyDefinition struct{ c *Command }

// GetCommand returns the blueprint command this definition is a handle on.
func (d *ForeignKeyDefinition) GetCommand() *Command { return d.c }

// References answers ForeignKeyDefinition::references: the column or columns on
// the referenced table.
func (d *ForeignKeyDefinition) References(columns ...string) *ForeignKeyDefinition {
	d.c.References = make([]any, len(columns))
	for i, column := range columns {
		d.c.References[i] = column
	}
	return d
}

// On answers ForeignKeyDefinition::on: the referenced table.
func (d *ForeignKeyDefinition) On(table string) *ForeignKeyDefinition {
	d.c.On = table
	return d
}

// OnDelete answers ForeignKeyDefinition::onDelete. The action is cascade,
// restrict, set null or no action.
func (d *ForeignKeyDefinition) OnDelete(action string) *ForeignKeyDefinition {
	d.c.OnDelete = action
	return d
}

// OnUpdate answers ForeignKeyDefinition::onUpdate, on the same four actions as
// OnDelete.
func (d *ForeignKeyDefinition) OnUpdate(action string) *ForeignKeyDefinition {
	d.c.OnUpdate = action
	return d
}

// CascadeOnDelete answers ForeignKeyDefinition::cascadeOnDelete.
func (d *ForeignKeyDefinition) CascadeOnDelete() *ForeignKeyDefinition {
	return d.OnDelete("cascade")
}

// RestrictOnDelete answers ForeignKeyDefinition::restrictOnDelete.
func (d *ForeignKeyDefinition) RestrictOnDelete() *ForeignKeyDefinition {
	return d.OnDelete("restrict")
}

// NullOnDelete answers ForeignKeyDefinition::nullOnDelete.
func (d *ForeignKeyDefinition) NullOnDelete() *ForeignKeyDefinition {
	return d.OnDelete("set null")
}

// NoActionOnDelete answers ForeignKeyDefinition::noActionOnDelete.
func (d *ForeignKeyDefinition) NoActionOnDelete() *ForeignKeyDefinition {
	return d.OnDelete("no action")
}

// CascadeOnUpdate answers ForeignKeyDefinition::cascadeOnUpdate.
func (d *ForeignKeyDefinition) CascadeOnUpdate() *ForeignKeyDefinition {
	return d.OnUpdate("cascade")
}

// RestrictOnUpdate answers ForeignKeyDefinition::restrictOnUpdate.
func (d *ForeignKeyDefinition) RestrictOnUpdate() *ForeignKeyDefinition {
	return d.OnUpdate("restrict")
}

// NullOnUpdate answers ForeignKeyDefinition::nullOnUpdate.
func (d *ForeignKeyDefinition) NullOnUpdate() *ForeignKeyDefinition {
	return d.OnUpdate("set null")
}

// NoActionOnUpdate answers ForeignKeyDefinition::noActionOnUpdate.
func (d *ForeignKeyDefinition) NoActionOnUpdate() *ForeignKeyDefinition {
	return d.OnUpdate("no action")
}

// Deferrable answers ForeignKeyDefinition::deferrable: the constraint may be
// checked at the end of the transaction rather than at each statement.
func (d *ForeignKeyDefinition) Deferrable(value ...bool) *ForeignKeyDefinition {
	d.c.Deferrable = boolArg(value)
	return d
}

// InitiallyImmediate answers ForeignKeyDefinition::initiallyImmediate: when the
// constraint is deferrable, whether it starts out checked per statement.
func (d *ForeignKeyDefinition) InitiallyImmediate(value ...bool) *ForeignKeyDefinition {
	d.c.InitiallyImmediate = boolArg(value)
	return d
}

// NotValid answers the Postgres "not valid" constraint modifier: existing rows
// are not checked when the constraint is added.
func (d *ForeignKeyDefinition) NotValid(value ...bool) *ForeignKeyDefinition {
	d.c.NotValid = boolArg(value)
	return d
}

// Lock answers ForeignKeyDefinition::lock, the MySQL DDL lock mode.
func (d *ForeignKeyDefinition) Lock(mode string) *ForeignKeyDefinition {
	d.c.Lock = mode
	return d
}
