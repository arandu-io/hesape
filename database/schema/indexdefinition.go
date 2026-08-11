package schema

// IndexDefinition answers Illuminate\Database\Schema\IndexDefinition.
//
// In PHP it is a Fluent subclass and the index command on the blueprint *is*
// the definition: Blueprint::indexCommand returns the very Fluent it appended,
// so chaining onto it changes the command the grammar will compile. Here it is
// a typed handle over that same *Command, which keeps the one object PHP has.
type IndexDefinition struct{ c *Command }

// GetCommand returns the blueprint command this definition is a handle on.
//
// PHP needs no such method because the definition is the command. The grammar
// compiles commands, so the handle has to be able to hand its own over.
func (d *IndexDefinition) GetCommand() *Command { return d.c }

// Algorithm answers IndexDefinition::algorithm: btree, hash, gin, gist.
func (d *IndexDefinition) Algorithm(algorithm string) *IndexDefinition {
	d.c.Algorithm = algorithm
	return d
}

// Deferrable answers IndexDefinition::deferrable.
func (d *IndexDefinition) Deferrable(value ...bool) *IndexDefinition {
	d.c.Deferrable = boolArg(value)
	return d
}

// InitiallyImmediate answers IndexDefinition::initiallyImmediate.
func (d *IndexDefinition) InitiallyImmediate(value ...bool) *IndexDefinition {
	d.c.InitiallyImmediate = boolArg(value)
	return d
}

// Language answers IndexDefinition::language, the Postgres text search
// configuration a full text index is built with.
func (d *IndexDefinition) Language(language string) *IndexDefinition {
	d.c.Language = language
	return d
}

// Lock answers IndexDefinition::lock, the MySQL DDL lock mode.
func (d *IndexDefinition) Lock(mode string) *IndexDefinition {
	d.c.Lock = mode
	return d
}

// NullsNotDistinct answers IndexDefinition::nullsNotDistinct: two null values
// collide in the unique index rather than both being allowed.
func (d *IndexDefinition) NullsNotDistinct(value ...bool) *IndexDefinition {
	d.c.NullsNotDistinct = boolArg(value)
	return d
}

// Online answers IndexDefinition::online: build the index without locking the
// table.
func (d *IndexDefinition) Online(value ...bool) *IndexDefinition {
	v := true
	if len(value) > 0 {
		v = value[0]
	}
	d.c.Online = v
	return d
}

// boolArg turns the variadic bool of a PHP `bool $value = true` parameter into
// the pointer the Command carries, which is how "not set" stays different from
// false.
func boolArg(value []bool) *bool {
	v := true
	if len(value) > 0 {
		v = value[0]
	}
	return &v
}
