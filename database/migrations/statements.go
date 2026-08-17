package migrations

import "context"

// UpStatements returns the statements a migration's Up would run, without
// running any of them.
//
// It is what a generator writes to a file and what a test asserts on: both need
// the text a migration would send, and neither has a server. A migration that
// reads before it writes sees no rows here, because Select returns none, so
// what comes back is the path taken over an empty result.
func UpStatements(ctx context.Context, migration Migration) ([]string, error) {
	conn := &recordingConnection{name: migration.GetConnection()}
	if err := migration.Up(ctx, conn); err != nil {
		return nil, err
	}
	return conn.statements, nil
}

// DownStatements returns the statements a migration's Down would run, without
// running any of them. A migration that is not reversible returns none.
func DownStatements(ctx context.Context, migration Migration) ([]string, error) {
	reversible, isReversible := migration.(ReversibleMigration)
	if !isReversible {
		return nil, nil
	}
	conn := &recordingConnection{name: migration.GetConnection()}
	if err := reversible.Down(ctx, conn); err != nil {
		return nil, err
	}
	return conn.statements, nil
}

// recordingConnection is the Connection UpStatements and DownStatements run
// against: it keeps every statement and sends none.
//
// Bindings are dropped rather than recorded. A schema change is DDL, which
// carries none, and a caller that needs them wants the connection's own
// Pretend rather than this.
type recordingConnection struct {
	name       string
	statements []string
}

// GetName returns the connection name the migration asked for.
func (c *recordingConnection) GetName() string { return c.name }

// Statement records query instead of running it.
func (c *recordingConnection) Statement(_ context.Context, query string, _ []any) (bool, error) {
	c.statements = append(c.statements, query)
	return true, nil
}

// Select returns no rows, because there is no server to ask.
func (c *recordingConnection) Select(_ context.Context, _ string, _ []any) ([]map[string]any, error) {
	return nil, nil
}
