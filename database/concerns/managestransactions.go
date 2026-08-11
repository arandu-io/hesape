package concerns

import (
	"errors"
	"fmt"
	"strconv"
)

// TransactionDriver is what ManagesTransactions drives.
//
// In PHP the trait is used by Connection and reaches straight for
// $this->getPdo(), $this->queryGrammar and $this->causedByLostConnection().
// Those calls are written out here as an interface, which is the Go spelling of
// "this trait may only be used by a class that has these".
//
// A Connection satisfies it; nothing else is meant to.
type TransactionDriver interface {
	// GetName answers Connection::getName. The transactions manager keys its
	// bookkeeping by it.
	GetName() string

	// ExecuteBeginTransactionStatement answers the protected
	// Connection::executeBeginTransactionStatement.
	ExecuteBeginTransactionStatement() error

	// CommitTransactionStatement is $this->getPdo()->commit().
	CommitTransactionStatement() error

	// RollBackTransactionStatement is $this->getPdo()->rollBack(), and answers
	// false when there was no transaction open -- the PDO::inTransaction() the
	// PHP checks before rolling back.
	RollBackTransactionStatement() (bool, error)

	// ExecuteSavepointStatement is the $this->getPdo()->exec() the savepoint
	// paths run.
	ExecuteSavepointStatement(sql string) error

	// SupportsSavepoints answers $this->queryGrammar->supportsSavepoints().
	SupportsSavepoints() bool

	// CompileSavepoint answers $this->queryGrammar->compileSavepoint().
	CompileSavepoint(name string) string

	// CompileSavepointRollBack answers
	// $this->queryGrammar->compileSavepointRollBack().
	CompileSavepointRollBack(name string) string

	// FireConnectionEvent answers the protected
	// Connection::fireConnectionEvent, with the PHP's own four names:
	// "beganTransaction", "committing", "committed", "rollingBack".
	FireConnectionEvent(event string)

	// CausedByConcurrencyError answers the DetectsConcurrencyErrors trait.
	CausedByConcurrencyError(err error) bool

	// CausedByLostConnection answers the DetectsLostConnections trait.
	CausedByLostConnection(err error) bool

	// Reconnect answers Connection::reconnect.
	Reconnect() error

	// ReconnectIfMissingConnection answers
	// Connection::reconnectIfMissingConnection.
	ReconnectIfMissingConnection() error
}

// TransactionsManager is what ManagesTransactions reports to.
//
// It answers Illuminate\Database\DatabaseTransactionsManager, narrowed to the
// five methods the trait calls. It is an interface here and a class in the
// database package for the usual reason: that package imports this one.
type TransactionsManager interface {
	// Begin answers DatabaseTransactionsManager::begin.
	Begin(connection string, level int)

	// Commit answers DatabaseTransactionsManager::commit.
	Commit(connection string, levelBeingCommitted, newTransactionLevel int)

	// Rollback answers DatabaseTransactionsManager::rollback.
	Rollback(connection string, newTransactionLevel int)

	// AddCallback answers DatabaseTransactionsManager::addCallback.
	AddCallback(callback func())

	// AddCallbackForRollback answers
	// DatabaseTransactionsManager::addCallbackForRollback.
	AddCallbackForRollback(callback func())
}

// DeadlockError answers Illuminate\Database\DeadlockException: a nested
// transaction hit a concurrency error, and the whole transaction is gone.
//
// It is declared here because ManagesTransactions is what raises it and the
// database package imports this one, so declaring it there would close the
// cycle. database.DeadlockException is an alias of this type -- one type, the
// two names the two languages give it.
//
// It is not retried, and that is the point of having its own type: on a
// deadlock the engine has already rolled the whole transaction back, so
// re-running the statement runs it outside the transaction the caller thinks
// it is in.
type DeadlockError struct {
	// Err is the driver error the engine reported.
	Err error
}

// NewDeadlockError answers `new DeadlockException($e->getMessage(), ...)`.
func NewDeadlockError(err error) *DeadlockError { return &DeadlockError{Err: err} }

