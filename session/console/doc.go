// Package console provides the code generator for the session table.
//
// [SessionTableCommand] writes the migration that creates the table
// [session.DatabaseSessionHandler] reads. It writes a file and runs nothing:
// a migration that ran itself would be N replicas racing each other at
// boot, and emitting a file is what lets somebody read it before it reaches
// production.
//
// Deprecated: the sessions table is read only by the deprecated
// session.DatabaseSessionHandler, and [session.RecordStore], the one session
// path, keeps no table. This package is removed in a later minor release.
package console
