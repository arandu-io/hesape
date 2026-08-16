package console

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// reservedNames are the words a generated type may not be called.
//
// It is every Go keyword plus the predeclared identifiers a generated file
// would shadow. A type called `range` does not compile, and one called
// `error` compiles and then confuses every file that imports it -- so both
// are refused.
var reservedNames = []string{
	"break", "case", "chan", "const", "continue", "default", "defer", "else",
	"fallthrough", "for", "func", "go", "goto", "if", "import", "interface",
	"map", "package", "range", "return", "select", "struct", "switch", "type", "var",

	"any", "bool", "byte", "comparable", "complex64", "complex128", "error",
	"float32", "float64", "int", "int8", "int16", "int32", "int64", "rune",
	"string", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
	"append", "cap", "clear", "close", "copy", "delete", "len", "make", "max",
	"min", "new", "panic", "print", "println", "recover",
	"false", "true", "iota", "nil",
}

// GeneratorCommand writes a new source file from a stub.
//
// It is a value that each generator fills in, because the two things that
// vary between generators -- the stub and the type name -- are data, not
// behaviour.
type GeneratorCommand struct {
	// Type is what is being generated, for the messages: "Model", "Policy".
	Type string

	// Stub is the template. The placeholders are {{ package }}, {{ class }} and
	// {{ rootModule }}, in both the spaced and the unspaced spelling, which is
	// the pair replaceNamespace looks for.
	Stub string

	// RootModule is the module path of the application, which a generated file
	// imports itself relative to: "github.com/acme/app".
	RootModule string

	// BasePath is the directory the application's source lives in. A generated
	// name is resolved against it.
	BasePath string

	// DefaultDirectory is where this generator's files go when the name carries
	// no directory of its own: "models", "policies".
	DefaultDirectory string
}

// GetNameInput reads the name the command was given.
//
// A trailing extension is dropped: somebody who typed the file name meant the
// type.
func (g GeneratorCommand) GetNameInput(o *IO) string {
	name := strings.TrimSpace(o.Argument("name").String())
	return strings.TrimSuffix(name, ".go")
}

// IsReservedName reports whether the name is a word the language owns.
//
// The comparison is case-insensitive.
func (g GeneratorCommand) IsReservedName(name string) bool {
	return slices.Contains(reservedNames, strings.ToLower(strings.TrimSpace(name)))
}

// QualifyClass turns what was typed into the full name of what is generated.
//
// A name that already carries a directory keeps it, and one that does not
// gets the generator's default.
func (g GeneratorCommand) QualifyClass(name string) string {
	name = strings.TrimLeft(strings.ReplaceAll(name, "\\", "/"), "/")

	if strings.Contains(name, "/") || g.DefaultDirectory == "" {
		return name
	}
	return g.DefaultDirectory + "/" + name
}

// GetNamespace is the package a generated file belongs to.
//
// It is everything before the last separator. A name with no separator is
// the root package of the application.
func (g GeneratorCommand) GetNamespace(name string) string {
	directory := filepath.Dir(filepath.FromSlash(name))
	if directory == "." {
		return filepath.Base(g.BasePath)
	}
	return filepath.Base(directory)
}

// GetPath is where a generated file is written.
//
// The file is named after the type, in snake case, which is what a Go file
// is called.
func (g GeneratorCommand) GetPath(name string) string {
	directory := filepath.Dir(filepath.FromSlash(name))
	base := filepath.Base(filepath.FromSlash(name))
	return filepath.Join(g.BasePath, directory, snakeFileName(base)+".go")
}

// AlreadyExists reports whether the file is already there.
func (g GeneratorCommand) AlreadyExists(name string) bool {
	_, err := os.Stat(g.GetPath(g.QualifyClass(name)))
	return err == nil
}

