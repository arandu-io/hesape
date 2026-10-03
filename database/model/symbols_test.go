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
// cache and read back by each package that imports the first. A model layer
// with a type parameter cost every package that named a model a copy of the
// layer, so the number these tests hold down is the number of instantiations a
// package of models compiles: none.

// shapeCeiling is the most text symbols with a shape in their name the
// generated fixture may compile to.
//
// None of them is an instantiation. They are the wrapper methods the compiler
// emits for an interface type that appears as a type argument in a signature the
// package can see -- reflect.Type and reflect.Value through the standard
// library, iter.Seq2 over the relation and entity interfaces through the model
// -- one small shared function per interface method, deduplicated by the
// linker. A program that imports reflect and nothing else compiles 57 of them.
// The ceiling is the measured count with a margin, so that a change that starts
// stamping out a method set per entity fails here even before it fails the
// instantiation test.
const shapeCeiling = 110

// compiled is one fixture's text symbols, compiled once per test binary.
type compiled struct {
	once    sync.Once
	symbols []string
	err     error
	output  []byte
}

var fixtures = map[string]*compiled{
	"./testdata/generated":        {},
	"./testdata/relation_surface": {},
}

// fixtureSymbols compiles a fixture and returns the names of the text symbols
// in the package it compiles to.
func fixtureSymbols(t *testing.T, path string) []string {
	t.Helper()
	if testing.Short() {
		t.Skip("compiles a fixture with the go tool")
	}

	fixture := fixtures[path]
	fixture.once.Do(func() {
		list := exec.Command("go", "list", "-export", "-f", "{{.Export}}", path)
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
		t.Fatalf("%s did not compile to a package whose symbols could be read: %v\n%s", path, fixture.err, fixture.output)
	}
	if len(fixture.symbols) == 0 {
		t.Fatalf("%s compiled to no text symbols at all, so nothing below would measure anything", path)
	}
	return fixture.symbols
}

// TestGeneratedModelsCompileToNoModelLayerInstantiation is the measure the
// model layer is designed around: two entities, their generated query types,
// collections, constructors and factory, and a service over them, compile to no
// instantiation of a generic function or method at all -- of the model, the
// pagination, the factories, the collections, or anything else.
//
// An instantiation is named after the generic it instantiates with the shape in
// brackets: model.(*Builder[go.shape.struct {...}]).Where,
// pagination.Paginate[go.shape.*uint8]. One of those in a package of models is
// the cost this layer exists not to pay, compiled again in every package that
// names a model.
func TestGeneratedModelsCompileToNoModelLayerInstantiation(t *testing.T) {
	for _, path := range []string{"./testdata/generated", "./testdata/relation_surface"} {
		t.Run(path, func(t *testing.T) {
			var instantiations, modelLayer []string
			for _, symbol := range fixtureSymbols(t, path) {
				if !strings.Contains(symbol, "[go.shape") {
					continue
				}
				instantiations = append(instantiations, symbol)
				for _, pkg := range []string{"hesape/database/model", "hesape/pagination", "hesape/collections"} {
					if strings.Contains(symbol, pkg) {
						modelLayer = append(modelLayer, symbol)
						break
					}
				}
			}
			if len(modelLayer) > 0 {
				t.Fatalf("%d instantiations of the model layer compile into a package of models; the first is\n%s", len(modelLayer), modelLayer[0])
			}
			if len(instantiations) > 0 {
				t.Fatalf("%d generic instantiations compile into a package of models, and generated code instantiates nothing; the first is\n%s", len(instantiations), instantiations[0])
			}
		})
	}
}

// TestNoShapeIsOverAnEntity: a shape built from one of the application's own
// types is a generic compiled for that type, whatever it is named after. The
// interface wrappers the fixture does compile are over the standard library's
// and the model's interfaces, never over an entity.
func TestNoShapeIsOverAnEntity(t *testing.T) {
	for _, symbol := range fixtureSymbols(t, "./testdata/generated") {
		if strings.Contains(symbol, "go.shape") && strings.Contains(symbol, "main.") {
			t.Fatalf("a shape over an entity compiled into the package of models:\n%s", symbol)
		}
	}
}

// TestTheGeneratedModelsCompileUnderTheShapeCeiling holds the interface
// wrappers the fixture compiles under shapeCeiling. See the constant for what
// they are and why there are any.
func TestTheGeneratedModelsCompileUnderTheShapeCeiling(t *testing.T) {
	shapes := 0
	for _, symbol := range fixtureSymbols(t, "./testdata/generated") {
		if strings.Contains(symbol, "go.shape") {
			shapes++
		}
	}
	if shapes > shapeCeiling {
		t.Fatalf("the two-entity fixture compiles %d shaped symbols, over the ceiling of %d: something now compiles a method set per entity", shapes, shapeCeiling)
	}
}
