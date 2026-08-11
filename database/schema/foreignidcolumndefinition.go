package schema

import "strings"

// ForeignIDColumnDefinition answers
// Illuminate\Database\Schema\ForeignIdColumnDefinition.
//
// It is the column ForeignID, ForeignUUID and ForeignULID return: an ordinary
// ColumnDefinition that also knows its blueprint, so Constrained can add the
// foreign key command without the caller naming the column twice.
type ForeignIDColumnDefinition struct {
	*ColumnDefinition
	blueprint *Blueprint
}

// Constrained answers ForeignIdColumnDefinition::constrained.
//
// The optional arguments are, in order, the referenced table, the referenced
// column and the index name; an omitted or empty one takes the PHP default.
// With none of them, the table is guessed from the column name the way PHP
// does: user_id references id on users, dropping the "_id" suffix and
// pluralising what is left.
func (d *ForeignIDColumnDefinition) Constrained(args ...string) *ForeignKeyDefinition {
	table, column, indexName := arg(args, 0), arg(args, 1), arg(args, 2)

	if table == "" {
		table = d.table
	}
	if column == "" {
		column = d.referencesModelColumn
	}
	if column == "" {
		column = "id"
	}

	key := d.References(column, indexName)

	if table == "" {
		table = plural(beforeLast(d.name, "_"+column))
	}

	return key.On(table)
}

// References answers ForeignIdColumnDefinition::references: it adds the foreign
// key command for this column and points it at a column of another table.
func (d *ForeignIDColumnDefinition) References(column string, indexName ...string) *ForeignKeyDefinition {
	name := ""
	if len(indexName) > 0 {
		name = indexName[0]
	}
	return d.blueprint.Foreign(d.name, name).References(column)
}

// arg reads the nth optional argument of a variadic that stands in for a PHP
// default argument list.
func arg(args []string, n int) string {
	if n < len(args) {
		return args[n]
	}
	return ""
}

// beforeLast answers Illuminate\Support\Stringable::beforeLast.
func beforeLast(subject, search string) string {
	if search == "" {
		return subject
	}
	if i := strings.LastIndex(subject, search); i >= 0 {
		return subject[:i]
	}
	return subject
}

// plural answers Str::plural for the shapes a table name takes.
//
// Laravel routes this through Doctrine's inflector, which carries an
// irregular-word table this component has no use for: it is applied to the
// stem of a foreign key column, so the input is a singular table name a
// developer chose. The rules below are the regular English ones -- s, x, z, ch
// and sh take -es, a consonant before y turns into -ies, everything else takes
// -s -- and a name they get wrong is a name Constrained's table argument is
// for.
func plural(word string) string {
	if word == "" {
		return word
	}
	lower := strings.ToLower(word)
	switch {
	case strings.HasSuffix(lower, "s"),
		strings.HasSuffix(lower, "x"),
		strings.HasSuffix(lower, "z"),
		strings.HasSuffix(lower, "ch"),
		strings.HasSuffix(lower, "sh"):
		return word + "es"
	case strings.HasSuffix(lower, "y") && len(word) > 1 && !isVowel(lower[len(lower)-2]):
		return word[:len(word)-1] + "ies"
	default:
		return word + "s"
	}
}

func isVowel(b byte) bool {
	switch b {
	case 'a', 'e', 'i', 'o', 'u':
		return true
	}
	return false
}
