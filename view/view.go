package view

import (
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/arandu-io/hesape/validation"
)

// View mirrors Illuminate\View\View.
//
// It wraps a render operation: a compiled Func, a name, a path, and the data
// merged from shared globals and per-render additions. A View is produced by
// Factory.Make and drawn by Render.
type View struct {
	factory *Factory
	engine  *Renderer
	name    string
	path    string
	data    map[string]any
	shared  map[string]any
}

// With adds data to the view. It accepts a key and a value, or a map.
//
//	view.With("title", "Hello")
//	view.With(map[string]any{"title": "Hello", "count": 42})
func (v *View) With(key string, value any) *View {
	if v.data == nil {
		v.data = map[string]any{}
	}
	v.data[key] = value
	return v
}

// WithName sets the view name.
func (v *View) WithName(name string) *View {
	v.name = name
	return v
}

// WithErrors shares the validation error bag with the view.
//
// It answers Illuminate\View\View::withErrors. The bag is reachable as
// the "errors" key in the data.
func (v *View) WithErrors(errs validation.Errors) *View {
	if v.data == nil {
		v.data = map[string]any{}
	}
	v.data["errors"] = errs
	return v
}

// Nest embeds a sub-view as data.
//
//	nest("sidebar", factory.Make("partials.sidebar", data))
func (v *View) Nest(key string, child *View) *View {
	if v.data == nil {
		v.data = map[string]any{}
	}
	v.data[key] = child
	return v
}

// GetData returns the per-view data merged with the shared globals.
func (v *View) GetData() map[string]any {
	merged := make(map[string]any, len(v.shared)+len(v.data))
	for k, val := range v.shared {
		merged[k] = val
	}
	for k, val := range v.data {
		merged[k] = val
	}
	return merged
}

// GetName returns the view's logical name (e.g. "posts/index").
func (v *View) GetName() string { return v.name }

// GetPath returns the absolute path of the source file.
func (v *View) GetPath() string { return v.path }

// SetPath changes the path.
func (v *View) SetPath(path string) { v.path = path }

// Render draws the view and returns the HTML.
//
// It increments the render counter, calls composers, gathers data, and runs
// the compiled function. Errors are decorated with the view name.
func (v *View) Render() (string, error) {
	if v.factory != nil {
		v.factory.IncrementRender()
		defer v.factory.DecrementRender()

		v.factory.CallComposer(v)
	}

	// Pick up shared data that was added after construction.
	var shared map[string]any
	if v.factory != nil {
		shared = v.factory.GetShared()
	}
	data := v.GetData()
	if shared == nil {
		shared = map[string]any{}
	}
	// Rebuild merged data with the latest shared values.
	for k, val := range shared {
		if _, overridden := v.data[k]; !overridden {
			data[k] = val
		}
	}

	buf := bufferPool.Get().(*strings.Builder)
	buf.Reset()
	defer func() {
		buf.Reset()
		bufferPool.Put(buf)
	}()

	if v.engine == nil {
		v.engine = NewRenderer()
	}

	mu.RLock()
	fn, known := views[v.name]
	mu.RUnlock()

	if !known {
		return "", fmt.Errorf("view: %q is not registered. Registered: %s",
			v.name, strings.Join(Registered(), ", "))
	}

	if err := fn(buf, data); err != nil {
		return "", fmt.Errorf("view: rendering %q: %w", v.name, err)
	}

	return buf.String(), nil
}

// String renders the view and returns the HTML, or a panic description.
func (v *View) String() string {
	s, err := v.Render()
	if err != nil {
		return html.EscapeString(err.Error())
	}
	return s
}

// ToHTML answers Illuminate\Contracts\Support\Htmlable.
func (v *View) ToHTML() string { return v.String() }

// Gather returns the merged data without rendering.
func (v *View) Gather() map[string]any { return v.GetData() }

// RenderView renders a View to an io.Writer directly.
func RenderView(w io.Writer, v *View) error {
	s, err := v.Render()
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, s)
	return err
}
