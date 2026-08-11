package database

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/database/concerns"
	dbevents "github.com/arandu-io/hesape/database/events"
	"github.com/arandu-io/hesape/database/query"
)

// Dispatcher answers Illuminate\Contracts\Events\Dispatcher, narrowed to the
// one method a connection calls on it.
//
// It is declared here rather than imported from hesape/events so that the SQL
// package keeps its own dependency list, which is the same reason every other
// interface in this component is declared where it is consumed.
type Dispatcher interface {
	// Dispatch fires an event.
	Dispatch(event any)
}

// QueryLogEntry is one row of Connection::$queryLog.
//
// PHP's is an untyped array with four keys; this is those keys.
type QueryLogEntry struct {
	// Query is the SQL as it was issued.
	Query string

	// Bindings are the values that went with it.
	Bindings []any

	// Time is how long it took, in milliseconds, which is the unit the PHP
	// records and the one every listener compares against.
	Time float64

	// ReadWriteType is "read", "write", or empty.
	ReadWriteType string
}

// Connection answers Illuminate\Database\Connection: one open connection, and
// everything that runs statements through it.
//
// # Where the Grant is, and why it is not on these methods
//
// RULE 17 is absolute: no path to application rows skips a Policy, on the way
// out as much as on the way in. That path is Repository, one file over, whose
// every method takes an auth.Grant and filters by auth.Tenant(g) -- and it is
// the only door application code goes through.
//
// A Connection is the plumbing underneath that door, and it takes no Grant on
// purpose. It could not use one: a Grant is enforced by filtering a query by
// tenant, and this type is handed a finished string. A parameter that looks
// like enforcement and enforces nothing is worse than no parameter, because the
// next reader stops checking -- the same reasoning that removed Query.Filter.
// The other half of the argument is that `aru migrate` runs here, in a process
// with no request and no subject, where a Grant cannot be constructed at all
// (RULE 14 leaves no system grant to fall back to).
//
// So: reach rows through a Repository. Reaching them through a Connection is
// how a module gets rejected in review.
//
// # PDO is *sql.DB
//
// The PHP holds a PDO and a second one for reads. Here both are *sql.DB, which
// is a pool rather than a socket -- so "reconnect" means replacing the pool,
// and the read/write split is two pools rather than two connections.
type Connection struct {
	// ManagesTransactions is the `use Concerns\ManagesTransactions` of the PHP
	// class: Transaction, BeginTransaction, Commit, RollBack,
	// TransactionLevel, AfterCommit and AfterRollBack are all reached through
	// it, spelled the way the PHP spells them.
	concerns.ManagesTransactions

	mu sync.RWMutex

	pdo     *sql.DB
	readPDO *sql.DB

	// txConn is the single pooled connection an open transaction is pinned to.
	//
	// PDO has no such field because a PDO is one connection; a *sql.DB is a
	// pool, and a BEGIN issued on the pool would be followed by statements on
	// whichever other connection happened to be free. Pinning is what makes
	// "the transaction" mean anything here.
	txConn *sql.Conn

	readPDOConfig map[string]any
	database      string
	readWriteType string
	tablePrefix   string
	config        map[string]any

	reconnector func(*Connection) error

	queryGrammar  query.Grammar
	schemaGrammar any
	postProcessor query.Processor

	events Dispatcher

	recordsModified       bool
	readOnWriteConnection bool

	queryLog           []QueryLogEntry
	loggingQueries     bool
	totalQueryDuration float64

	queryDurationHandlers []*queryDurationHandler
	queryListeners        []func(*dbevents.QueryExecuted)

	pretending bool

	beforeExecutingCallbacks []func(query string, bindings []any, connection *Connection)

	latestPDOTypeRetrieved string
}

// queryDurationHandler is one entry of Connection::$queryDurationHandlers.
type queryDurationHandler struct {
	hasRun    bool
	threshold float64
	handler   func(*Connection, *dbevents.QueryExecuted)
}

// NewConnection answers Connection::__construct.
//
// The PHP's first argument is a PDO or a closure returning one; here it is the
// pool, and a nil pool is the closure that was never called -- Reconnect is
// what fills it in.
func NewConnection(pdo *sql.DB, database, tablePrefix string, config map[string]any) *Connection {
	if config == nil {
		config = map[string]any{}
	}
	c := &Connection{
		pdo:         pdo,
		database:    database,
		tablePrefix: tablePrefix,
		config:      config,
	}
	c.UseDefaultQueryGrammar()
	c.UseDefaultPostProcessor()
	c.UseTransactions(c)
	return c
}

// DefaultQueryGrammar is where UseDefaultQueryGrammar gets its grammar.
//
// The PHP has one getDefaultQueryGrammar per connection subclass --
// MySqlConnection returns a MySqlGrammar and so on. Here the concrete grammars
// live in database/query/grammars, which imports query, so a connection that
// constructed one directly would tie this package to all of them. A connector
// sets this once, from its init, next to the driver it registers.
//
// Nil leaves the grammar unset, which is what a connection built for a test
// that never compiles SQL wants.
var DefaultQueryGrammar func(dialect Dialect) query.Grammar

// UseDefaultQueryGrammar answers Connection::useDefaultQueryGrammar.
func (c *Connection) UseDefaultQueryGrammar() {
	if DefaultQueryGrammar == nil {
		return
	}
	c.SetQueryGrammar(DefaultQueryGrammar(c.dialect()))
}

// UseDefaultSchemaGrammar answers Connection::useDefaultSchemaGrammar.
//
// The base class's getDefaultSchemaGrammar returns null, and so does this: a
// connection with no driver subclass has no schema grammar, which is exactly
// what the PHP means by an empty method body.
func (c *Connection) UseDefaultSchemaGrammar() {}

