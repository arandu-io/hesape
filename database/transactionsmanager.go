package database

import (
	"sync"

	"github.com/arandu-io/hesape/database/concerns"
	dbevents "github.com/arandu-io/hesape/database/events"
)

// newConnectionEstablished builds the event the manager dispatches. It is a
// function rather than a call at the site so the events package's Connection
// interface is satisfied in one place.
func newConnectionEstablished(connection *Connection) *dbevents.ConnectionEstablished {
	return dbevents.NewConnectionEstablished(connection)
}

// DatabaseTransactionRecord answers
// Illuminate\Database\DatabaseTransactionRecord: one open transaction, and the
// callbacks waiting on how it ends.
type DatabaseTransactionRecord struct {
	// Connection is DatabaseTransactionRecord::$connection, the connection's
	// name.
	Connection string

	// Level is DatabaseTransactionRecord::$level.
	Level int

	// Parent is DatabaseTransactionRecord::$parent, the transaction this one
	// was opened inside.
	Parent *DatabaseTransactionRecord

	callbacks            []func()
	callbacksForRollback []func()
}

// NewDatabaseTransactionRecord answers
// DatabaseTransactionRecord::__construct.
func NewDatabaseTransactionRecord(connection string, level int, parent *DatabaseTransactionRecord) *DatabaseTransactionRecord {
	return &DatabaseTransactionRecord{Connection: connection, Level: level, Parent: parent}
}

// AddCallback answers DatabaseTransactionRecord::addCallback.
func (r *DatabaseTransactionRecord) AddCallback(callback func()) {
	r.callbacks = append(r.callbacks, callback)
}

// AddCallbackForRollback answers
// DatabaseTransactionRecord::addCallbackForRollback.
func (r *DatabaseTransactionRecord) AddCallbackForRollback(callback func()) {
	r.callbacksForRollback = append(r.callbacksForRollback, callback)
}

// ExecuteCallbacks answers
// DatabaseTransactionRecord::executeCallbacks.
func (r *DatabaseTransactionRecord) ExecuteCallbacks() {
	for _, callback := range r.callbacks {
		callback()
	}
}

// ExecuteCallbacksForRollback answers
// DatabaseTransactionRecord::executeCallbacksForRollback.
func (r *DatabaseTransactionRecord) ExecuteCallbacksForRollback() {
	for _, callback := range r.callbacksForRollback {
		callback()
	}
}

// GetCallbacks answers DatabaseTransactionRecord::getCallbacks.
func (r *DatabaseTransactionRecord) GetCallbacks() []func() { return r.callbacks }

// GetCallbacksForRollback answers
// DatabaseTransactionRecord::getCallbacksForRollback.
func (r *DatabaseTransactionRecord) GetCallbacksForRollback() []func() { return r.callbacksForRollback }

// DatabaseTransactionsManager answers
// Illuminate\Database\DatabaseTransactionsManager: it remembers which
// transactions are open on which connection, and runs the callbacks that were
// waiting for the outermost one to commit.
//
// It is what makes AfterCommit mean what it says. A queue job dispatched inside
// a transaction that then rolls back is a job about a row that does not exist,
// and it is the classic way a background worker starts failing on data nobody
// can find.
type DatabaseTransactionsManager struct {
	mu sync.Mutex

	// committedTransactions is
	// DatabaseTransactionsManager::$committedTransactions.
	committedTransactions []*DatabaseTransactionRecord

	// pendingTransactions is
	// DatabaseTransactionsManager::$pendingTransactions.
	pendingTransactions []*DatabaseTransactionRecord

	// currentTransaction is
	// DatabaseTransactionsManager::$currentTransaction, keyed by connection.
	currentTransaction map[string]*DatabaseTransactionRecord
}

// NewDatabaseTransactionsManager answers
// DatabaseTransactionsManager::__construct.
func NewDatabaseTransactionsManager() *DatabaseTransactionsManager {
	return &DatabaseTransactionsManager{currentTransaction: map[string]*DatabaseTransactionRecord{}}
}

// Begin answers DatabaseTransactionsManager::begin.
func (m *DatabaseTransactionsManager) Begin(connection string, level int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record := NewDatabaseTransactionRecord(connection, level, m.currentTransaction[connection])
	m.pendingTransactions = append(m.pendingTransactions, record)
	m.currentTransaction[connection] = record
}

// Commit answers DatabaseTransactionsManager::commit.
//
// The callbacks run only when the outermost transaction is the one committing,
// which is the whole guarantee: a savepoint that commits inside a transaction
// that later rolls back has not committed anything.
func (m *DatabaseTransactionsManager) Commit(connection string, levelBeingCommitted, newTransactionLevel int) {
	m.mu.Lock()

	m.stageTransactionsLocked(connection, levelBeingCommitted)

	if current := m.currentTransaction[connection]; current != nil {
		m.currentTransaction[connection] = current.Parent
	}

	if !m.AfterCommitCallbacksShouldBeExecuted(newTransactionLevel) && newTransactionLevel != 0 {
		m.mu.Unlock()
		return
	}

	m.pendingTransactions = rejectRecords(m.pendingTransactions, func(r *DatabaseTransactionRecord) bool {
		return r.Connection == connection && r.Level >= levelBeingCommitted
	})

	var forThisConnection, forOtherConnections []*DatabaseTransactionRecord
	for _, record := range m.committedTransactions {
		if record.Connection == connection {
			forThisConnection = append(forThisConnection, record)
			continue
		}
		forOtherConnections = append(forOtherConnections, record)
	}
	m.committedTransactions = forOtherConnections

	m.mu.Unlock()

	// Outside the lock: a callback that touches the database would otherwise
	// deadlock against the manager it is being called from. The PHP cannot meet
	// this and takes no lock at all.
	for _, record := range forThisConnection {
		record.ExecuteCallbacks()
	}
}

