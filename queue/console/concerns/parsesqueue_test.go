package concerns_test

import (
	"testing"

	"github.com/arandu-io/hesape/queue/console/concerns"
)

func TestParseQueue(t *testing.T) {
	for _, c := range []struct {
		arg, connection, queue string
	}{
		{"redis:reports", "redis", "reports"},
		{"reports", "", "reports"},
		{"redis:", "redis", "default"},
		{"", "", "default"},
	} {
		connection, queue := concerns.ParsesQueue{}.ParseQueue(c.arg)
		if connection != c.connection || queue != c.queue {
			t.Errorf("ParseQueue(%q) = %q, %q; want %q, %q",
				c.arg, connection, queue, c.connection, c.queue)
		}
	}
}