// UseDefaultPostProcessor answers Connection::useDefaultPostProcessor.
var DefaultPostProcessor func() query.Processor

// UseDefaultPostProcessor answers Connection::useDefaultPostProcessor.
func (c *Connection) UseDefaultPostProcessor() {
	if DefaultPostProcessor == nil {
		return
	}
	c.SetPostProcessor(DefaultPostProcessor())
}

// dialect reads the driver name off the configuration and answers the Dialect
// for it, which is what DefaultQueryGrammar is keyed by.
func (c *Connection) dialect() Dialect {
	if d, err := ParseDialect(c.GetDriverName()); err == nil {
		return d
	}
	return DialectSQLite
}

// Table answers Connection::table: a query builder against one table.
//
// It takes a context, which the PHP's does not: a builder that cannot be
// cancelled holds a connection open for as long as the server feels like, and
// there is nowhere else to put the context once the builder exists.
func (c *Connection) Table(ctx context.Context, table any, as ...string) *query.Builder {
	return c.Query(ctx).From(table, as...)
}

// Query answers Connection::query: a fresh query builder on this connection.
func (c *Connection) Query(ctx context.Context) *query.Builder {
	return query.NewBuilder(&boundConnection{connection: c, ctx: ctx}, c.GetQueryGrammar(), c.GetPostProcessor())
}

// boundConnection is what makes a Connection usable as a query.Connection.
//
// query.Connection takes no context, because in PHP there is none to take. The
// builder gets one bound at the moment it is created, which is the only place a
// context can enter without changing an interface this package does not own.
type boundConnection struct {
	connection *Connection
	ctx        context.Context
}

func (b *boundConnection) Select(q string, bindings []any, useReadPDO bool) ([]query.Record, error) {
	return b.connection.Select(b.ctx, q, bindings, useReadPDO)
}

func (b *boundConnection) Insert(q string, bindings []any) (bool, error) {
	return b.connection.Insert(b.ctx, q, bindings)
}

func (b *boundConnection) Update(q string, bindings []any) (int64, error) {
	return b.connection.Update(b.ctx, q, bindings)
}

func (b *boundConnection) Delete(q string, bindings []any) (int64, error) {
	return b.connection.Delete(b.ctx, q, bindings)
}

func (b *boundConnection) Statement(q string, bindings []any) (bool, error) {
	return b.connection.Statement(b.ctx, q, bindings)
}

// SelectOne answers Connection::selectOne: the first row, or false for none.
//
// The PHP answers null for an empty result. A Go map is nil in that case, which
// is indistinguishable from a row of no columns, so the second value says which
// it was.
func (c *Connection) SelectOne(ctx context.Context, q string, bindings []any, useReadPDO bool) (query.Record, bool, error) {
	records, err := c.Select(ctx, q, bindings, useReadPDO)
	if err != nil {
		return nil, false, err
	}
	if len(records) == 0 {
		return nil, false, nil
	}
	return records[0], true, nil
}

// Scalar answers Connection::scalar: the first column of the first row.
//
// More than one column selected is an error, as it is there: a query that
// answers two columns and is read as one is a query somebody edited without
// reading its caller.
func (c *Connection) Scalar(ctx context.Context, q string, bindings []any, useReadPDO bool) (any, error) {
	record, found, err := c.SelectOne(ctx, q, bindings, useReadPDO)
	if err != nil || !found {
		return nil, err
	}
	if len(record) > 1 {
		return nil, ErrMultipleColumnsSelected
	}
	for _, value := range record {
		return value, nil
	}
	return nil, nil
}

// SelectFromWriteConnection answers
// Connection::selectFromWriteConnection: a read that must see writes this
// request already made.
func (c *Connection) SelectFromWriteConnection(ctx context.Context, q string, bindings []any) ([]query.Record, error) {
	return c.Select(ctx, q, bindings, false)
}