// StageTransactions answers
// DatabaseTransactionsManager::stageTransactions: move the pending records of a
// connection at or above a level into the committed list.
func (m *DatabaseTransactionsManager) StageTransactions(connection string, levelBeingCommitted int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stageTransactionsLocked(connection, levelBeingCommitted)
}

func (m *DatabaseTransactionsManager) stageTransactionsLocked(connection string, levelBeingCommitted int) {
	for _, record := range m.pendingTransactions {
		if record.Connection == connection && record.Level >= levelBeingCommitted {
			m.committedTransactions = append(m.committedTransactions, record)
		}
	}
	m.pendingTransactions = rejectRecords(m.pendingTransactions, func(r *DatabaseTransactionRecord) bool {
		return r.Connection == connection && r.Level >= levelBeingCommitted
	})
}

// Rollback answers DatabaseTransactionsManager::rollback.
func (m *DatabaseTransactionsManager) Rollback(connection string, newTransactionLevel int) {
	m.mu.Lock()

	var toRun []*DatabaseTransactionRecord

	if newTransactionLevel == 0 {
		for record := m.currentTransaction[connection]; record != nil; record = record.Parent {
			toRun = append(toRun, record)
		}
		m.currentTransaction[connection] = nil

		m.pendingTransactions = rejectRecords(m.pendingTransactions, func(r *DatabaseTransactionRecord) bool {
			return r.Connection == connection
		})
		m.committedTransactions = rejectRecords(m.committedTransactions, func(r *DatabaseTransactionRecord) bool {
			return r.Connection == connection
		})
	} else {
		m.pendingTransactions = rejectRecords(m.pendingTransactions, func(r *DatabaseTransactionRecord) bool {
			return r.Connection == connection && r.Level > newTransactionLevel
		})

		for current := m.currentTransaction[connection]; current != nil; current = m.currentTransaction[connection] {
			m.removeCommittedTransactionsThatAreChildrenOf(current)
			toRun = append(toRun, current)
			m.currentTransaction[connection] = current.Parent

			if next := m.currentTransaction[connection]; next == nil || next.Level <= newTransactionLevel {
				break
			}
		}
	}

	m.mu.Unlock()

	for _, record := range toRun {
		record.ExecuteCallbacksForRollback()
	}
}

// removeCommittedTransactionsThatAreChildrenOf answers the protected method of
// the same name. The caller holds the lock.
func (m *DatabaseTransactionsManager) removeCommittedTransactionsThatAreChildrenOf(transaction *DatabaseTransactionRecord) {
	var removed, kept []*DatabaseTransactionRecord
	for _, committed := range m.committedTransactions {
		if committed.Connection == transaction.Connection && committed.Parent == transaction {
			removed = append(removed, committed)
			continue
		}
		kept = append(kept, committed)
	}
	m.committedTransactions = kept

	for _, child := range removed {
		m.removeCommittedTransactionsThatAreChildrenOf(child)
	}
}

// AddCallback answers DatabaseTransactionsManager::addCallback: run the
// callback after the outermost transaction commits, or now when there is none.
func (m *DatabaseTransactionsManager) AddCallback(callback func()) {
	m.mu.Lock()
	current := lastRecord(m.pendingTransactions)
	m.mu.Unlock()

	if current != nil {
		current.AddCallback(callback)
		return
	}
	callback()
}

// AddCallbackForRollback answers
// DatabaseTransactionsManager::addCallbackForRollback.
//
// Outside a transaction it does nothing, which is the PHP's behaviour and is
// right: there is no rollback coming for a statement that already committed.
func (m *DatabaseTransactionsManager) AddCallbackForRollback(callback func()) {
	m.mu.Lock()
	current := lastRecord(m.pendingTransactions)
	m.mu.Unlock()

	if current != nil {
		current.AddCallbackForRollback(callback)
	}
}

// CallbackApplicableTransactions answers
// DatabaseTransactionsManager::callbackApplicableTransactions.
func (m *DatabaseTransactionsManager) CallbackApplicableTransactions() []*DatabaseTransactionRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*DatabaseTransactionRecord(nil), m.pendingTransactions...)
}

// AfterCommitCallbacksShouldBeExecuted answers
// DatabaseTransactionsManager::afterCommitCallbacksShouldBeExecuted.
func (m *DatabaseTransactionsManager) AfterCommitCallbacksShouldBeExecuted(level int) bool {
	return level == 0
}

// GetPendingTransactions answers
// DatabaseTransactionsManager::getPendingTransactions.
func (m *DatabaseTransactionsManager) GetPendingTransactions() []*DatabaseTransactionRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*DatabaseTransactionRecord(nil), m.pendingTransactions...)
}

// GetCommittedTransactions answers
// DatabaseTransactionsManager::getCommittedTransactions.
func (m *DatabaseTransactionsManager) GetCommittedTransactions() []*DatabaseTransactionRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*DatabaseTransactionRecord(nil), m.committedTransactions...)
}

func rejectRecords(records []*DatabaseTransactionRecord, reject func(*DatabaseTransactionRecord) bool) []*DatabaseTransactionRecord {
	var out []*DatabaseTransactionRecord
	for _, record := range records {
		if !reject(record) {
			out = append(out, record)
		}
	}
	return out
}

func lastRecord(records []*DatabaseTransactionRecord) *DatabaseTransactionRecord {
	if len(records) == 0 {
		return nil
	}
	return records[len(records)-1]
}

// DatabaseTransactionsManager is what ManagesTransactions reports to, and the
// compiler says so here rather than at the call site.
var _ concerns.TransactionsManager = (*DatabaseTransactionsManager)(nil)