// MakeDirectory creates the directory a file is about to be written into.
func (g GeneratorCommand) MakeDirectory(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// BuildClass renders the stub for a name.
//
// The package is substituted first, then the type name.
func (g GeneratorCommand) BuildClass(name string) string {
	stub := g.ReplaceNamespace(g.Stub, name)
	return g.ReplaceClass(stub, name)
}

// ReplaceNamespace writes the package and the module path into the stub.
//
// Both spellings of each placeholder are replaced.
func (g GeneratorCommand) ReplaceNamespace(stub, name string) string {
	replacements := map[string]string{
		"{{ package }}":    g.GetNamespace(name),
		"{{package}}":      g.GetNamespace(name),
		"{{ rootModule }}": g.RootModule,
		"{{rootModule}}":   g.RootModule,
	}
	for placeholder, value := range replacements {
		stub = strings.ReplaceAll(stub, placeholder, value)
	}
	return stub
}

// ReplaceClass writes the type name into the stub.
func (g GeneratorCommand) ReplaceClass(stub, name string) string {
	class := filepath.Base(filepath.FromSlash(name))
	for _, placeholder := range []string{"{{ class }}", "{{class}}"} {
		stub = strings.ReplaceAll(stub, placeholder, class)
	}
	return stub
}

// SortImports puts a generated import block in order.
//
// A stub that assembles its imports from replacements ends up with them in
// the order the replacements ran, and a file whose imports are not sorted is
// a file the formatter rewrites on the first save.
func (g GeneratorCommand) SortImports(stub string) string {
	lines := strings.Split(stub, "\n")

	start, end := -1, -1
	for i, line := range lines {
		if strings.HasPrefix(line, "import (") {
			start = i + 1
			continue
		}
		if start >= 0 && strings.TrimSpace(line) == ")" {
			end = i
			break
		}
	}
	if start < 0 || end <= start {
		return stub
	}

	block := make([]string, 0, end-start)
	for _, line := range lines[start:end] {
		if strings.TrimSpace(line) != "" {
			block = append(block, line)
		}
	}
	sort.Strings(block)

	sorted := append([]string{}, lines[:start]...)
	sorted = append(sorted, block...)
	sorted = append(sorted, lines[end:]...)
	return strings.Join(sorted, "\n")
}

// Handle generates the file.
//
// The order is the reserved name check first, so nothing is written when the
// name would not compile; then the already-exists check, unless --force;
// then the directory, the file and the message.
func (g GeneratorCommand) Handle(_ context.Context, o *IO) error {
	name := g.GetNameInput(o)
	if name == "" {
		return Exit(1, "%s needs a name", strings.ToLower(g.Type))
	}

	if g.IsReservedName(name) {
		o.OutputComponents().Error(fmt.Sprintf("The name %q is reserved by Go", name))
		return Exit(1, "")
	}

	qualified := g.QualifyClass(name)
	path := g.GetPath(qualified)

	force := o.HasOption("force") && o.Option("force").Bool()
	if !force && g.AlreadyExists(name) {
		o.OutputComponents().Error(g.Type + " already exists")
		return Exit(1, "")
	}

	if err := g.MakeDirectory(path); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(g.SortImports(g.BuildClass(qualified))), 0o644); err != nil {
		return err
	}

	o.OutputComponents().Info(fmt.Sprintf("%s [%s] created successfully", g.Type, path))
	return nil
}

// snakeFileName turns a type name into the file name it is written to.
//
// A Go file is lower case with underscores, so InvoiceLine becomes
// invoice_line.go.
func snakeFileName(name string) string {
	var out strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				out.WriteByte('_')
			}
			out.WriteRune(r - 'A' + 'a')
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// MigrationGeneratorCommand writes a migration for one table.
//
// The framework's own tables -- the cache table, the queue table, the
// session table -- each ship one of these, and the only thing that differs
// between them is the table name and the stub.
type MigrationGeneratorCommand struct {
	// MigrationTableName is the table the migration creates.
	MigrationTableName string

	// MigrationStub is the template. Its one placeholder is {{table}}.
	MigrationStub string

	// MigrationPath is the directory migrations are written to.
	MigrationPath string

	// Now returns the timestamp a migration is named with. Nil means the
	// process clock, and a test passes a fixed one.
	Now func() string
}

// MigrationExists reports whether a migration for the table is already there.
//
// The glob is on the suffix, so a migration written yesterday under a
// different timestamp still counts.
func (m MigrationGeneratorCommand) MigrationExists() (bool, error) {
	matches, err := filepath.Glob(filepath.Join(m.MigrationPath, "*_create_"+m.MigrationTableName+"_table.sql"))
	if err != nil {
		return false, err
	}
	return len(matches) > 0, nil
}

// CreateBaseMigration writes the file and returns its path.
func (m MigrationGeneratorCommand) CreateBaseMigration() (string, error) {
	if err := os.MkdirAll(m.MigrationPath, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(m.MigrationPath,
		m.timestamp()+"_create_"+m.MigrationTableName+"_table.sql")
	return path, os.WriteFile(path, nil, 0o644)
}

// ReplaceMigrationPlaceholders writes the stub into the file, table name
// substituted.
func (m MigrationGeneratorCommand) ReplaceMigrationPlaceholders(path string) error {
	stub := strings.ReplaceAll(m.MigrationStub, "{{table}}", m.MigrationTableName)
	return os.WriteFile(path, []byte(stub), 0o644)
}

// Handle generates the migration.
//
// An existing migration is an error rather than an overwrite, because the
// one that is there may already have run somewhere.
func (m MigrationGeneratorCommand) Handle(_ context.Context, o *IO) error {
	exists, err := m.MigrationExists()
	if err != nil {
		return err
	}
	if exists {
		o.OutputComponents().Error("Migration already exists")
		return Exit(1, "")
	}

	path, err := m.CreateBaseMigration()
	if err != nil {
		return err
	}
	if err := m.ReplaceMigrationPlaceholders(path); err != nil {
		return err
	}

	o.OutputComponents().Info("Migration created successfully")
	return nil
}

// timestamp is what a migration file is named with.
func (m MigrationGeneratorCommand) timestamp() string {
	if m.Now != nil {
		return m.Now()
	}
	return nowTimestamp()
}

// nowTimestamp is the default migration timestamp: the moment, to the second,
// in the order that sorts.
func nowTimestamp() string { return time.Now().UTC().Format("2006_01_02_150405") }