// Error carries the driver's own message, as the PHP passes $e->getMessage()
// through.
func (e *DeadlockError) Error() string {
	if e.Err == nil {
		return "deadlock"
	}
	return e.Err.Error()
}

// Unwrap makes errors.Is and errors.As reach the driver error, which is what
// the PHP's $previous is for.
func (e *DeadlockError) Unwrap() error { return e.Err }

// ErrNoTransactionsManager answers the RuntimeException AfterCommit and
// AfterRollBack raise when no manager was set.
var ErrNoTransactionsManager = errors.New("Transactions Manager has not been set.")

// ManagesTransactions answers Illuminate\Database\Concerns\ManagesTransactions.
//
// In PHP it is a trait Connection uses; here it is a struct Connection embeds,
// which is what "extends, for the part that is code" means in Go. The state it
// owns -- the nesting level, the manager, the before-start hooks -- is the
// state the trait's properties declare on Connection.
//
// # Savepoints exist here and are not the framework's transaction story
//
// The nested transaction below opens a savepoint, because that is what the
// Illuminate connection does and this package is that connection. It is not the
// path an Arandu application takes: database.Transaction joins the outer
// transaction rather than nesting, on the grounds that partial rollback is a
// second failure mode for one operation. Both spellings exist because both
// exist in Laravel; the generated repository uses the first.
type ManagesTransactions struct {
	// driver is the connection this trait was used by.
	driver TransactionDriver

	// transactions is Connection::$transactions: the nesting level.
	transactions int

	// transactionsManager is Connection::$transactionsManager.
	transactionsManager TransactionsManager

	// beforeStartingTransaction is Connection::$beforeStartingTransaction.
	beforeStartingTransaction []func()
}

// UseTransactions wires the trait to the connection that used it.
//
// PHP needs no such call: `use ManagesTransactions` inside the class body makes
// $this the receiver. A Go struct has to be told, so the connection calls this
// once from its constructor.
func (m *ManagesTransactions) UseTransactions(driver TransactionDriver) {
	m.driver = driver
}

// Transaction answers ManagesTransactions::transaction: it runs the callback
// inside a transaction, committing when it returns nil and rolling back when it
// returns an error.
//
// attempts is the PHP's retry count, and it retries only what is worth
// retrying: a concurrency error at the outermost level. Anything else is
// returned on the first try, because re-running a statement that failed on a
// constraint just fails again, slower.
//
// The PHP returns whatever the callback returned. A Go method cannot be
// generic, so the callback returns only an error and carries its result out
// through the closure -- which is the shape database.Transaction already has.
func (m *ManagesTransactions) Transaction(callback func() error, attempts int) error {
	if attempts < 1 {
		attempts = 1
	}

	for currentAttempt := 1; currentAttempt <= attempts; currentAttempt++ {
		if err := m.BeginTransaction(); err != nil {
			return err
		}

		// The callback runs inside what amounts to the PHP's try; an error is
		// its catch, and a retryable one continues the loop.
		if err := callback(); err != nil {
			if retry, handled := m.handleTransactionException(err, currentAttempt, attempts); !retry {
				return handled
			}
			continue
		}

		levelBeingCommitted := m.transactions

		if err := m.commitCurrent(); err != nil {
			if retry, handled := m.handleCommitTransactionException(err, currentAttempt, attempts); !retry {
				return handled
			}
			continue
		}

		if m.transactionsManager != nil {
			m.transactionsManager.Commit(m.driver.GetName(), levelBeingCommitted, m.transactions)
		}

		m.driver.FireConnectionEvent("committed")

		return nil
	}

	// Every attempt asked for a retry and the loop ran out. The PHP falls off
	// the end of the method here and returns null, which is a bug it has never
	// been bitten by because the last attempt always rethrows. Saying so is
	// cheaper than a nil nobody can explain.
	return fmt.Errorf("the transaction was retried %d times and every attempt hit a concurrency error", attempts)
}

