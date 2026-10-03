package model_test

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"strconv"
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
//
// The same holds, at a smaller scale, for what the layer's exported types
// mention. A package that names a table compiles against the export data of
// everything the table's fields and methods reach, and an instantiation over an
// interface anywhere in that reach -- iter.Seq2 over a model, an iterator over
// reflect.Type -- is a wrapper per method of the interface, compiled again in
// that package. The floor fixture is the package with nothing else in it.

// shapeCeiling is the most text symbols with a shape in their name a fixture
// may compile to.
//
// None of them is an instantiation. They are the wrapper methods the compiler
// emits for an interface type that appears as a type argument in a signature the
// package can see, one small shared function per interface method, deduplicated
// by the linker. Two are left, each over a one-method interface: base, for the
// iter.Seq2 over Entity that Builder.Cursor, Lazy and LazyById return, and Error,
// for the iter.Seq2 over error that query.Builder's streams return. The ceiling
// is that count and a fifth more, rounded up, so that an interface with a method
// set coming back into reach -- reflect.Type was 41 wrappers, a relation's model
// 33 -- fails here even before anything instantiates the layer.
const shapeCeiling = 3

// floorExportCeiling and floorArchiveCeiling are the most bytes the floor
// fixture may compile to: its export data, which every package importing it
// reads, and the whole archive the compiler writes, export data and object code
// together.
//
// Each is the measured size and a fifth more. The export data is almost all of
// it, and it is the model layer's exported surface as seen from outside: every
// type a Table reaches, written into the package that names one.
const (
	floorExportCeiling  = 156_230
	floorArchiveCeiling = 175_408
)

// compiled is one fixture as the compiler wrote it, compiled once per test
// binary: the archive go list names, and its text symbols.
type compiled struct {
	once    sync.Once
	export  string
	symbols []string
	err     error
	output  []byte
}

var fixtures = map[string]*compiled{
	"./testdata/floor":            {},
	"./testdata/generated":        {},
	"./testdata/relation_surface": {},
}

// compile compiles a fixture with the go tool, once, and returns what it wrote.
func compile(t *testing.T, path string) *compiled {
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
		fixture.export = strings.TrimSpace(string(out))

		nm := exec.Command("go", "tool", "nm", fixture.export)
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
	return fixture
}

// fixtureSymbols compiles a fixture and returns the names of the text symbols
// in the package it compiles to.
func fixtureSymbols(t *testing.T, path string) []string {
	t.Helper()
	return compile(t, path).symbols
}

// fixtureSizes compiles a fixture and returns the size of the archive the
// compiler wrote for it, and of the export data inside: the __.PKGDEF member,
// which is what a package importing the fixture reads.
func fixtureSizes(t *testing.T, path string) (archive, export int64) {
	t.Helper()
	file := compile(t, path).export
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading the archive of %s: %v", path, err)
	}

	// A Unix archive: the magic string, then a 60-byte header per member --
	// the name in the first 16 bytes, the size in decimal at 48 -- and the
	// member, padded to an even length.
	const magic = "!<arch>\n"
	if !bytes.HasPrefix(data, []byte(magic)) {
		t.Fatalf("%s compiled to %s, which is not an archive", path, file)
	}
	for at := len(magic); at+60 <= len(data); {
		header := data[at : at+60]
		size, err := strconv.ParseInt(strings.TrimSpace(string(header[48:58])), 10, 64)
		if err != nil {
			t.Fatalf("a member of the archive of %s has no readable size: %v", path, err)
		}
		if strings.TrimSpace(string(header[:16])) == "__.PKGDEF" {
			return int64(len(data)), size
		}
		at += 60 + int(size) + int(size%2)
	}
	t.Fatalf("the archive of %s holds no export data", path)
	return 0, 0
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
		t.Fatalf("the two-entity fixture compiles %d shaped symbols, over the ceiling of %d: something now compiles a method set per entity, or an interface with a method set is back in the reach of the model's exported types", shapes, shapeCeiling)
	}
}

// TestAPackageThatNamesATableCompilesUnderTheShapeCeiling: the floor fixture
// calls Table.Name and nothing else, so every shaped symbol it compiles comes
// from what the model layer's exported types reach. It was 92 once, in every
// package that named a table: 57 for the reflect.Type a field of Table held --
// every method of reflect.Type and of reflect.Value -- and 33 for the iter.Seq2
// over the relation seam's model that the relation builder's Cursor returned.
func TestAPackageThatNamesATableCompilesUnderTheShapeCeiling(t *testing.T) {
	var shaped []string
	for _, symbol := range fixtureSymbols(t, "./testdata/floor") {
		if strings.Contains(symbol, "go.shape") {
			shaped = append(shaped, symbol)
		}
	}
	if len(shaped) > shapeCeiling {
		t.Fatalf("a package that only names a table compiles %d shaped symbols, over the ceiling of %d: an interface with a method set is back in the reach of the model's exported types. They are\n%s", len(shaped), shapeCeiling, strings.Join(shaped, "\n"))
	}
}

// TestAPackageThatNamesATableCompilesUnderTheExportCeiling holds the floor
// fixture under floorExportCeiling and floorArchiveCeiling: what every package
// of an application that touches a model pays before it does anything, in the
// build cache and in each package that imports it.
func TestAPackageThatNamesATableCompilesUnderTheExportCeiling(t *testing.T) {
	archive, export := fixtureSizes(t, "./testdata/floor")
	if export > floorExportCeiling {
		t.Errorf("a package that only names a table writes %d bytes of export data, over the ceiling of %d: the model's exported types reach more than they did", export, floorExportCeiling)
	}
	if archive > floorArchiveCeiling {
		t.Errorf("a package that only names a table compiles to a %d-byte archive, over the ceiling of %d: it compiles code it did not write", archive, floorArchiveCeiling)
	}
}
