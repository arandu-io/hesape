// Package linking keeps the registries that record which connectors a binary
// links, and writes the one error every registry gives when the configuration
// asks for a connector nobody imported.
//
// A connector is a package with an init() and nothing to call: the binary links
// what it imports, and a setting in the environment names which of the linked
// connectors to use. So a value the binary cannot serve is never a typo the
// process could correct at run time -- it is an import missing from the build,
// and the error has to say that, with the command and the line that add it.
//
// The text lives here, once, because three packages give it: database for
// DATABASE_URL, cache for CACHE_STORE and SESSION_DRIVER, and queue for
// QUEUE_CONNECTION. Three copies of one sentence are three sentences the next
// edit makes disagree.
package linking

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// NotLinked returns the error for a setting that asks for a connector no
// imported package registered.
//
// setting is the environment variable that asked, driver is the value it
// holds, module is the import path that carries the connector, and linked is
// what the binary does link, listed so the gap is visible. An empty module
// means no package of the collection provides that connector, and the error
// says so instead of naming a command that would fail.
func NotLinked(setting, driver, module string, linked []string) error {
	list := "none"
	if len(linked) > 0 {
		list = strings.Join(linked, ", ")
	}

	head := fmt.Sprintf("%s asks for %s and no connector for it is linked into this binary (linked: %s).", setting, driver, list)
	if module == "" {
		return errors.New(head + "\nNo package of github.com/arandu-io/hesape provides one.")
	}
	return errors.New(head + "\n" +
		"Add it:\n\n" +
		"    go get " + module + "\n\n" +
		"and blank-import it in bootstrap/app.go, next to the other connectors:\n\n" +
		"    _ \"" + module + "\"")
}

// Registry maps the value a setting takes to the connector registered for it.
//
// It is safe for concurrent use. Registration happens from init(), which the
// runtime runs on one goroutine, but a lookup can race a registration in a test
// that registers late, and the race detector is right to say so.
type Registry[C any] struct {
	component string

	mu     sync.RWMutex
	byName map[string]C
}

// NewRegistry returns an empty registry. component names the package that owns
// it, and prefixes the panic a duplicate registration raises.
func NewRegistry[C any](component string) *Registry[C] {
	return &Registry[C]{component: component, byName: map[string]C{}}
}

// Register records c under name.
//
// A duplicate name panics rather than picking one. Each package initialises
// once, so a second registration under a name means two imported packages claim
// the same setting value -- an import nobody meant to add, and finding out at
// boot beats finding out from a store that behaves differently. An empty name
// panics too: no setting can ask for it, so the connector could never be used.
func (r *Registry[C]) Register(name string, c C) {
	if name == "" {
		panic(r.component + ": a connector registered with an empty driver name, which no setting can ask for")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, taken := r.byName[name]; taken {
		panic(fmt.Sprintf("%s: a connector for %q is already registered, and a second one wants it too -- remove one of the imports", r.component, name))
	}
	r.byName[name] = c
}

// Names reports the registered names, sorted, so two runs report the same
// thing.
func (r *Registry[C]) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.namesLocked()
}

// Lookup returns the connector registered under name and whether there is one.
// When there is none it also returns the registered names, read under the same
// lock, so the error built from them describes the registry the lookup saw.
func (r *Registry[C]) Lookup(name string) (C, []string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, found := r.byName[name]
	if found {
		return c, nil, true
	}
	return c, r.namesLocked(), false
}

// namesLocked returns the registered names. The caller holds the lock: a
// sync.RWMutex is not reentrant, and taking the read lock twice deadlocks the
// moment a writer queues between the two acquisitions.
func (r *Registry[C]) namesLocked() []string {
	out := make([]string, 0, len(r.byName))
	for name := range r.byName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