// commitCurrent is the commit half of the transaction loop: the PDO commit at
// level one, and the level decrement at every level.
func (m *ManagesTransactions) commitCurrent() error {
	if m.transactions == 1 {
		m.driver.FireConnectionEvent("committing")
		if err := m.driver.CommitTransactionStatement(); err != nil {
			return err
		}
	}
	m.transactions = max(0, m.transactions-1)
	return nil
}

// handleTransactionException answers the protected
// ManagesTransactions::handleTransactionException.
//
// It answers (retry, err): retry true means the loop tries again, and err is
// what to return when it does not.
func (m *ManagesTransactions) handleTransactionException(err error, currentAttempt, maxAttempts int) (bool, error) {
	// On a deadlock the engine has rolled the entire transaction back, so a
	// nested level cannot be retried in place: it has to reach the caller.
	if m.driver.CausedByConcurrencyError(err) && m.transactions > 1 {
		m.transactions--

		if m.transactionsManager != nil {
			m.transactionsManager.Rollback(m.driver.GetName(), m.transactions)
		}

		return false, NewDeadlockError(err)
	}

	if rollbackErr := m.RollBack(nil); rollbackErr != nil {
		return false, rollbackErr
	}

	if m.driver.CausedByConcurrencyError(err) && currentAttempt < maxAttempts {
		return true, nil
	}

	return false, err
}

// handleCommitTransactionException answers the protected
// ManagesTransactions::handleCommitTransactionException.
func (m *ManagesTransactions) handleCommitTransactionException(err error, currentAttempt, maxAttempts int) (bool, error) {
	m.transactions = max(0, m.transactions-1)

	if m.driver.CausedByConcurrencyError(err) && currentAttempt < maxAttempts {
		return true, nil
	}

	if m.driver.CausedByLostConnection(err) {
		// The connection is gone, so the level it was counting is gone with it.
		m.transactions = 0
	}

	return false, err
}

// BeginTransaction answers ManagesTransactions::beginTransaction.
func (m *ManagesTransactions) BeginTransaction() error {
	for _, callback := range m.beforeStartingTransaction {
		callback()
	}

	if err := m.createTransaction(); err != nil {
		return err
	}

	m.transactions++

	if m.transactionsManager != nil {
		m.transactionsManager.Begin(m.driver.GetName(), m.transactions)
	}

	m.driver.FireConnectionEvent("beganTransaction")

	return nil
}

// createTransaction answers the protected ManagesTransactions::createTransaction:
// a real transaction at level zero, a savepoint above it.
func (m *ManagesTransactions) createTransaction() error {
	if m.transactions == 0 {
		if err := m.driver.ReconnectIfMissingConnection(); err != nil {
			return err
		}
		if err := m.driver.ExecuteBeginTransactionStatement(); err != nil {
			return m.handleBeginTransactionException(err)
		}
		return nil
	}

	if m.transactions >= 1 && m.driver.SupportsSavepoints() {
		return m.createSavepoint()
	}
	return nil
}

// createSavepoint answers the protected ManagesTransactions::createSavepoint.
//
// The name is "trans" plus the level about to be entered, which is what makes
// performRollBack able to name the one it wants.
func (m *ManagesTransactions) createSavepoint() error {
	return m.driver.ExecuteSavepointStatement(
		m.driver.CompileSavepoint("trans" + strconv.Itoa(m.transactions+1)),
	)
}

// handleBeginTransactionException answers the protected
// ManagesTransactions::handleBeginTransactionException: a lost connection is
// reconnected and the BEGIN reissued once, anything else is returned.
func (m *ManagesTransactions) handleBeginTransactionException(err error) error {
	if m.driver.CausedByLostConnection(err) {
		if reconnectErr := m.driver.Reconnect(); reconnectErr != nil {
			return reconnectErr
		}
		return m.driver.ExecuteBeginTransactionStatement()
	}
	return err
}

