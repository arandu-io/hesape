package model_test

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
)

// The tests in this file measure what a package that declares models pays to
// compile them, and they measure it the only way it can be measured: by
// compiling such a package and reading the symbols out of what the compiler
// wrote.
//
// The cost is not visible from inside this package. Every method of a generic
// type is compiled again in each package that instantiates it, whether or not
// anything calls the method, and the instantiations are deduplicated only by the
// linker -- after every one of them has been compiled, written to the build
// cache and read back by each package that imports the first. What decides the
// bill is how many distinct instantiations a model drags in, so that is the
// number these tests hold down.

// shapeCeiling is the most generic instantiations, counted as text symbols with
// a shape in their name, that the two-model fixture may compile to.
//
// It is the measured count with a fifth on top, so that an unrelated method
// added to a model does not fail the suite, while a change that starts stamping
// out another type's whole method set per model does.
const shapeCeiling = 3756

var fixture struct {
	once    sync.Once
	symbols []string
	err     error
	output  []byte
}

// fixtureSymbols compiles the relation surface fixture -- two models, a relation
// between them, an eager load, an existence filter and an aggregate -- and
// returns the names of the text symbols in the package it compiles to.
func fixtureSymbols(t *testing.T) []string {
	t.Helper()
	if testing.Short() {
		t.Skip("compiles a fixture with the go tool")
	}

	fixture.once.Do(func() {
		list := exec.Command("go", "list", "-export", "-f", "{{.Export}}", "./testdata/relation_surface")
		list.Env = append(os.Environ(), "GOWORK=off")
		out, err := list.CombinedOutput()
		if err != nil {
			fixture.err, fixture.output = err, out
			return
		}
		export := strings.TrimSpace(string(out))

		nm := exec.Command("go", "tool", "nm", export)
		nm.Env = append(os.Environ(), "GOWORK=off")
		out, err = nm.Output()
		if err != nil {
			fixture.err, fixture.output = err, out
			return
		}

		scanner := bufio.NewScanner(bytes.NewReader(out))
		scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for scanner.Scan() {
			// address, kind, name -- and the name of an instantiation carries
			// spaces, so it is everything after the second field.
			parts := strings.SplitN(strings.TrimSpace(scanner.Text()), " ", 3)
			if len(parts) == 3 && parts[1] == "T" {
				fixture.symbols = append(fixture.symbols, parts[2])
			}
		}
		fixture.err = scanner.Err()
	})

	if fixture.err != nil {
		t.Fatalf("the fixture did not compile to a package whose symbols could be read: %v\n%s", fixture.err, fixture.output)
	}
	if len(fixture.symbols) == 0 {
		t.Fatal("the fixture compiled to no text symbols at all, so nothing below would measure anything")
	}
	return fixture.symbols
}

// TestTheCollectionsPackageIsNotCompiledOncePerModel: a model once handed its
// rows to collections.Flip, whose element is constrained to comparable. A
// pointer under that constraint is compiled for the type it points at instead
// of once for every pointer, so each model compiled the whole
// collections.Collection method set, and the lazy collection behind it, again
// in every package that named it, for a method nobody called.
func TestTheCollectionsPackageIsNotCompiledOncePerModel(t *testing.T) {
	var offending []string
	for _, symbol := range fixtureSymbols(t) {
		if !strings.Contains(symbol, "hesape/collections.") {
			continue
		}
		if strings.Contains(symbol, "database/model.Model[go.shape") || strings.Contains(symbol, "*go.shape.struct") {
			offending = append(offending, symbol)
		}
	}
	if len(offending) > 0 {
		t.Fatalf("%d collections symbols are compiled once per model type; the first is\n%s", len(offending), offending[0])
	}
}

// TestTwoModelsCompileUnderTheGenericCeiling holds the number of generic
// instantiations the fixture compiles to under shapeCeiling, which is what keeps
// the cost of declaring a model proportional to what is specific to it.
func TestTwoModelsCompileUnderTheGenericCeiling(t *testing.T) {
	shapes := 0
	for _, symbol := range fixtureSymbols(t) {
		if strings.Contains(symbol, "go.shape") {
			shapes++
		}
	}
	if shapes > shapeCeiling {
		t.Fatalf("the two-model fixture compiles %d generic instantiations, over the ceiling of %d: something now compiles a method set per model type", shapes, shapeCeiling)
	}
}