// Select answers Connection::select.
func (c *Connection) Select(ctx context.Context, q string, bindings []any, useReadPDO bool) ([]query.Record, error) {
	var records []query.Record

	err := c.run(ctx, q, bindings, func(runQuery string, runBindings []any) error {
		if c.Pretending() {
			return nil
		}

		pool, err := c.getPDOForSelect(useReadPDO)
		if err != nil {
			return err
		}

		rows, err := pool.QueryContext(ctx, runQuery, c.PrepareBindings(runBindings)...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		records, err = scanRecords(rows)
		return err
	})

	return records, err
}

// SelectResultSets answers Connection::selectResultSets: every result set a
// statement produced, not only the first.
//
// database/sql exposes the second set through Rows.NextResultSet, which is what
// PDOStatement::nextRowset is. A driver that does not support it answers one
// set, which is also what PDO does.
func (c *Connection) SelectResultSets(ctx context.Context, q string, bindings []any, useReadPDO bool) ([][]query.Record, error) {
	var sets [][]query.Record

	err := c.run(ctx, q, bindings, func(runQuery string, runBindings []any) error {
		if c.Pretending() {
			return nil
		}

		pool, err := c.getPDOForSelect(useReadPDO)
		if err != nil {
			return err
		}

		rows, err := pool.QueryContext(ctx, runQuery, c.PrepareBindings(runBindings)...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()

		for {
			set, err := scanRecords(rows)
			if err != nil {
				return err
			}
			sets = append(sets, set)

			if !rows.NextResultSet() {
				return rows.Err()
			}
		}
	})

	return sets, err
}

// Cursor answers Connection::cursor: the rows one at a time, without holding
// the whole result set.
//
// The PHP returns a Generator. Go 1.23 has range-over-func, so this returns an
// iterator with the same shape as concerns.Lazy: the error is yielded once and
// ends the iteration, so a caller that forgets to check it still stops.
func (c *Connection) Cursor(ctx context.Context, q string, bindings []any, useReadPDO bool) func(yield func(query.Record, error) bool) {
	return func(yield func(query.Record, error) bool) {
		if c.Pretending() {
			return
		}

		pool, err := c.getPDOForSelect(useReadPDO)
		if err != nil {
			yield(nil, err)
			return
		}

		start := time.Now()
		rows, err := pool.QueryContext(ctx, q, c.PrepareBindings(bindings)...)
		c.LogQuery(q, bindings, elapsed(start))
		if err != nil {
			yield(nil, c.wrapQueryError(err, q, bindings))
			return
		}
		defer func() { _ = rows.Close() }()

		columns, err := rows.Columns()
		if err != nil {
			yield(nil, err)
			return
		}

		for rows.Next() {
			record, err := scanRecord(rows, columns)
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(record, nil) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			yield(nil, err)
		}
	}
}

// Insert answers Connection::insert.
func (c *Connection) Insert(ctx context.Context, q string, bindings []any) (bool, error) {
	return c.Statement(ctx, q, bindings)
}

// Update answers Connection::update: the number of rows it changed.
func (c *Connection) Update(ctx context.Context, q string, bindings []any) (int64, error) {
	return c.AffectingStatement(ctx, q, bindings)
}

// Delete answers Connection::delete: the number of rows it removed.
func (c *Connection) Delete(ctx context.Context, q string, bindings []any) (int64, error) {
	return c.AffectingStatement(ctx, q, bindings)
}

// Statement answers Connection::statement: run something that answers neither
// rows nor a count.
func (c *Connection) Statement(ctx context.Context, q string, bindings []any) (bool, error) {
	ok := false

	err := c.run(ctx, q, bindings, func(runQuery string, runBindings []any) error {
		if c.Pretending() {
			ok = true
			return nil
		}

		pool, err := c.GetPDO()
		if err != nil {
			return err
		}

		c.RecordsHaveBeenModified(true)

		_, err = pool.ExecContext(ctx, runQuery, c.PrepareBindings(runBindings)...)
		ok = err == nil
		return err
	})

	return ok, err
}

// AffectingStatement answers Connection::affectingStatement.
func (c *Connection) AffectingStatement(ctx context.Context, q string, bindings []any) (int64, error) {
	var count int64

	err := c.run(ctx, q, bindings, func(runQuery string, runBindings []any) error {
		if c.Pretending() {
			return nil
		}

		pool, err := c.GetPDO()
		if err != nil {
			return err
		}

		result, err := pool.ExecContext(ctx, runQuery, c.PrepareBindings(runBindings)...)
		if err != nil {
			return err
		}

		count, err = result.RowsAffected()
		if err != nil {
			// A driver that cannot count is not a failed statement. The PHP
			// never meets this because PDO always answers rowCount.
			count = 0
		}
		c.RecordsHaveBeenModified(count > 0)
		return nil
	})

	return count, err
}

// Unprepared answers Connection::unprepared: run the statement as it is, with
// no prepare and no bindings.
//
// It is what a schema dump is loaded with, and nothing else should use it: a
// value that reaches a database through this has not been through a
// placeholder.
func (c *Connection) Unprepared(ctx context.Context, q string) (bool, error) {
	changed := false

	err := c.run(ctx, q, nil, func(runQuery string, _ []any) error {
		if c.Pretending() {
			changed = true
			return nil
		}

		pool, err := c.GetPDO()
		if err != nil {
			return err
		}

		_, err = pool.ExecContext(ctx, runQuery)
		changed = err == nil
		c.RecordsHaveBeenModified(changed)
		return err
	})

	return changed, err
}

// ThreadCount answers Connection::threadCount: how many connections the server
// has open, or zero when the grammar cannot ask.
func (c *Connection) ThreadCount(ctx context.Context) (int64, error) {
	grammar, ok := c.GetQueryGrammar().(interface{ CompileThreadCount() string })
	if !ok {
		return 0, nil
	}
	q := grammar.CompileThreadCount()
	if q == "" {
		return 0, nil
	}

	value, err := c.Scalar(ctx, q, nil, true)
	if err != nil {
		return 0, err
	}
	return toInt64(value), nil
}

// Pretend answers Connection::pretend: run the callback with nothing reaching
// the server, and answer the statements it would have run.
func (c *Connection) Pretend(ctx context.Context, callback func(*Connection) error) ([]QueryLogEntry, error) {
	var log []QueryLogEntry

	err := c.withFreshQueryLog(func() error {
		c.mu.Lock()
		c.pretending = true
		c.mu.Unlock()

		defer func() {
			c.mu.Lock()
			c.pretending = false
			c.mu.Unlock()
		}()

		err := callback(c)

		log = c.GetQueryLog()
		return err
	})

	return log, err
}

// WithoutPretending answers Connection::withoutPretending: run the callback for
// real even inside a Pretend.
func (c *Connection) WithoutPretending(callback func() error) error {
	c.mu.RLock()
	pretending := c.pretending
	c.mu.RUnlock()

	if !pretending {
		return callback()
	}

	c.mu.Lock()
	c.pretending = false
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.pretending = true
		c.mu.Unlock()
	}()

	return callback()
}

// withFreshQueryLog answers the protected Connection::withFreshQueryLog.
func (c *Connection) withFreshQueryLog(callback func() error) error {
	c.mu.Lock()
	logging := c.loggingQueries
	c.loggingQueries = true
	c.queryLog = nil
	c.mu.Unlock()

	err := callback()

	c.mu.Lock()
	c.loggingQueries = logging
	c.mu.Unlock()

	return err
}

// BindValues answers Connection::bindValues.
//
// PDO needs each value bound with a type; database/sql infers it from the Go
// type, so this is the type conversion PrepareBindings does not do -- and it is
// the same list: an int is an int, a byte slice is a blob, everything else is a
// string as far as the driver is concerned.
func (c *Connection) BindValues(bindings []any) []any { return c.PrepareBindings(bindings) }

// PrepareBindings answers Connection::prepareBindings.
//
// A time becomes the string the grammar's date format spells, and a bool
// becomes 0 or 1 -- both because the engines disagree about the wire form and
// the grammar is the one that knows which.
func (c *Connection) PrepareBindings(bindings []any) []any {
	if len(bindings) == 0 {
		return bindings
	}

	layout := "2006-01-02 15:04:05"
	if grammar := c.GetQueryGrammar(); grammar != nil {
		layout = grammar.GetDateFormat()
	}

	out := make([]any, len(bindings))
	for i, value := range bindings {
		switch v := value.(type) {
		case time.Time:
			out[i] = v.Format(layout)
		case bool:
			if v {
				out[i] = 1
			} else {
				out[i] = 0
			}
		default:
			out[i] = value
		}
	}
	return out
}

// run answers the protected Connection::run: the before-hooks, the reconnect,
// the timing, the retry on a lost connection, and the log.
func (c *Connection) run(ctx context.Context, q string, bindings []any, callback func(string, []any) error) error {
	c.mu.RLock()
	before := slices.Clone(c.beforeExecutingCallbacks)
	c.mu.RUnlock()

	for _, hook := range before {
		hook(q, bindings, c)
	}

	if err := c.ReconnectIfMissingConnection(); err != nil {
		return err
	}

	start := time.Now()

	err := c.runQueryCallback(q, bindings, callback)
	if err != nil {
		err = c.handleQueryException(err, q, bindings, callback)
	}

	c.LogQuery(q, bindings, elapsed(start))

	return err
}

// runQueryCallback answers the protected Connection::runQueryCallback: it turns
// a driver error into a QueryException carrying the statement and its values.
func (c *Connection) runQueryCallback(q string, bindings []any, callback func(string, []any) error) error {
	if err := callback(q, bindings); err != nil {
		return c.wrapQueryError(err, q, bindings)
	}
	return nil
}

// wrapQueryError builds the QueryException, or the unique-constraint subtype
// when the driver said that is what it was.
func (c *Connection) wrapQueryError(err error, q string, bindings []any) error {
	if c.IsUniqueConstraintError(err) {
		return NewUniqueConstraintViolationException(
			c.GetNameWithReadWriteType(), q, c.PrepareBindings(bindings), err,
			c.GetConnectionDetails(), c.latestReadWriteTypeUsed())
	}
	return NewQueryException(
		c.GetNameWithReadWriteType(), q, c.PrepareBindings(bindings), err,
		c.GetConnectionDetails(), c.latestReadWriteTypeUsed())
}

// IsUniqueConstraintError answers the protected
// Connection::isUniqueConstraintError, which is false on the base class and
// overridden per driver.
//
// It is exported because Go has no protected and a driver connection lives in
// another package -- a connector sets UniqueConstraintDetector rather than
// subclassing, which is the same decision as DefaultQueryGrammar.
func (c *Connection) IsUniqueConstraintError(err error) bool {
	if UniqueConstraintDetector == nil {
		return false
	}
	return UniqueConstraintDetector(c.GetDriverName(), err)
}

// UniqueConstraintDetector says whether a driver error was a unique constraint
// violation. A connector sets it next to the driver it registers, because the
// answer is the driver's SQLSTATE and nothing this package can read.
var UniqueConstraintDetector func(driver string, err error) bool

// LogQuery answers Connection::logQuery.
func (c *Connection) LogQuery(q string, bindings []any, timeMS float64) {
	c.mu.Lock()
	c.totalQueryDuration += timeMS
	logging := c.loggingQueries
	pretending := c.pretending
	handlers := slices.Clone(c.queryDurationHandlers)
	listeners := slices.Clone(c.queryListeners)
	total := c.totalQueryDuration
	c.mu.Unlock()

	readWriteType := c.latestReadWriteTypeUsed()

	event := dbevents.NewQueryExecuted(q, bindings, timeMS, c, readWriteType)
	c.event(event)

	for _, listener := range listeners {
		listener(event)
	}

	for _, handler := range handlers {
		if !handler.hasRun && total > handler.threshold {
			handler.handler(c, event)
			handler.hasRun = true
		}
	}

	logged := q
	if pretending {
		logged = substituteBindings(q, c.PrepareBindings(bindings))
	}

	if logging {
		c.mu.Lock()
		c.queryLog = append(c.queryLog, QueryLogEntry{
			Query: logged, Bindings: bindings, Time: timeMS, ReadWriteType: readWriteType,
		})
		c.mu.Unlock()
	}
}

// elapsed answers the protected Connection::getElapsedTime: milliseconds,
// rounded to two decimals.
func elapsed(start time.Time) float64 {
	ms := float64(time.Since(start).Microseconds()) / 1000
	return float64(int64(ms*100+0.5)) / 100
}

// WhenQueryingForLongerThan answers
// Connection::whenQueryingForLongerThan: run the handler once the connection
// has spent more than the threshold on queries.
//
// The threshold is a time.Duration rather than the PHP's float-of-milliseconds
// or DateTimeInterface or CarbonInterval: three spellings of one number is
// exactly what RULE 9 exists to remove, and Go has one type for a duration.
func (c *Connection) WhenQueryingForLongerThan(threshold time.Duration, handler func(*Connection, *dbevents.QueryExecuted)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queryDurationHandlers = append(c.queryDurationHandlers, &queryDurationHandler{
		threshold: float64(threshold.Microseconds()) / 1000,
		handler:   handler,
	})
}

// AllowQueryDurationHandlersToRunAgain answers
// Connection::allowQueryDurationHandlersToRunAgain.
func (c *Connection) AllowQueryDurationHandlersToRunAgain() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, handler := range c.queryDurationHandlers {
		handler.hasRun = false
	}
}

