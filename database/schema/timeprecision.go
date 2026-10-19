package schema

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MaxTimePrecision is the finest fractional-second precision a date-time
// column takes on PostgreSQL and MySQL: microseconds.
const MaxTimePrecision = 6

// WidenTimePrecision raises every date-time column of table whose
// fractional-second precision is below precision to precision, and answers
// the columns it altered.
//
// It exists for the columns a migration created while the default precision
// was zero. PostgreSQL rounds a value written to a timestamp(0) column to the
// nearest second, and MySQL truncates it to the second, so two rows written in
// the same second read back as written at the same instant -- their order is
// lost, and a value read back can be later than the moment it was written. The
// default is now MaxTimePrecision; a table created before carries the old
// columns until this widens them.
//
// The columns are read off the database rather than named by the caller, so a
// migration does not have to list what an earlier migration declared, and a
// column already at precision or finer is left alone: running it twice alters
// nothing the second time. Only timestamp, timestamp with time zone, time and
// time with time zone on PostgreSQL, and datetime, timestamp and time on MySQL,
// are touched; a date or an interval has no fractional second to widen.
//
// Every column of the table goes in one statement. On PostgreSQL that is one
// ALTER TABLE whose clauses change only the type modifier: nullability,
// defaults, comments and indexes stay as they are, and raising the precision
// of these types needs no rewrite of the table, since every value stored at
// the lower precision is already a value of the higher one. On MySQL each
// column is restated whole by MODIFY -- its nullability, its default, its
// on-update clause and its comment are read back and written again -- and the
// engine may copy the table to do it.
//
// SQLite stores date-time values as text with whatever fraction they were
// written with, so there is nothing to widen and nothing is run there.
//
// precision must be between 1 and MaxTimePrecision.
func (b *Builder) WidenTimePrecision(ctx context.Context, table string, precision int) ([]string, error) {
	if precision < 1 || precision > MaxTimePrecision {
		return nil, fmt.Errorf("schema: a time precision is between 1 and %d, got %d", MaxTimePrecision, precision)
	}
	switch driver := b.connection.GetDriverName(); driver {
	case "sqlite":
		return nil, nil
	case "pgsql":
		return b.widenPostgresTimePrecision(ctx, table, precision)
	case "mysql", "mariadb":
		return b.widenMySQLTimePrecision(ctx, table, precision)
	default:
		return nil, fmt.Errorf("%w: widening time precision on %s", ErrUnsupported, driver)
	}
}

// postgresTimeType matches format_type's spelling of the four date-time types
// that carry a fractional second, with or without an explicit precision.
var postgresTimeType = regexp.MustCompile(`^(timestamp|time)(?:\((\d+)\))? (with|without) time zone$`)

func (b *Builder) widenPostgresTimePrecision(ctx context.Context, table string, precision int) ([]string, error) {
	columns, err := b.GetColumns(ctx, table)
	if err != nil {
		return nil, err
	}
	var clauses, altered []string
	for _, column := range columns {
		match := postgresTimeType.FindStringSubmatch(column.Type)
		if match == nil {
			continue
		}
		// No modifier is PostgreSQL's own default, which is microseconds.
		current := MaxTimePrecision
		if match[2] != "" {
			current, _ = strconv.Atoi(match[2])
		}
		if current >= precision {
			continue
		}
		clauses = append(clauses, fmt.Sprintf("alter column %s type %s(%d) %s time zone",
			b.grammar.Wrap(column.Name), match[1], precision, match[3]))
		altered = append(altered, column.Name)
	}
	if len(clauses) == 0 {
		return nil, nil
	}
	statement := "alter table " + b.grammar.WrapTable(table) + " " + strings.Join(clauses, ", ")
	if err := b.connection.Statement(ctx, statement); err != nil {
		return nil, err
	}
	return altered, nil
}

// mysqlTimeType matches information_schema's column_type for the three
// date-time types that carry a fractional second.
var mysqlTimeType = regexp.MustCompile(`^(datetime|timestamp|time)(?:\((\d+)\))?$`)

// mysqlCurrentTimestamp matches the spellings MySQL and MariaDB report for a
// CURRENT_TIMESTAMP default or on-update clause.
var mysqlCurrentTimestamp = regexp.MustCompile(`(?i)^(current_timestamp|now)(\(\d*\))?$`)

func (b *Builder) widenMySQLTimePrecision(ctx context.Context, table string, precision int) ([]string, error) {
	_, name, err := ParseSchemaAndTable(table)
	if err != nil {
		return nil, err
	}
	rows, err := b.connection.Select(ctx, "select column_name as `name`, column_type as `type`, is_nullable as `nullable`, "+
		"column_default as `default`, extra as `extra`, column_comment as `comment` "+
		"from information_schema.columns where table_schema = schema() and table_name = "+
		quoteLiteral(b.connection.GetTablePrefix()+name)+" order by ordinal_position")
	if err != nil {
		return nil, err
	}
	var clauses, altered []string
	for _, row := range rows {
		match := mysqlTimeType.FindStringSubmatch(strings.ToLower(recordText(row["type"])))
		if match == nil {
			continue
		}
		current := 0
		if match[2] != "" {
			current, _ = strconv.Atoi(match[2])
		}
		if current >= precision {
			continue
		}
		column := recordText(row["name"])
		clause := fmt.Sprintf("modify %s %s(%d)", b.grammar.Wrap(column), match[1], precision)
		if strings.EqualFold(recordText(row["nullable"]), "YES") {
			clause += " null"
		} else {
			clause += " not null"
		}
		if value, ok := row["default"]; ok && value != nil {
			text := recordText(value)
			if mysqlCurrentTimestamp.MatchString(text) {
				clause += fmt.Sprintf(" default CURRENT_TIMESTAMP(%d)", precision)
			} else {
				clause += " default " + quoteLiteral(text)
			}
		}
		if strings.Contains(strings.ToLower(recordText(row["extra"])), "on update") {
			clause += fmt.Sprintf(" on update CURRENT_TIMESTAMP(%d)", precision)
		}
		if comment := recordText(row["comment"]); comment != "" {
			clause += " comment " + quoteLiteral(comment)
		}
		clauses = append(clauses, clause)
		altered = append(altered, column)
	}
	if len(clauses) == 0 {
		return nil, nil
	}
	statement := "alter table " + b.grammar.WrapTable(table) + " " + strings.Join(clauses, ", ")
	if err := b.connection.Statement(ctx, statement); err != nil {
		return nil, err
	}
	return altered, nil
}

// recordText reads a value a schema query answered as text, whether the driver
// handed it back as a string or as bytes.
func recordText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

// quoteLiteral writes s as a single-quoted MySQL string literal, with the
// backslash escaped as MySQL reads it by default.
func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", "''") + "'"
}
