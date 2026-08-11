package concerns

import (
	"errors"
	"strings"
	"testing"
)

// fakeDriver records everything ManagesTransactions asks of a connection.
type fakeDriver struct {
	statements  []string
	events      []string
	beginErr    error
	commitErr   error
	concurrency error
	lost        error
	reconnects  int
	savepoints  bool
	inTx        bool
}

func (d *fakeDriver) GetName() string { return "testing" }

func (d *fakeDriver) ExecuteBeginTransactionStatement() error {
	if d.beginErr != nil {
		err := d.beginErr
		d.beginErr = nil
		return err
	}
	d.statements = append(d.statements, "BEGIN")
	d.inTx = true
	return nil
}

func (d *fakeDriver) CommitTransactionStatement() error {
	if d.commitErr != nil {
		err := d.commitErr
		d.commitErr = nil
		return err
	}
	d.statements = append(d.statements, "COMMIT")
	d.inTx = false
	return nil
}

func (d *fakeDriver) RollBackTransactionStatement() (bool, error) {
	if !d.inTx {
		return false, nil
	}
	d.statements = append(d.statements, "ROLLBACK")
	d.inTx = false
	return true, nil
}

func (d *fakeDriver) ExecuteSavepointStatement(sql string) error {
	d.statements = append(d.statements, sql)
	return nil
}

func (d *fakeDriver) SupportsSavepoints() bool { return d.savepoints }

func (d *fakeDriver) CompileSavepoint(name string) string { return "SAVEPOINT " + name }

func (d *fakeDriver) CompileSavepointRollBack(name string) string {
	return "ROLLBACK TO SAVEPOINT " + name
}

func (d *fakeDriver) FireConnectionEvent(event string) { d.events = append(d.events, event) }

func (d *fakeDriver) CausedByConcurrencyError(err error) bool {
	return d.concurrency != nil && errors.Is(err, d.concurrency)
}

func (d *fakeDriver) CausedByLostConnection(err error) bool {
	return d.lost != nil && errors.Is(err, d.lost)
}

func (d *fakeDriver) Reconnect() error { d.reconnects++; return nil }

func (d *fakeDriver) ReconnectIfMissingConnection() error { return nil }

func newTransactions(driver *fakeDriver) *ManagesTransactions {
	m := &ManagesTransactions{}
	m.UseTransactions(driver)
	return m
}

func TestTransactionCommitsOnSuccess(t *testing.T) {
	driver := &fakeDriver{}
	m := newTransactions(driver)

	if err := m.Transaction(func() error { return nil }, 1); err != nil {
		t.Fatalf("Transaction: %v", err)
	}
	if strings.Join(driver.statements, ",") != "BEGIN,COMMIT" {
		t.Fatalf("statements were %v", driver.statements)
	}
	if strings.Join(driver.events, ",") != "beganTransaction,committing,committed" {
		t.Fatalf("events were %v", driver.events)
	}
	if m.TransactionLevel() != 0 {
		t.Fatalf("the level is %d after a commit", m.TransactionLevel())
	}
}

func TestTransactionRollsBackAndReturnsTheError(t *testing.T) {
	driver := &fakeDriver{}
	m := newTransactions(driver)

	boom := errors.New("the invoice was already paid")
	if err := m.Transaction(func() error { return boom }, 1); !errors.Is(err, boom) {
		t.Fatalf("Transaction answered %v, want the callback's own error", err)
	}
	if strings.Join(driver.statements, ",") != "BEGIN,ROLLBACK" {
		t.Fatalf("statements were %v", driver.statements)
	}
	if m.TransactionLevel() != 0 {
		t.Fatalf("the level is %d after a rollback", m.TransactionLevel())
	}
}