// TotalQueryDuration answers Connection::totalQueryDuration, in milliseconds.
func (c *Connection) TotalQueryDuration() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalQueryDuration
}

// ResetTotalQueryDuration answers Connection::resetTotalQueryDuration.
func (c *Connection) ResetTotalQueryDuration() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalQueryDuration = 0
}

// handleQueryException answers the protected
// Connection::handleQueryException: inside a transaction the error goes
// straight out, because retrying half a transaction is not retrying anything.
func (c *Connection) handleQueryException(err error, q string, bindings []any, callback func(string, []any) error) error {
	if c.TransactionLevel() >= 1 {
		return err
	}
	return c.tryAgainIfCausedByLostConnection(err, q, bindings, callback)
}

// tryAgainIfCausedByLostConnection answers the protected method of the same
// name.
func (c *Connection) tryAgainIfCausedByLostConnection(err error, q string, bindings []any, callback func(string, []any) error) error {
	var previous error = err
	if queryErr, ok := err.(*QueryException); ok {
		previous = queryErr.Previous
	}

	if c.CausedByLostConnection(previous) {
		if reconnectErr := c.Reconnect(); reconnectErr != nil {
			return err
		}
		return c.runQueryCallback(q, bindings, callback)
	}
	return err
}

// Reconnect answers Connection::reconnect.
func (c *Connection) Reconnect() error {
	c.mu.RLock()
	reconnector := c.reconnector
	c.mu.RUnlock()

	if reconnector != nil {
		return reconnector(c)
	}
	return NewLostConnectionException("Lost connection and no reconnector available.")
}

