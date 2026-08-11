package concerns

import (
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/database/query"
)

// OfManyColumn is one entry of the column => aggregate map ofMany takes.
//
// PHP passes an array whose keys are columns and whose values are MIN or MAX,
// and relies on the array keeping the order they were written in. A Go map does
// not, and the order is the tie-break order -- the first column decides, the
// next one breaks the ties the first one left -- so it is a slice.
type OfManyColumn struct {
	// Column is the column the aggregate is taken over.
	Column string

	// Aggregate is MIN or MAX. Nothing else is accepted, which is the PHP's own
	// InvalidArgumentException.
	Aggregate string
}

// CanBeOneOfMany answers the trait of the same name: the "latest post of each
// user" relation, which is a has-one over a has-many.
//
// It is a single row per parent chosen by an aggregate, and it is done with a
// join against a grouped subquery rather than with a limit, because a limit
// would take one row from the whole result set rather than one row per parent.
// That is the difference between a relation that eager loads and one that
// cannot.
type CanBeOneOfMany struct {
	// OneOfManyQuery answers $this->query: the relation's builder.
	OneOfManyQuery func() Builder

	// OneOfManyRelated answers $this->query->getModel().
	OneOfManyRelated func() Model

	// OneOfManyAddConstraints answers $this->addConstraints(), which ofMany
	// re-runs after the join is in place.
	OneOfManyAddConstraints func()

	// AddOneOfManySubQueryConstraints answers the trait's abstract method of
	// the same name.
	AddOneOfManySubQueryConstraints func(query Builder, column, aggregate string)

	// GetOneOfManySubQuerySelectColumns answers the trait's abstract method of
	// the same name.
	GetOneOfManySubQuerySelectColumns func() []any

	// AddOneOfManyJoinSubQueryConstraints answers the trait's abstract method
	// of the same name.
	AddOneOfManyJoinSubQueryConstraints func(join *query.JoinClause)

	isOneOfMany       bool
	relationName      string
	oneOfManySubQuery Builder
}

// OfMany answers CanBeOneOfMany::ofMany.
//
// The relation name is required, where the PHP guesses it from a
// debug_backtrace three frames up. There is no equivalent -- and a name read
// off a stack frame is a join alias that changes when somebody renames a
// method, which is the sort of coupling that only shows up in production.
func (c *CanBeOneOfMany) OfMany(columns []OfManyColumn, relation string) error {
	if relation == "" {
		return fmt.Errorf("relations: ofMany needs the relation name, which becomes the join alias")
	}
	if len(columns) == 0 {
		columns = []OfManyColumn{{Column: "id", Aggregate: "MAX"}}
	}

	c.isOneOfMany = true
	c.relationName = c.getDefaultOneOfManyJoinAlias(relation)

	keyName := c.OneOfManyRelated().GetKeyName()
	if !hasColumn(columns, keyName) {
		columns = append(columns, OfManyColumn{Column: keyName, Aggregate: "MAX"})
	}

	var previousSubQuery Builder
	var previousColumns []string

	for index, entry := range columns {
		if aggregate := strings.ToLower(entry.Aggregate); aggregate != "min" && aggregate != "max" {
			return fmt.Errorf("relations: invalid aggregate [%s] used within ofMany relation. Available aggregates: MIN, MAX", entry.Aggregate)
		}

		on := append([]string{entry.Column}, previousColumns...)

		subQuery := c.newOneOfManySubQuery(c.GetOneOfManySubQuerySelectColumns(), on, entry.Aggregate)

		if previousSubQuery != nil {
			c.addOneOfManyJoinSubQuery(subQuery, previousSubQuery, previousColumns)
		} else {
			c.oneOfManySubQuery = subQuery
		}

		if index == len(columns)-1 {
			c.addOneOfManyJoinSubQuery(c.OneOfManyQuery(), subQuery, on)
		}

		previousSubQuery, previousColumns = subQuery, on
	}

	if c.OneOfManyAddConstraints != nil {
		c.OneOfManyAddConstraints()
	}

	if selected := c.OneOfManyQuery().GetQuery().GetColumns(); len(selected) == 0 || isStar(selected) {
		c.OneOfManyQuery().Select(c.OneOfManyRelated().QualifyColumn("*"))
	}

	return nil
}

// LatestOfMany answers CanBeOneOfMany::latestOfMany.
func (c *CanBeOneOfMany) LatestOfMany(column string, relation string) error {
	return c.OfMany([]OfManyColumn{{Column: defaultOfManyColumn(column), Aggregate: "MAX"}}, relation)
}

// OldestOfMany answers CanBeOneOfMany::oldestOfMany.
func (c *CanBeOneOfMany) OldestOfMany(column string, relation string) error {
	return c.OfMany([]OfManyColumn{{Column: defaultOfManyColumn(column), Aggregate: "MIN"}}, relation)
}

