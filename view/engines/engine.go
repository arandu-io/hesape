package engines

// Engine mirrors Illuminate\View\Engines\Engine.
//
// An Engine turns a view path and data into rendered content. In Arandu, views
// are compiled into Go functions by kyse at build time, so the engine
// delegates to the compiled function registry — there is no runtime
// compilation. The Engine interface exists to match the Illuminate surface.
type Engine interface {
	Get(path string, data map[string]any) (string, error)
}

// Resolver mirrors Illuminate\View\Engines\EngineResolver.
//
// It maps engine names to constructors. In Arandu, the only engine is the
// compiled kyse engine; this type preserves the Illuminate API surface for
// compatibility and future extension.
type Resolver struct {
	resolvers map[string]func() Engine
	resolved  map[string]Engine
}

// NewResolver returns a resolver with no engines registered.
func NewResolver() *Resolver {
	return &Resolver{
		resolvers: map[string]func() Engine{},
		resolved:  map[string]Engine{},
	}
}

// Register stores a constructor for the given engine name.
func (r *Resolver) Register(name string, resolver func() Engine) {
	delete(r.resolved, name)
	r.resolvers[name] = resolver
}

// Resolve returns the engine for name, creating it once and caching.
func (r *Resolver) Resolve(name string) (Engine, error) {
	if eng, ok := r.resolved[name]; ok {
		return eng, nil
	}
	resolver, ok := r.resolvers[name]
	if !ok {
		return nil, &engineNotFoundError{name: name}
	}
	eng := resolver()
	r.resolved[name] = eng
	return eng, nil
}

// Forget removes a resolved engine from the cache.
func (r *Resolver) Forget(name string) {
	delete(r.resolved, name)
}

type engineNotFoundError struct{ name string }

func (e *engineNotFoundError) Error() string {
	return "view: engine " + e.name + " is not registered"
}

// Compiler mirrors Illuminate\View\Engines\CompilerEngine.
//
// In Arandu, there is no runtime compilation. The kyse compiler runs at build
// time and produces Go functions. CompilerEngine delegates to the view
// function registry via the view.Func interface.
type Compiler struct {
	render func(path string, data map[string]any) (string, error)
}

// NewCompiler returns a CompilerEngine that renders compiled views.
func NewCompiler(render func(path string, data map[string]any) (string, error)) *Compiler {
	return &Compiler{render: render}
}

// Get renders the view at the given path with the given data.
func (c *Compiler) Get(path string, data map[string]any) (string, error) {
	return c.render(path, data)
}

// File mirrors Illuminate\View\Engines\FileEngine.
//
// It reads a raw file from disk. In Arandu, this is used during development
// by `aru view:build` to read .kyse.go source files before compilation.
type File struct {
	read func(path string) (string, error)
}

// NewFile returns a FileEngine that reads files with the given reader.
func NewFile(read func(path string) (string, error)) *File {
	return &File{read: read}
}

// Get reads the file at path and returns its content.
func (e *File) Get(path string, _ map[string]any) (string, error) {
	return e.read(path)
}