// ReconnectIfMissingConnection answers
// Connection::reconnectIfMissingConnection.
func (c *Connection) ReconnectIfMissingConnection() error {
	c.mu.RLock()
	missing := c.pdo == nil
	c.mu.RUnlock()

	if missing {
		return c.Reconnect()
	}
	return nil
}

// Disconnect answers Connection::disconnect.
func (c *Connection) Disconnect() {
	c.SetPDO(nil)
	c.SetReadPDO(nil)
}

// BeforeExecuting answers Connection::beforeExecuting.
func (c *Connection) BeforeExecuting(callback func(query string, bindings []any, connection *Connection)) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.beforeExecutingCallbacks = append(c.beforeExecutingCallbacks, callback)
	return c
}

// Listen answers Connection::listen: run the callback for every query.
//
// The PHP registers the closure on the event dispatcher, keyed by the
// QueryExecuted class name. There is no class-name-keyed dispatcher here (a
// string key for a type is what generics removed), so the connection keeps its
// own list and calls it alongside dispatching the event -- which is what a
// listener registered there would have got anyway.
func (c *Connection) Listen(callback func(*dbevents.QueryExecuted)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queryListeners = append(c.queryListeners, callback)
}

// FireConnectionEvent answers the protected
// Connection::fireConnectionEvent, with the PHP's own four names.
//
// It is exported because ManagesTransactions calls it from the concerns
// package, and Go has no protected.
func (c *Connection) FireConnectionEvent(event string) {
	switch event {
	case "beganTransaction":
		c.event(dbevents.NewTransactionBeginning(c))
	case "committed":
		c.event(dbevents.NewTransactionCommitted(c))
	case "committing":
		c.event(dbevents.NewTransactionCommitting(c))
	case "rollingBack":
		c.event(dbevents.NewTransactionRolledBack(c))
	}
}

// event answers the protected Connection::event.
func (c *Connection) event(e any) {
	c.mu.RLock()
	dispatcher := c.events
	c.mu.RUnlock()

	if dispatcher != nil {
		dispatcher.Dispatch(e)
	}
}

// Raw answers Connection::raw: a fragment of SQL the grammar leaves alone.
func (c *Connection) Raw(value any) query.Expression { return query.Raw(value) }

// Escape answers Connection::escape.
//
// It is here because the PHP has it, and it is the one method in this file that
// should almost never be called: a value that goes through here is a value that
// did not go through a placeholder. The grammar uses it to write a literal into
// a schema dump, which is the case it exists for.
func (c *Connection) Escape(value any, binary bool) (string, error) {
	switch v := value.(type) {
	case nil:
		return "null", nil
	case int, int8, int16, int32, int64, float32, float64:
		return fmt.Sprint(v), nil
	case bool:
		if v {
			return "1", nil
		}
		return "0", nil
	case []byte:
		if !binary {
			return "", fmt.Errorf("Strings with null bytes cannot be escaped. Use the binary escape option.")
		}
		return "X'" + fmt.Sprintf("%x", v) + "'", nil
	case string:
		if binary {
			return "X'" + fmt.Sprintf("%x", v) + "'", nil
		}
		if strings.ContainsRune(v, 0) {
			return "", fmt.Errorf("Strings with null bytes cannot be escaped. Use the binary escape option.")
		}
		return "'" + strings.ReplaceAll(v, "'", "''") + "'", nil
	default:
		return "", fmt.Errorf("The database connection does not support escaping values of this type.")
	}
}

// HasModifiedRecords answers Connection::hasModifiedRecords.
func (c *Connection) HasModifiedRecords() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.recordsModified
}

// RecordsHaveBeenModified answers Connection::recordsHaveBeenModified: it only
// ever sets the flag, never clears it, which is the PHP's `if (! ...)` guard.
func (c *Connection) RecordsHaveBeenModified(value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.recordsModified {
		c.recordsModified = value
	}
}

