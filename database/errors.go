package database

import (
	"errors"
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/database/concerns"
)

// QueryException answers Illuminate\Database\QueryException: a statement
// failed, and the error says which one, on which connection, with which values.
//
// The message is the PHP's, assembled the same way: the driver's own sentence,
// then the connection with its host, port and database, then the SQL with the
// bindings written into the placeholders. That last part is why this type
// exists at all -- "syntax error at or near $3" names nothing a person can act
// on, and the same error with the values in it usually names the mistake.
type QueryException struct {
	// ConnectionName is QueryException::$connectionName.
	ConnectionName string

	// SQL is QueryException::$sql.
	SQL string

	// Bindings is QueryException::$bindings, already through PrepareBindings.
	Bindings []any

	// ConnectionDetails is QueryException::$connectionDetails: driver, name,
	// host, port, database, unix_socket.
	ConnectionDetails map[string]any

	// ReadWriteType is QueryException::$readWriteType.
	ReadWriteType string

	// Previous is the driver error, the PHP's $previous.
	Previous error
}

// NewQueryException answers QueryException::__construct.
func NewQueryException(connectionName, sql string, bindings []any, previous error, connectionDetails map[string]any, readWriteType string) *QueryException {
	return &QueryException{
		ConnectionName:    connectionName,
		SQL:               sql,
		Bindings:          bindings,
		ConnectionDetails: connectionDetails,
		ReadWriteType:     readWriteType,
		Previous:          previous,
	}
}

// Error answers QueryException::formatMessage, shape for shape.
func (e *QueryException) Error() string {
	message := ""
	if e.Previous != nil {
		message = e.Previous.Error()
	}
	return message + " (Connection: " + e.ConnectionName + e.formatConnectionDetails() +
		", SQL: " + substituteBindings(e.SQL, e.Bindings) + ")"
}

// formatConnectionDetails answers the protected
// QueryException::formatConnectionDetails.
func (e *QueryException) formatConnectionDetails() string {
	if len(e.ConnectionDetails) == 0 {
		return ""
	}

	driver, _ := e.ConnectionDetails["driver"].(string)

	var segments []string
	if driver != "sqlite" {
		if socket, _ := e.ConnectionDetails["unix_socket"].(string); socket != "" {
			segments = append(segments, "Socket: "+socket)
		} else {
			segments = append(segments,
				"Host: "+detail(e.ConnectionDetails, "host"),
				"Port: "+detail(e.ConnectionDetails, "port"))
		}
	}
	segments = append(segments, "Database: "+detail(e.ConnectionDetails, "database"))

	return ", " + strings.Join(segments, ", ")
}

func detail(details map[string]any, key string) string {
	switch v := details[key].(type) {
	case nil:
		return ""
	case string:
		return v
	case []string:
		return strings.Join(v, ", ")
	default:
		return fmt.Sprint(v)
	}
}

// Unwrap makes errors.Is and errors.As reach the driver error.
func (e *QueryException) Unwrap() error { return e.Previous }

// GetConnectionName answers QueryException::getConnectionName.
func (e *QueryException) GetConnectionName() string { return e.ConnectionName }

// GetSQL answers QueryException::getSql. The PHP spells it getSql.
func (e *QueryException) GetSQL() string { return e.SQL }

// GetBindings answers QueryException::getBindings.
func (e *QueryException) GetBindings() []any { return e.Bindings }

// GetConnectionDetails answers QueryException::getConnectionDetails.
func (e *QueryException) GetConnectionDetails() map[string]any { return e.ConnectionDetails }

// GetRawSQL answers QueryException::getRawSql: the statement with its bindings
// written in.
//
// The PHP asks the DB facade for the connection by name and goes back through
// its grammar. There is no facade here (ADR 0002), and there does not need to
// be: the bindings are already on the exception.
func (e *QueryException) GetRawSQL() string { return substituteBindings(e.SQL, e.Bindings) }

// UniqueConstraintViolationException answers
// Illuminate\Database\UniqueConstraintViolationException, which extends
// QueryException there and embeds it here.
//
// It exists so firstOrCreate and its neighbours can catch the one failure they
// expect -- two requests inserting the same row at the same time -- without
// catching every other statement error with it.
type UniqueConstraintViolationException struct{ QueryException }

// NewUniqueConstraintViolationException answers its constructor, which is
// QueryException's.
func NewUniqueConstraintViolationException(connectionName, sql string, bindings []any, previous error, connectionDetails map[string]any, readWriteType string) *UniqueConstraintViolationException {
	return &UniqueConstraintViolationException{
		*NewQueryException(connectionName, sql, bindings, previous, connectionDetails, readWriteType),
	}
}

