package exception_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestTheExceptionPackageDoesNotImportTheDatabaseLayer: answering a request
// with a status does not need the query builder, the connections and the
// models behind them. The database errors state their own status, and this
// package reads it by method set; an import added back to recognise one
// sentinel would carry the whole layer into everything that renders an error.
func TestTheExceptionPackageDoesNotImportTheDatabaseLayer(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", ".")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if dep == "github.com/arandu-io/hesape/database" {
			t.Fatal("hesape/exception depends on hesape/database again")
		}
	}
}
