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

// Base is the abstract Illuminate\View\Engines\Engine.
//
// PHP's abstract class holds one field and one method: the view drawn last,
// which the exception message names. An engine embeds this to answer for it.
type Base struct {
	lastRendered string
}

// GetLastRendered is Engine::getLastRendered.
func (e *Base) GetLastRendered() string { return e.lastRendered }

// setLastRendered records the path being drawn.
func (e *Base) setLastRendered(path string) { e.lastRendered = path }

// CompilerInterface is Illuminate\View\Compilers\CompilerInterface.
//
// The contract is declared where it is consumed rather than beside the
// implementation, which is what keeps view -> engines -> compilers -> view from
// closing into a cycle.
type CompilerInterface interface {
	// GetCompiledPath is Compiler::getCompiledPath.
	GetCompiledPath(path string) string

	// IsExpired is Compiler::isExpired.
	IsExpired(path string) (bool, error)

	// Compile is CompilerInterface::compile.
	Compile(path string) error
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

// Compiler is Illuminate\View\Engines\CompilerEngine.
//
// In Arandu there is no runtime compilation on the hot path: the kyse compiler
// runs at build time and produces Go functions. What survives from PHP is the
// bookkeeping around it -- whether a path was already found current in this
// process, and which path is being drawn when something fails.
type Compiler struct {
	Base

	compiler             CompilerInterface
	evaluate             func(path string, data map[string]any) (string, error)
	compiledOrNotExpired map[string]bool
}

// NewCompiler is CompilerEngine::__construct.
//
// evaluate stands in for PhpEngine::evaluatePath, which has no counterpart: it
// exists because PHP includes the compiled file at request time, and a compiled
// view here is a Go function that was linked into the binary.
func NewCompiler(compiler CompilerInterface, evaluate func(path string, data map[string]any) (string, error)) *Compiler {
	return &Compiler{
		compiler:             compiler,
		evaluate:             evaluate,
		compiledOrNotExpired: map[string]bool{},
	}
}

// Get is CompilerEngine::get.
func (c *Compiler) Get(path string, data map[string]any) (string, error) {
	c.setLastRendered(path)

	if c.compiler != nil && !c.compiledOrNotExpired[path] {
		expired, err := c.compiler.IsExpired(path)
		if err != nil {
			return "", err
		}
		if expired {
			if err := c.compiler.Compile(path); err != nil {
				return "", err
			}
		}
	}

	compiled := path
	if c.compiler != nil {
		compiled = c.compiler.GetCompiledPath(path)
	}

	if c.evaluate == nil {
		return "", &engineNotFoundError{name: "compiler"}
	}

	result, err := c.evaluate(compiled, data)
	if err != nil {
		return "", err
	}

	if c.compiledOrNotExpired == nil {
		c.compiledOrNotExpired = map[string]bool{}
	}
	c.compiledOrNotExpired[path] = true
	return result, nil
}

// GetCompiler is CompilerEngine::getCompiler.
func (c *Compiler) GetCompiler() CompilerInterface { return c.compiler }

// ForgetCompiledOrNotExpired is CompilerEngine::forgetCompiledOrNotExpired.
func (c *Compiler) ForgetCompiledOrNotExpired() {
	c.compiledOrNotExpired = map[string]bool{}
}

// File is Illuminate\View\Engines\FileEngine.
//
// It reads a raw file from disk. In Arandu that is `aru view:build` reading a
// .kyse.go source before compiling it, never a request.
type File struct {
	Base

	read func(path string) (string, error)
}

// NewFile is FileEngine::__construct.
func NewFile(read func(path string) (string, error)) *File {
	return &File{read: read}
}

// Get is FileEngine::get.
func (e *File) Get(path string, _ map[string]any) (string, error) {
	e.setLastRendered(path)
	return e.read(path)
}