// SetRecordModificationState answers
// Connection::setRecordModificationState.
func (c *Connection) SetRecordModificationState(value bool) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recordsModified = value
	return c
}

// ForgetRecordModificationState answers
// Connection::forgetRecordModificationState.
func (c *Connection) ForgetRecordModificationState() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recordsModified = false
}

// UseWriteConnectionWhenReading answers
// Connection::useWriteConnectionWhenReading.
func (c *Connection) UseWriteConnectionWhenReading(value bool) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readOnWriteConnection = value
	return c
}

// getPDOForSelect answers the protected Connection::getPdoForSelect.
func (c *Connection) getPDOForSelect(useReadPDO bool) (*sql.DB, error) {
	if useReadPDO {
		return c.GetReadPDO()
	}
	return c.GetPDO()
}

// GetPDO answers Connection::getPdo. The PHP spells it getPdo; PDO is an
// initialism.
func (c *Connection) GetPDO() (*sql.DB, error) {
	c.mu.Lock()
	c.latestPDOTypeRetrieved = "write"
	pool := c.pdo
	c.mu.Unlock()

	if pool == nil {
		return nil, NewLostConnectionException("Lost connection and no reconnector available.")
	}
	return pool, nil
}

// GetRawPDO answers Connection::getRawPdo: the pool as it is, with no
// reconnect.
func (c *Connection) GetRawPDO() *sql.DB {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pdo
}

// GetReadPDO answers Connection::getReadPdo.
//
// The three conditions that send a read to the write pool are the PHP's: an
// open transaction, an explicit useWriteConnectionWhenReading, and a sticky
// connection that has already written. The last is what keeps a read-after-write
// in one request from landing on a replica that has not caught up.
func (c *Connection) GetReadPDO() (*sql.DB, error) {
	if c.TransactionLevel() > 0 {
		return c.GetPDO()
	}

	c.mu.RLock()
	onWrite := c.readOnWriteConnection
	modified := c.recordsModified
	readPool := c.readPDO
	c.mu.RUnlock()

	sticky, _ := c.GetConfig("sticky").(bool)
	if onWrite || (modified && sticky) {
		return c.GetPDO()
	}

	if readPool == nil {
		return c.GetPDO()
	}

	c.mu.Lock()
	c.latestPDOTypeRetrieved = "read"
	c.mu.Unlock()

	return readPool, nil
}

// GetRawReadPDO answers Connection::getRawReadPdo.
func (c *Connection) GetRawReadPDO() *sql.DB {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.readPDO
}

// SetPDO answers Connection::setPdo. It resets the transaction level, because
// whatever transaction was open belonged to the pool being replaced.
func (c *Connection) SetPDO(pdo *sql.DB) *Connection {
	c.ResetTransactionLevel()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.pdo = pdo
	return c
}

// SetReadPDO answers Connection::setReadPdo.
func (c *Connection) SetReadPDO(pdo *sql.DB) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readPDO = pdo
	return c
}

// SetReadPDOConfig answers Connection::setReadPdoConfig.
func (c *Connection) SetReadPDOConfig(config map[string]any) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readPDOConfig = config
	return c
}

// SetReconnector answers Connection::setReconnector.
func (c *Connection) SetReconnector(reconnector func(*Connection) error) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reconnector = reconnector
	return c
}

// GetName answers Connection::getName.
func (c *Connection) GetName() string {
	name, _ := c.GetConfig("name").(string)
	return name
}

// GetNameWithReadWriteType answers
// Connection::getNameWithReadWriteType.
func (c *Connection) GetNameWithReadWriteType() string {
	c.mu.RLock()
	readWriteType := c.readWriteType
	c.mu.RUnlock()

	name := c.GetName()
	if readWriteType != "" {
		name += "::" + readWriteType
	}
	return name
}

// GetConfig answers Connection::getConfig. An empty option answers nothing
// rather than the whole array, because a caller that wants the array can ask
// for the keys it needs.
func (c *Connection) GetConfig(option string) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config[option]
}

// GetConnectionDetails answers the protected
// Connection::getConnectionDetails, exported because QueryException carries it
// and Go has no protected.
func (c *Connection) GetConnectionDetails() map[string]any {
	c.mu.RLock()
	config := c.config
	if c.latestReadWriteTypeUsedLocked() == "read" && c.readPDOConfig != nil {
		config = c.readPDOConfig
	}
	c.mu.RUnlock()

	return map[string]any{
		"driver":      config["driver"],
		"name":        c.GetNameWithReadWriteType(),
		"host":        config["host"],
		"port":        config["port"],
		"database":    config["database"],
		"unix_socket": config["unix_socket"],
	}
}

// GetDriverName answers Connection::getDriverName.
func (c *Connection) GetDriverName() string {
	driver, _ := c.GetConfig("driver").(string)
	return driver
}

// GetDriverTitle answers Connection::getDriverTitle, which on the base class is
// the driver name.
func (c *Connection) GetDriverTitle() string { return c.GetDriverName() }

// GetQueryGrammar answers Connection::getQueryGrammar.
func (c *Connection) GetQueryGrammar() query.Grammar {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.queryGrammar
}

// SetQueryGrammar answers Connection::setQueryGrammar.
func (c *Connection) SetQueryGrammar(grammar query.Grammar) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queryGrammar = grammar
	return c
}

// GetSchemaGrammar answers Connection::getSchemaGrammar.
//
// It is any because the schema grammars live in database/schema/grammars, which
// nothing here should import: a connection needs to hold one and never to call
// it, and the schema builder that does call it knows the type.
func (c *Connection) GetSchemaGrammar() any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.schemaGrammar
}

