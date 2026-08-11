package processors

import "github.com/arandu-io/hesape/database/query"

// MariaDBProcessor answers
// Illuminate\Database\Query\Processors\MariaDbProcessor. The PHP spells it
// MariaDb; Go initialisms are upper case throughout.
//
// It adds nothing to MySQL's, in Illuminate and here: MariaDB differs from
// MySQL in what it accepts, not in what it answers. It exists so that a
// connection wires a grammar and a processor by the same name, and so that the
// place to put the first difference already exists.
type MariaDBProcessor struct {
	MySQLProcessor
}

var _ query.Processor = (*MariaDBProcessor)(nil)

// NewMariaDBProcessor answers `new MariaDbProcessor`.
func NewMariaDBProcessor() *MariaDBProcessor { return &MariaDBProcessor{} }