// IsOneOfMany answers CanBeOneOfMany::isOneOfMany.
func (c *CanBeOneOfMany) IsOneOfMany() bool { return c.isOneOfMany }

// GetOneOfManySubQuery answers CanBeOneOfMany::getOneOfManySubQuery.
func (c *CanBeOneOfMany) GetOneOfManySubQuery() Builder { return c.oneOfManySubQuery }

// GetRelationName answers CanBeOneOfMany::getRelationName: the join alias.
func (c *CanBeOneOfMany) GetRelationName() string { return c.relationName }

// GetRelationQuery answers CanBeOneOfMany::getRelationQuery: the constraints go
// on the subquery when there is one, because a where on the outer query would
// filter after the aggregate has already chosen the row.
func (c *CanBeOneOfMany) GetRelationQuery(fallback Builder) Builder {
	if c.isOneOfMany && c.oneOfManySubQuery != nil {
		return c.oneOfManySubQuery
	}
	return fallback
}

// QualifySubSelectColumn answers CanBeOneOfMany::qualifySubSelectColumn.
func (c *CanBeOneOfMany) QualifySubSelectColumn(column string) string {
	segments := strings.Split(column, ".")
	return c.relationName + "." + segments[len(segments)-1]
}

// QualifyRelatedColumn answers CanBeOneOfMany::qualifyRelatedColumn.
func (c *CanBeOneOfMany) QualifyRelatedColumn(column string) string {
	return c.OneOfManyRelated().QualifyColumn(column)
}

// MergeOneOfManyJoinsTo answers CanBeOneOfMany::mergeOneOfManyJoinsTo: the
// existence query needs the same join the relation query got.
func (c *CanBeOneOfMany) MergeOneOfManyJoinsTo(target Builder) {
	source := c.OneOfManyQuery().GetQuery()
	destination := target.GetQuery()

	destination.BeforeQueryCallbacks = append(destination.BeforeQueryCallbacks, source.BeforeQueryCallbacks...)
	destination.ApplyBeforeQueryCallbacks()
}

// getDefaultOneOfManyJoinAlias answers
// CanBeOneOfMany::getDefaultOneOfManyJoinAlias.
func (c *CanBeOneOfMany) getDefaultOneOfManyJoinAlias(relation string) string {
	if relation == c.OneOfManyRelated().GetTable() {
		return relation + "_of_many"
	}
	return relation
}

// newOneOfManySubQuery answers CanBeOneOfMany::newOneOfManySubQuery.
func (c *CanBeOneOfMany) newOneOfManySubQuery(groupBy []any, columns []string, aggregate string) Builder {
	subQuery := c.OneOfManyRelated().NewQuery()

	for _, group := range groupBy {
		if column, ok := group.(string); ok {
			subQuery.GroupBy(c.QualifyRelatedColumn(column))
			continue
		}
		subQuery.GroupBy(group)
	}

	grammar := subQuery.GetQuery().GetGrammar()
	for index, column := range columns {
		aggregated := grammar.Wrap(c.QualifyRelatedColumn(column))
		if index == 0 {
			aggregated = aggregate + "(" + aggregated + ")"
		} else {
			aggregated = "min(" + aggregated + ")"
		}
		subQuery.SelectRaw(aggregated + " as " + grammar.Wrap(column+"_aggregate"))
	}

	if c.AddOneOfManySubQueryConstraints != nil {
		c.AddOneOfManySubQueryConstraints(subQuery, "", aggregate)
	}

	return subQuery
}

// addOneOfManyJoinSubQuery answers CanBeOneOfMany::addOneOfManyJoinSubQuery.
//
// The PHP calls joinSub, which the base builder in this collection does not
// have. The join is written out instead: the compiled subquery becomes the
// table expression and its bindings are merged into the join segment, which is
// what joinSub does with three fewer lines of ceremony.
func (c *CanBeOneOfMany) addOneOfManyJoinSubQuery(parent Builder, subQuery Builder, on []string) {
	alias := c.relationName

	parent.GetQuery().BeforeQuery(func(b *query.Builder) {
		sub := subQuery.GetQuery()
		sub.ApplyBeforeQueryCallbacks()

		b.AddBinding(sub.GetBindings(), "join")
		b.Join(query.Raw("("+sub.ToSQL()+") as "+alias), func(join *query.JoinClause) {
			for _, column := range on {
				join.On(c.QualifySubSelectColumn(column+"_aggregate"), "=", c.QualifyRelatedColumn(column))
			}
			if c.AddOneOfManyJoinSubQueryConstraints != nil {
				c.AddOneOfManyJoinSubQueryConstraints(join)
			}
		})
	})
}

func hasColumn(columns []OfManyColumn, name string) bool {
	for _, column := range columns {
		if column.Column == name {
			return true
		}
	}
	return false
}

func defaultOfManyColumn(column string) string {
	if column == "" {
		return "id"
	}
	return column
}

func isStar(columns []any) bool {
	return len(columns) == 1 && columns[0] == "*"
}
