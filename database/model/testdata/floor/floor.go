// Package floor names a table and does nothing else with it.
//
// It is what every package of an application that touches a model pays before
// it does anything: a service, a controller, a route file, a test. The only
// line of its own is a call to Table.Name, so what it compiles to is the cost of
// the model layer's exported surface, read through the export data of the
// packages it imports -- and the tests in symbols_test.go compile it and hold
// that cost under a ceiling.
//
// The directory is under testdata, so the go tool never builds it as part of the
// module.
package floor

import "github.com/arandu-io/hesape/database/model"

// TableName names a table and nothing else.
func TableName(t *model.Table) string { return t.Name() }
