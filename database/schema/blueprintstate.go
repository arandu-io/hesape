package schema

import (
	"context"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database/query"
)

// BlueprintState answers Illuminate\Database\Schema\BlueprintState.
//
// SQLite cannot alter most of a table, so the SQLite grammar rebuilds it: it
// creates a table with the shape the blueprint asks for, copies the rows over,
// drops the original and renames. To do that it has to know the shape the table
// already has, which is what this reads from the server and then keeps up to
// date as the blueprint's commands are compiled.
type BlueprintState struct {
	blueprint  *Blueprint
	connection Connection

	columns     []*ColumnDefinition
	primaryKey  *IndexDefinition
	indexes     []*IndexDefinition
	foreignKeys []*ForeignKeyDefinition
}

// NewBlueprintState answers BlueprintState::__construct.
//
// It carries a context and an auth.Grant where the PHP carries neither, because
// the constructor reads three catalogues off the server. See Blueprint.ToSQL.
func NewBlueprintState(ctx context.Context, g auth.Grant, blueprint *Blueprint, connection Connection) (*BlueprintState, error) {
	s := &BlueprintState{blueprint: blueprint, connection: connection}
	builder := NewBuilder(connection)
	table := blueprint.GetTable()

	columns, err := builder.GetColumns(ctx, g, table)
	if err != nil {
		return nil, err
	}
	for _, column := range columns {
		definition := &ColumnDefinition{
			name:               column.Name,
			typ:                column.TypeName,
			fullTypeDefinition: column.Type,
			autoIncrement:      column.AutoIncrement,
			collation:          column.Collation,
		}
		nullable := column.Nullable
		definition.nullable = &nullable
		if column.Comment != "" {
			comment := column.Comment
			definition.comment = &comment
		}
		if column.Default != nil {
			definition.def = query.Raw(wrapParens(stringify(column.Default)))
		}
		if column.Generation != nil {
			switch column.Generation.Type {
			case "virtual":
				definition.virtualAs = column.Generation.Expression
			case "stored":
				definition.storedAs = column.Generation.Expression
			}
		}
		s.columns = append(s.columns, definition)
	}

	indexes, err := builder.GetIndexes(ctx, g, table)
	if err != nil {
		return nil, err
	}
	for _, index := range indexes {
		name := "index"
		switch {
		case index.Primary:
			name = "primary"
		case index.Unique:
			name = "unique"
		}
		command := NewCommand(name)
		command.Index = index.Name
		command.Columns = toAnyList(index.Columns)

		if index.Primary {
			if s.primaryKey == nil {
				s.primaryKey = &IndexDefinition{c: command}
			}
			continue
		}
		s.indexes = append(s.indexes, &IndexDefinition{c: command})
	}

	foreignKeys, err := builder.GetForeignKeys(ctx, g, table)
	if err != nil {
		return nil, err
	}
	for _, foreignKey := range foreignKeys {
		command := NewCommand("foreign")
		command.Columns = toAnyList(foreignKey.Columns)
		command.On = query.Raw(foreignKey.ForeignTable)
		command.References = toAnyList(foreignKey.ForeignColumns)
		command.OnUpdate = foreignKey.OnUpdate
		command.OnDelete = foreignKey.OnDelete
		s.foreignKeys = append(s.foreignKeys, &ForeignKeyDefinition{c: command})
	}

	return s, nil
}

// GetPrimaryKey answers BlueprintState::getPrimaryKey.
func (s *BlueprintState) GetPrimaryKey() *IndexDefinition { return s.primaryKey }

// GetColumns answers BlueprintState::getColumns.
func (s *BlueprintState) GetColumns() []*ColumnDefinition { return s.columns }

// GetIndexes answers BlueprintState::getIndexes.
func (s *BlueprintState) GetIndexes() []*IndexDefinition { return s.indexes }

// GetForeignKeys answers BlueprintState::getForeignKeys.
func (s *BlueprintState) GetForeignKeys() []*ForeignKeyDefinition { return s.foreignKeys }

// Update answers BlueprintState::update: it applies one compiled command to the
// remembered shape, so that the rebuild statement sees the table as it will be
// rather than as it was.
func (s *BlueprintState) Update(command *Command) {
	switch command.Name {
	case "alter":
		// Already handled...

	case "add":
		s.columns = append(s.columns, command.Column)

	case "change":
		for i, column := range s.columns {
			if column.name == command.Column.name {
				s.columns[i] = command.Column
				break
			}
		}

	case "renameColumn":
		for _, column := range s.columns {
			if column.name == command.From {
				column.name = command.To
				break
			}
		}
		if s.primaryKey != nil {
			s.primaryKey.c.Columns = replaceColumns(s.primaryKey.c.Columns, command.From, command.To)
		}
		for _, index := range s.indexes {
			index.c.Columns = replaceColumns(index.c.Columns, command.From, command.To)
		}
		for _, foreignKey := range s.foreignKeys {
			foreignKey.c.Columns = replaceColumns(foreignKey.c.Columns, command.From, command.To)
		}

	case "dropColumn":
		dropped := make(map[string]bool, len(command.Columns))
		for _, column := range command.Columns {
			dropped[stringify(column)] = true
		}
		kept := s.columns[:0]
		for _, column := range s.columns {
			if !dropped[column.name] {
				kept = append(kept, column)
			}
		}
		s.columns = kept

	case "primary":
		s.primaryKey = &IndexDefinition{c: command}

	case "unique", "index":
		s.indexes = append(s.indexes, &IndexDefinition{c: command})

	case "renameIndex":
		for _, index := range s.indexes {
			if index.c.Index == command.From {
				index.c.Index = command.To
				break
			}
		}

	case "foreign":
		s.foreignKeys = append(s.foreignKeys, &ForeignKeyDefinition{c: command})

	case "dropPrimary":
		s.primaryKey = nil

	case "dropIndex", "dropUnique":
		kept := s.indexes[:0]
		for _, index := range s.indexes {
			if index.c.Index != command.Index {
				kept = append(kept, index)
			}
		}
		s.indexes = kept

	case "dropForeign":
		kept := s.foreignKeys[:0]
		for _, foreignKey := range s.foreignKeys {
			if !sameColumns(foreignKey.c.Columns, command.Columns) {
				kept = append(kept, foreignKey)
			}
		}
		s.foreignKeys = kept
	}
}

// replaceColumns answers the PHP's str_replace over an index's column list.
func replaceColumns(columns []any, from, to string) []any {
	out := make([]any, len(columns))
	for i, column := range columns {
		if name, ok := column.(string); ok {
			out[i] = strings.ReplaceAll(name, from, to)
			continue
		}
		out[i] = column
	}
	return out
}

func sameColumns(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if stringify(a[i]) != stringify(b[i]) {
			return false
		}
	}
	return true
}

// wrapParens answers Str::wrap($default, '(', ')').
func wrapParens(value string) string { return "(" + value + ")" }