// DeadlockException answers Illuminate\Database\DeadlockException.
//
// It is an alias rather than a declaration because ManagesTransactions is what
// raises it and lives in the concerns package, which this one imports -- so
// declaring the type here would close the cycle. An alias is one type under two
// names, which is what PHP's class_alias is, so errors.As works whichever name
// a caller reaches for.
type DeadlockException = concerns.DeadlockError

// NewDeadlockException answers `new DeadlockException(...)`.
func NewDeadlockException(previous error) *DeadlockException {
	return concerns.NewDeadlockError(previous)
}

// LostConnectionException answers
// Illuminate\Database\LostConnectionException: the connection went away and
// there was no reconnector to get it back.
type LostConnectionException struct {
	// Message is the sentence the PHP constructs it with.
	Message string
}

// NewLostConnectionException answers `new LostConnectionException($message)`.
func NewLostConnectionException(message string) *LostConnectionException {
	return &LostConnectionException{Message: message}
}

// Error is the message.
func (e *LostConnectionException) Error() string { return e.Message }

// ErrRecordNotFound answers Illuminate\Database\RecordNotFoundException.
//
// It is the same value as concerns.ErrRecordNotFound, re-exported: FirstOrFail
// lives there, and errors.Is has to keep working across both names, which it
// does because there is only one.
var ErrRecordNotFound = concerns.ErrRecordNotFound

// ErrRecordsNotFound is what Sole returns when the query matched no row at all.
//
// It is the same value as concerns.ErrRecordsNotFound, re-exported for the same
// reason ErrRecordNotFound is: Sole lives in that package, and errors.Is has to
// give the same answer under either name.
//
// Answers Illuminate\Database\RecordsNotFoundException.
var ErrRecordsNotFound = concerns.ErrRecordsNotFound

// MultipleRecordsFoundException is what Sole returns when the query matched
// more than one row, and it carries how many it saw.
//
// It is an alias of concerns.MultipleRecordsFoundError, not a second type: a
// value built by either package satisfies errors.As under both names. See that
// type for what the count means.
//
// Answers Illuminate\Database\MultipleRecordsFoundException.
type MultipleRecordsFoundException = concerns.MultipleRecordsFoundError

// NewMultipleRecordsFoundException answers its constructor.
func NewMultipleRecordsFoundException(count int) *MultipleRecordsFoundException {
	return concerns.NewMultipleRecordsFoundError(count)
}

// ErrMultipleColumnsSelected answers
// Illuminate\Database\MultipleColumnsSelectedException, which Scalar raises
// when the row it read has more than one column.
var ErrMultipleColumnsSelected = errors.New("database: more than one column was selected")

// ErrSQLiteDatabaseDoesNotExist answers
// Illuminate\Database\SQLiteDatabaseDoesNotExistException.
//
// The PHP carries the path in a property; this wraps it into the message,
// because a caller that has the error has nothing to do with the path except
// print it.
var ErrSQLiteDatabaseDoesNotExist = errors.New("database file does not exist")

// NewSQLiteDatabaseDoesNotExistException answers its constructor.
func NewSQLiteDatabaseDoesNotExistException(path string) error {
	return fmt.Errorf("%w: %s. Ensure this is an absolute path to the database", ErrSQLiteDatabaseDoesNotExist, path)
}

// substituteBindings writes the bindings into the placeholders, for a message a
// person reads.
//
// It is Str::replaceArray('?', $bindings, $sql), and it is only ever used to
// build a message: nothing here executes what it produces, and nothing should.
// A string built this way is the injection this framework exists to make
// impossible -- it is safe here precisely because it goes to a log and not to a
// server.
func substituteBindings(sql string, bindings []any) string {
	var b strings.Builder
	next := 0

	for i := 0; i < len(sql); i++ {
		if sql[i] != '?' || next >= len(bindings) {
			b.WriteByte(sql[i])
			continue
		}
		b.WriteString(formatBinding(bindings[next]))
		next++
	}
	return b.String()
}

// formatBinding writes one value the way a log wants to read it.
func formatBinding(v any) string {
	switch value := v.(type) {
	case nil:
		return "null"
	case string:
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	case bool:
		if value {
			return "1"
		}
		return "0"
	case []byte:
		return "'" + strings.ReplaceAll(string(value), "'", "''") + "'"
	default:
		return fmt.Sprint(value)
	}
}
