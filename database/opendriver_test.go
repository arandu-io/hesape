package database_test

import (
	"database/sql"
	"database/sql/driver"
	"io"
)

// A driver that connects to nothing, so the pool policy and the registry can be
// tested without three servers running. The thing under test is which driver
// name gets resolved and how the pool is configured -- neither needs a network.
func init() { sql.Register("arandu-open-test", noopDriver{}) }

type noopDriver struct{}

func (noopDriver) Open(string) (driver.Conn, error) { return noopConn{}, nil }

type noopConn struct{}

func (noopConn) Prepare(string) (driver.Stmt, error) { return nil, io.EOF }
func (noopConn) Close() error                        { return nil }
func (noopConn) Begin() (driver.Tx, error)           { return nil, io.EOF }