// SetSchemaGrammar answers Connection::setSchemaGrammar.
func (c *Connection) SetSchemaGrammar(grammar any) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.schemaGrammar = grammar
	return c
}

// GetPostProcessor answers Connection::getPostProcessor.
func (c *Connection) GetPostProcessor() query.Processor {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.postProcessor
}

// SetPostProcessor answers Connection::setPostProcessor.
func (c *Connection) SetPostProcessor(processor query.Processor) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.postProcessor = processor
	return c
}

// GetEventDispatcher answers Connection::getEventDispatcher.
func (c *Connection) GetEventDispatcher() Dispatcher {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.events
}

// SetEventDispatcher answers Connection::setEventDispatcher.
func (c *Connection) SetEventDispatcher(dispatcher Dispatcher) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = dispatcher
	return c
}

// UnsetEventDispatcher answers Connection::unsetEventDispatcher.
func (c *Connection) UnsetEventDispatcher() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = nil
}

// Pretending answers Connection::pretending.
func (c *Connection) Pretending() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.pretending
}

// GetQueryLog answers Connection::getQueryLog.
func (c *Connection) GetQueryLog() []QueryLogEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return slices.Clone(c.queryLog)
}

// GetRawQueryLog answers Connection::getRawQueryLog: the log with the bindings
// written into each statement.
func (c *Connection) GetRawQueryLog() []QueryLogEntry {
	entries := c.GetQueryLog()
	out := make([]QueryLogEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, QueryLogEntry{
			Query:         substituteBindings(entry.Query, c.PrepareBindings(entry.Bindings)),
			Time:          entry.Time,
			ReadWriteType: entry.ReadWriteType,
		})
	}
	return out
}

// FlushQueryLog answers Connection::flushQueryLog.
func (c *Connection) FlushQueryLog() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queryLog = nil
}

// EnableQueryLog answers Connection::enableQueryLog.
func (c *Connection) EnableQueryLog() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loggingQueries = true
}

// DisableQueryLog answers Connection::disableQueryLog.
func (c *Connection) DisableQueryLog() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loggingQueries = false
}

// Logging answers Connection::logging.
func (c *Connection) Logging() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.loggingQueries
}

// GetDatabaseName answers Connection::getDatabaseName.
func (c *Connection) GetDatabaseName() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.database
}

// SetDatabaseName answers Connection::setDatabaseName.
func (c *Connection) SetDatabaseName(database string) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.database = database
	return c
}

// SetReadWriteType answers Connection::setReadWriteType.
func (c *Connection) SetReadWriteType(readWriteType string) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.readWriteType = readWriteType
	return c
}

// latestReadWriteTypeUsed answers the protected method of the same name.
func (c *Connection) latestReadWriteTypeUsed() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latestReadWriteTypeUsedLocked()
}

func (c *Connection) latestReadWriteTypeUsedLocked() string {
	if c.readWriteType != "" {
		return c.readWriteType
	}
	return c.latestPDOTypeRetrieved
}

// GetTablePrefix answers Connection::getTablePrefix.
func (c *Connection) GetTablePrefix() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tablePrefix
}

// SetTablePrefix answers Connection::setTablePrefix.
func (c *Connection) SetTablePrefix(prefix string) *Connection {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tablePrefix = prefix
	return c
}

// WithoutTablePrefix answers Connection::withoutTablePrefix.
func (c *Connection) WithoutTablePrefix(callback func(*Connection) error) error {
	prefix := c.GetTablePrefix()

	c.SetTablePrefix("")
	defer c.SetTablePrefix(prefix)

	return callback(c)
}

// GetServerVersion answers Connection::getServerVersion.
//
// The PHP reads PDO::ATTR_SERVER_VERSION, which database/sql has no equivalent
// of, so it asks the server: version() is spelled the same by PostgreSQL,
// MySQL and SQLite (sqlite_version() there, which the grammar overrides).
func (c *Connection) GetServerVersion(ctx context.Context) (string, error) {
	q := "SELECT version()"
	if c.GetDriverName() == string(DialectSQLite) {
		q = "SELECT sqlite_version()"
	}
	value, err := c.Scalar(ctx, q, nil, true)
	if err != nil {
		return "", err
	}
	return asText(value), nil
}

// resolvers answers Connection::$resolvers.
var (
	resolversMu sync.RWMutex
	resolvers   = map[string]func(pdo *sql.DB, database, prefix string, config map[string]any) *Connection{}
)

// ResolverFor answers Connection::resolverFor: register how a driver builds
// its connection.
//
// It is what lets a connector supply a driver-specific Connection without this
// package importing it, and it is the same hook the PHP has for the same reason
// -- there, so a package can add a driver Laravel does not ship.
func ResolverFor(driver string, callback func(pdo *sql.DB, database, prefix string, config map[string]any) *Connection) {
	resolversMu.Lock()
	defer resolversMu.Unlock()
	resolvers[driver] = callback
}

// GetResolver answers Connection::getResolver.
func GetResolver(driver string) func(pdo *sql.DB, database, prefix string, config map[string]any) *Connection {
	resolversMu.RLock()
	defer resolversMu.RUnlock()
	return resolvers[driver]
}

// The rest of concerns.TransactionDriver, which Connection satisfies so
// ManagesTransactions can drive it.

