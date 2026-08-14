package pagination

import "sync"

// The nine views Illuminate ships under Pagination/resources/views, by the name
// a paginator asks for one by. They are names, not templates: nothing in this
// package renders (see the package doc), and the view layer is what turns a
// name into markup.
//
// The default is Tailwind, as it is in Illuminate.
const (
	TailwindView             = "pagination::tailwind"
	SimpleTailwindView       = "pagination::simple-tailwind"
	BootstrapFiveView        = "pagination::bootstrap-5"
	SimpleBootstrapFiveView  = "pagination::simple-bootstrap-5"
	BootstrapFourView        = "pagination::bootstrap-4"
	SimpleBootstrapFourView  = "pagination::simple-bootstrap-4"
	BootstrapThreeView       = "pagination::bootstrap-3"
	SimpleBootstrapThreeView = "pagination::simple-bootstrap-3"
	SemanticUIView           = "pagination::semantic-ui"
)

var (
	viewMu            sync.RWMutex
	defaultView       = TailwindView
	defaultSimpleView = SimpleTailwindView
)

// DefaultView is AbstractPaginator::defaultView and reads back
// AbstractPaginator::$defaultView, the static property
// a view reads, and AbstractPaginator::defaultView(), the static method that
// writes it. Called with a view it sets the default and returns it; called with
// none it reads.
//
// PHP has a property and a method of the same name for the two directions, and
// Go has one namespace for both, so the argument is what tells them apart --
// the same shape AbstractPaginator::fragment() has in PHP.
//
// It is the view a numbered pager is rendered with. Set it once where the
// application is wired: it is global, as it is in Illuminate, and a request
// that changes it changes it for every other request in flight.
func DefaultView(view ...string) string {
	viewMu.Lock()
	defer viewMu.Unlock()
	if len(view) > 0 && view[0] != "" {
		defaultView = view[0]
	}
	return defaultView
}

// DefaultSimpleView is AbstractPaginator::defaultSimpleView and reads back
// AbstractPaginator::$defaultSimpleView, and
// AbstractPaginator::defaultSimpleView(), for the pager that has only previous
// and next. It reads and writes the same way [DefaultView] does.
func DefaultSimpleView(view ...string) string {
	viewMu.Lock()
	defer viewMu.Unlock()
	if len(view) > 0 && view[0] != "" {
		defaultSimpleView = view[0]
	}
	return defaultSimpleView
}

// UseTailwind is AbstractPaginator::useTailwind. It is the default.
func UseTailwind() {
	DefaultView(TailwindView)
	DefaultSimpleView(SimpleTailwindView)
}

// UseBootstrap is AbstractPaginator::useBootstrap, which is
// [UseBootstrapFour] under the name it had before there were four and five.
func UseBootstrap() { UseBootstrapFour() }

// UseBootstrapThree is AbstractPaginator::useBootstrapThree.
func UseBootstrapThree() {
	DefaultView(BootstrapThreeView)
	DefaultSimpleView(SimpleBootstrapThreeView)
}

// UseBootstrapFour is AbstractPaginator::useBootstrapFour.
func UseBootstrapFour() {
	DefaultView(BootstrapFourView)
	DefaultSimpleView(SimpleBootstrapFourView)
}

// UseBootstrapFive is AbstractPaginator::useBootstrapFive.
func UseBootstrapFive() {
	DefaultView(BootstrapFiveView)
	DefaultSimpleView(SimpleBootstrapFiveView)
}