func TestTransactionRetriesOnlyConcurrencyErrors(t *testing.T) {
	deadlock := errors.New("deadlock detected")
	driver := &fakeDriver{concurrency: deadlock}
	m := newTransactions(driver)

	attempts := 0
	err := m.Transaction(func() error {
		attempts++
		if attempts < 3 {
			return deadlock
		}
		return nil
	}, 3)
	if err != nil {
		t.Fatalf("Transaction: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("the callback ran %d times, want three", attempts)
	}

	// A plain error is not retried.
	driver = &fakeDriver{concurrency: deadlock}
	m = newTransactions(driver)
	attempts = 0
	other := errors.New("null value in column")
	if err := m.Transaction(func() error { attempts++; return other }, 3); !errors.Is(err, other) {
		t.Fatalf("Transaction answered %v", err)
	}
	if attempts != 1 {
		t.Fatalf("a constraint failure was retried %d times, and retrying it just fails again", attempts)
	}
}

func TestNestedTransactionUsesASavepoint(t *testing.T) {
	driver := &fakeDriver{savepoints: true}
	m := newTransactions(driver)

	if err := m.BeginTransaction(); err != nil {
		t.Fatalf("BeginTransaction: %v", err)
	}
	if err := m.BeginTransaction(); err != nil {
		t.Fatalf("nested BeginTransaction: %v", err)
	}
	if m.TransactionLevel() != 2 {
		t.Fatalf("the level is %d after two begins", m.TransactionLevel())
	}
	if strings.Join(driver.statements, ",") != "BEGIN,SAVEPOINT trans2" {
		t.Fatalf("statements were %v", driver.statements)
	}

	if err := m.RollBack(nil); err != nil {
		t.Fatalf("RollBack: %v", err)
	}
	if m.TransactionLevel() != 1 {
		t.Fatalf("the level is %d after rolling back one", m.TransactionLevel())
	}
	if driver.statements[len(driver.statements)-1] != "ROLLBACK TO SAVEPOINT trans2" {
		t.Fatalf("the rollback ran %q", driver.statements[len(driver.statements)-1])
	}
}

func TestDeadlockInsideANestedTransactionIsNotRetried(t *testing.T) {
	deadlock := errors.New("deadlock detected")
	driver := &fakeDriver{concurrency: deadlock, savepoints: true}
	m := newTransactions(driver)

	if err := m.BeginTransaction(); err != nil {
		t.Fatalf("BeginTransaction: %v", err)
	}

	var deadlockErr *DeadlockError
	err := m.Transaction(func() error { return deadlock }, 3)
	if !errors.As(err, &deadlockErr) {
		t.Fatalf("Transaction answered %v, want a DeadlockError", err)
	}
	if !errors.Is(err, deadlock) {
		t.Fatal("the DeadlockError does not unwrap to the driver error")
	}
}

func TestRollBackToAnImpossibleLevelDoesNothing(t *testing.T) {
	driver := &fakeDriver{}
	m := newTransactions(driver)

	level := 5
	if err := m.RollBack(&level); err != nil {
		t.Fatalf("RollBack: %v", err)
	}
	if len(driver.statements) != 0 {
		t.Fatalf("rolling back to a level that is not open ran %v", driver.statements)
	}
}

func TestBeginReconnectsOnALostConnection(t *testing.T) {
	lost := errors.New("server has gone away")
	driver := &fakeDriver{lost: lost, beginErr: lost}
	m := newTransactions(driver)

	if err := m.BeginTransaction(); err != nil {
		t.Fatalf("BeginTransaction: %v", err)
	}
	if driver.reconnects != 1 {
		t.Fatalf("it reconnected %d times", driver.reconnects)
	}
	if m.TransactionLevel() != 1 {
		t.Fatalf("the level is %d after a reconnect and a retry", m.TransactionLevel())
	}
}

func TestAfterCommitNeedsAManager(t *testing.T) {
	m := newTransactions(&fakeDriver{})

	if err := m.AfterCommit(func() {}); !errors.Is(err, ErrNoTransactionsManager) {
		t.Fatalf("AfterCommit answered %v with no manager set", err)
	}
	if err := m.AfterRollBack(func() {}); !errors.Is(err, ErrNoTransactionsManager) {
		t.Fatalf("AfterRollBack answered %v with no manager set", err)
	}
}