// ExecuteBeginTransactionStatement answers the protected
// Connection::executeBeginTransactionStatement.
//
// database/sql has no BEGIN outside sql.Tx, so this issues one on a dedicated
// connection taken from the pool, and holds it for the life of the transaction.
// It is what BeginTx does internally, spelled out, because the trait's
// bookkeeping is what decides when the matching COMMIT runs.
func (c *Connection) ExecuteBeginTransactionStatement() error {
	pool, err := c.GetPDO()
	if err != nil {
		return err
	}

	conn, err := pool.Conn(context.Background())
	if err != nil {
		return err
	}
	if _, err := conn.ExecContext(context.Background(), "BEGIN"); err != nil {
		_ = conn.Close()
		return err
	}

	c.mu.Lock()
	c.txConn = conn
	c.mu.Unlock()
	return nil
}

// CommitTransactionStatement is $this->getPdo()->commit().
func (c *Connection) CommitTransactionStatement() error {
	c.mu.Lock()
	conn := c.txConn
	c.txConn = nil
	c.mu.Unlock()

	if conn == nil {
		return nil
	}
	_, err := conn.ExecContext(context.Background(), "COMMIT")
	_ = conn.Close()
	return err
}

// RollBackTransactionStatement is the PDO rollBack, guarded by inTransaction.
func (c *Connection) RollBackTransactionStatement() (bool, error) {
	c.mu.Lock()
	conn := c.txConn
	c.txConn = nil
	c.mu.Unlock()

	if conn == nil {
		return false, nil
	}
	_, err := conn.ExecContext(context.Background(), "ROLLBACK")
	_ = conn.Close()
	return true, err
}

// ExecuteSavepointStatement runs a savepoint statement on the open transaction.
func (c *Connection) ExecuteSavepointStatement(statement string) error {
	c.mu.RLock()
	conn := c.txConn
	c.mu.RUnlock()

	if conn == nil {
		pool, err := c.GetPDO()
		if err != nil {
			return err
		}
		_, err = pool.ExecContext(context.Background(), statement)
		return err
	}
	_, err := conn.ExecContext(context.Background(), statement)
	return err
}

// SupportsSavepoints answers $this->queryGrammar->supportsSavepoints(), with
// the base grammar's answer when no grammar is set.
func (c *Connection) SupportsSavepoints() bool {
	if grammar := c.GetQueryGrammar(); grammar != nil {
		return grammar.SupportsSavepoints()
	}
	return baseGrammar.SupportsSavepoints()
}

// CompileSavepoint answers $this->queryGrammar->compileSavepoint().
func (c *Connection) CompileSavepoint(name string) string {
	if grammar := c.GetQueryGrammar(); grammar != nil {
		return grammar.CompileSavepoint(name)
	}
	return baseGrammar.CompileSavepoint(name)
}

// CompileSavepointRollBack answers
// $this->queryGrammar->compileSavepointRollBack().
func (c *Connection) CompileSavepointRollBack(name string) string {
	if grammar := c.GetQueryGrammar(); grammar != nil {
		return grammar.CompileSavepointRollBack(name)
	}
	return baseGrammar.CompileSavepointRollBack(name)
}

// CausedByLostConnection answers the DetectsLostConnections trait.
func (c *Connection) CausedByLostConnection(err error) bool { return CausedByLostConnection(err) }

// CausedByConcurrencyError answers the DetectsConcurrencyErrors trait.
func (c *Connection) CausedByConcurrencyError(err error) bool { return CausedByConcurrencyError(err) }

// SubstituteBindingsIntoRawSQL answers the grammar method QueryExecuted reaches
// for, so a Connection satisfies events.RawSQLConnection.
func (c *Connection) SubstituteBindingsIntoRawSQL(sql string, bindings []any) string {
	return substituteBindings(sql, bindings)
}

// baseGrammar is the fallback for the three savepoint questions, which the
// abstract Illuminate\Database\Query\Grammars\Grammar answers on its own.
var baseGrammar = &query.BaseGrammar{}

// scanRecords reads a whole result set into records.
func scanRecords(rows *sql.Rows) ([]query.Record, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var out []query.Record
	for rows.Next() {
		record, err := scanRecord(rows, columns)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

// scanRecord reads one row into a record.
//
// Every column is scanned into an any, which is what PDO::FETCH_OBJ produces:
// the driver decides the Go type, and the caller reads it. A []byte is turned
// into a string, because MySQL's text protocol answers every column that way
// and a caller comparing against a string would otherwise never match.
func scanRecord(rows *sql.Rows, columns []string) (query.Record, error) {
	values := make([]any, len(columns))
	pointers := make([]any, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}

	if err := rows.Scan(pointers...); err != nil {
		return nil, err
	}

	record := make(query.Record, len(columns))
	for i, column := range columns {
		if b, ok := values[i].([]byte); ok {
			record[column] = string(b)
			continue
		}
		record[column] = values[i]
	}
	return record, nil
}

func asText(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprint(v)
	}
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int32:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case string:
		return int64(parseUint(n))
	case []byte:
		return int64(parseUint(string(n)))
	default:
		return 0
	}
}

func parseUint(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// sortedNames is used by the manager to list connections in a stable order.
func sortedNames(names []string) []string {
	sort.Strings(names)
	return names
}

// SchemaBuilderFactory is where the schema package registers how to build a
// schema builder for a connection.
//
// The PHP's getSchemaBuilder does `new SchemaBuilder($this)`, which would make
// this package import database/schema while that package imports this one for
// the connection it builds against. The registration goes the other way and
// closes nothing.
var SchemaBuilderFactory func(*Connection) any

// GetSchemaBuilder answers Connection::getSchemaBuilder.
//
// It answers any rather than a schema builder type for the reason above. Nil
// means the binary never imported the schema package, which is a truthful
// answer to "give me the schema builder I did not link".
func (c *Connection) GetSchemaBuilder() any {
	if c.GetSchemaGrammar() == nil {
		c.UseDefaultSchemaGrammar()
	}
	if SchemaBuilderFactory == nil {
		return nil
	}
	return SchemaBuilderFactory(c)
}