// Commit answers ManagesTransactions::commit: the standalone commit, for a
// transaction somebody began by hand.
func (m *ManagesTransactions) Commit() error {
	if m.TransactionLevel() == 1 {
		m.driver.FireConnectionEvent("committing")
		if err := m.driver.CommitTransactionStatement(); err != nil {
			return err
		}
	}

	levelBeingCommitted := m.transactions
	m.transactions = max(0, m.transactions-1)

	if m.transactionsManager != nil {
		m.transactionsManager.Commit(m.driver.GetName(), levelBeingCommitted, m.transactions)
	}

	m.driver.FireConnectionEvent("committed")

	return nil
}

// RollBack answers ManagesTransactions::rollBack.
//
// toLevel nil is the PHP's null default: roll back one level. A level outside
// the open range is ignored rather than refused, which is the PHP's own early
// return -- rolling back to a level that does not exist is a no-op, not a
// failure.
//
// The PHP spells it rollBack, with the capital B, and so does this.
func (m *ManagesTransactions) RollBack(toLevel *int) error {
	level := m.transactions - 1
	if toLevel != nil {
		level = *toLevel
	}

	if level < 0 || level >= m.transactions {
		return nil
	}

	if err := m.performRollBack(level); err != nil {
		return m.handleRollBackException(err)
	}

	m.transactions = level

	if m.transactionsManager != nil {
		m.transactionsManager.Rollback(m.driver.GetName(), m.transactions)
	}

	m.driver.FireConnectionEvent("rollingBack")

	return nil
}

// performRollBack answers the protected ManagesTransactions::performRollBack.
func (m *ManagesTransactions) performRollBack(toLevel int) error {
	if toLevel == 0 {
		_, err := m.driver.RollBackTransactionStatement()
		return err
	}
	if m.driver.SupportsSavepoints() {
		return m.driver.ExecuteSavepointStatement(
			m.driver.CompileSavepointRollBack("trans" + strconv.Itoa(toLevel+1)),
		)
	}
	return nil
}

// handleRollBackException answers the protected
// ManagesTransactions::handleRollBackException: a rollback that failed because
// the connection went away leaves nothing to roll back to.
func (m *ManagesTransactions) handleRollBackException(err error) error {
	if m.driver.CausedByLostConnection(err) {
		m.transactions = 0

		if m.transactionsManager != nil {
			m.transactionsManager.Rollback(m.driver.GetName(), m.transactions)
		}
	}
	return err
}

// TransactionLevel answers ManagesTransactions::transactionLevel: how many
// transactions are open, counting savepoints.
func (m *ManagesTransactions) TransactionLevel() int { return m.transactions }

// AfterCommit answers ManagesTransactions::afterCommit: run the callback once
// the outermost transaction commits, or now when there is none open.
func (m *ManagesTransactions) AfterCommit(callback func()) error {
	if m.transactionsManager != nil {
		m.transactionsManager.AddCallback(callback)
		return nil
	}
	return ErrNoTransactionsManager
}

// AfterRollBack answers ManagesTransactions::afterRollBack.
func (m *ManagesTransactions) AfterRollBack(callback func()) error {
	if m.transactionsManager != nil {
		m.transactionsManager.AddCallbackForRollback(callback)
		return nil
	}
	return ErrNoTransactionsManager
}

// BeforeStartingTransaction answers Connection::beforeStartingTransaction: a
// hook to run just before a transaction opens.
//
// It lives on the trait rather than on the connection because the slice it
// appends to is read by BeginTransaction, which is here.
func (m *ManagesTransactions) BeforeStartingTransaction(callback func()) {
	m.beforeStartingTransaction = append(m.beforeStartingTransaction, callback)
}

// SetTransactionManager answers Connection::setTransactionManager.
func (m *ManagesTransactions) SetTransactionManager(manager TransactionsManager) {
	m.transactionsManager = manager
}

// UnsetTransactionManager answers Connection::unsetTransactionManager.
func (m *ManagesTransactions) UnsetTransactionManager() { m.transactionsManager = nil }

// ResetTransactionLevel is Connection::setPdo's `$this->transactions = 0`.
//
// Replacing the handle throws away whatever transaction was open on the old
// one, and a level that outlived its connection is a rollback aimed at nothing.
func (m *ManagesTransactions) ResetTransactionLevel() { m.transactions = 0 }
