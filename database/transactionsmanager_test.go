package database

import (
	"strings"
	"testing"
)

func TestAfterCommitRunsOnlyWhenTheOutermostTransactionCommits(t *testing.T) {
	manager := NewDatabaseTransactionsManager()

	var ran []string

	manager.Begin("primary", 1)
	manager.AddCallback(func() { ran = append(ran, "outer") })

	manager.Begin("primary", 2)
	manager.AddCallback(func() { ran = append(ran, "inner") })

	// The savepoint commits: nothing has actually reached the database yet.
	manager.Commit("primary", 2, 1)
	if len(ran) != 0 {
		t.Fatalf("a callback ran on a savepoint commit: %v", ran)
	}

	manager.Commit("primary", 1, 0)

	// Innermost first, which is not the obvious order: the savepoint commit
	// stages its record into the committed list before the outer one gets
	// there, and the list is run in the order it was built.
	if strings.Join(ran, ",") != "inner,outer" {
		t.Fatalf("the callbacks ran as %v", ran)
	}
}

func TestAfterCommitRunsImmediatelyOutsideATransaction(t *testing.T) {
	manager := NewDatabaseTransactionsManager()

	ran := false
	manager.AddCallback(func() { ran = true })

	if !ran {
		t.Fatal("a callback added outside a transaction did not run, so it would never run")
	}
}

func TestRollbackDropsTheCallbacksAndRunsTheRollbackOnes(t *testing.T) {
	manager := NewDatabaseTransactionsManager()

	var ran []string

	manager.Begin("primary", 1)
	manager.AddCallback(func() { ran = append(ran, "commit") })
	manager.AddCallbackForRollback(func() { ran = append(ran, "rollback") })

	manager.Rollback("primary", 0)

	if strings.Join(ran, ",") != "rollback" {
		t.Fatalf("the callbacks ran as %v; a job dispatched inside a rolled-back transaction is a job about a row that does not exist", ran)
	}
	if len(manager.GetPendingTransactions()) != 0 {
		t.Fatal("the rolled-back transaction is still pending")
	}
}

func TestOneConnectionsCommitDoesNotRunAnothersCallbacks(t *testing.T) {
	manager := NewDatabaseTransactionsManager()

	var ran []string

	manager.Begin("primary", 1)
	manager.AddCallback(func() { ran = append(ran, "primary") })

	manager.Begin("analytics", 1)
	manager.AddCallback(func() { ran = append(ran, "analytics") })

	manager.Commit("analytics", 1, 0)

	if strings.Join(ran, ",") != "analytics" {
		t.Fatalf("committing analytics ran %v", ran)
	}
}

func TestTransactionRecordCarriesItsParent(t *testing.T) {
	outer := NewDatabaseTransactionRecord("primary", 1, nil)
	inner := NewDatabaseTransactionRecord("primary", 2, outer)

	if inner.Parent != outer {
		t.Fatal("the nested record does not point at the one it was opened inside")
	}

	ran := 0
	inner.AddCallback(func() { ran++ })
	inner.AddCallbackForRollback(func() { ran += 10 })

	if len(inner.GetCallbacks()) != 1 || len(inner.GetCallbacksForRollback()) != 1 {
		t.Fatal("the record did not keep its callbacks")
	}

	inner.ExecuteCallbacks()
	inner.ExecuteCallbacksForRollback()
	if ran != 11 {
		t.Fatalf("the callbacks ran to %d, want 11", ran)
	}
}

func TestConnectionResolver(t *testing.T) {
	primary := NewConnection(nil, "arandu", "", map[string]any{"name": "primary"})

	resolver := NewConnectionResolver(map[string]ConnectionInterface{"primary": primary})
	resolver.SetDefaultConnection("primary")

	if !resolver.HasConnection("primary") {
		t.Fatal("HasConnection said no about a connection it was given")
	}

	got, err := resolver.Connection("")
	if err != nil {
		t.Fatalf("Connection: %v", err)
	}
	if got != ConnectionInterface(primary) {
		t.Fatal("an empty name did not answer the default connection")
	}

	if _, err := resolver.Connection("nope"); err == nil {
		t.Fatal("an unknown connection answered no error, so the nil would fail four frames away")
	}
}

func TestDatabaseManagerMakesAConnectionOnceAndKeepsIt(t *testing.T) {
	config := MapConfiguration{
		"database.default": "sqlite",
		"database.connections": map[string]any{
			"sqlite": map[string]any{"driver": "sqlite", "database": ":memory:"},
		},
	}

	manager := NewDatabaseManager(config, NewConnectionFactory())

	// No connector is linked in this test binary, so the factory refuses --
	// and the message is the one that names the import to add.
	_, err := manager.Connection("")
	if err == nil {
		t.Skip("a connector is linked into this binary, so the refusal cannot be observed")
	}
	if !strings.Contains(err.Error(), "connectors/sqlite") {
		t.Fatalf("the error does not name the module to add:\n%v", err)
	}
}

func TestCalculateDynamicConnectionNameIsStable(t *testing.T) {
	config := map[string]any{"driver": "pgsql", "database": "arandu", "host": "127.0.0.1"}

	first := CalculateDynamicConnectionName(config)
	second := CalculateDynamicConnectionName(config)

	if first != second {
		t.Fatalf("the same configuration got two names, %q and %q -- map order is not an order", first, second)
	}
	if !strings.HasPrefix(first, "dynamic_") {
		t.Fatalf("the name is %q", first)
	}
}

func TestParseConnectionNameReadsTheReadWriteSuffix(t *testing.T) {
	for name, want := range map[string][2]string{
		"primary":        {"primary", ""},
		"primary::read":  {"primary", "read"},
		"primary::write": {"primary", "write"},
	} {
		database, typ := parseConnectionName(name)
		if database != want[0] || typ != want[1] {
			t.Fatalf("parseConnectionName(%q) = (%q, %q), want %v", name, database, typ, want)
		}
	}
}

func TestSupportedDriversLeavesSQLServerOut(t *testing.T) {
	for _, driver := range SupportedDrivers() {
		if driver == "sqlsrv" {
			t.Fatal("SQL Server is on the supported list, and RULE 11 names three engines")
		}
	}
	if len(SupportedDrivers()) != 4 {
		t.Fatalf("SupportedDrivers answered %v", SupportedDrivers())
	}
}

func TestCalledSeedersAreRememberedOnce(t *testing.T) {
	t.Cleanup(ForgetCalledSeeders)
	ForgetCalledSeeders()

	if len(CalledSeeders()) != 0 {
		t.Fatal("the called list is not empty at the start of the test")
	}
}
