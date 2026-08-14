package pipeline

import (
	"fmt"
	"sync"
)

// Hub mirrors Illuminate\Pipeline\Hub: a set of pipelines under names, so a
// caller sends an object through one by name instead of holding the list of
// pipes it takes.
//
// The callback is given a fresh [Pipeline] and the object, and it is the one
// that decides the pipes -- `fn ($pipeline, $object) => $pipeline->send($object)
// ->through([...])->thenReturn()`. Illuminate's Hub is not generic because
// everything it carries is mixed; here the object type is the parameter, and a
// process with more than one passable type has more than one Hub.
//
// The zero value is an empty Hub and it is usable. Unlike the PHP, a Hub is
// safe for concurrent use: pipelines are defined at boot and sent through while
// serving, which in Go is a data race unless the map is guarded.
type Hub[T any] struct {
	mu        sync.RWMutex
	pipelines map[string]func(p *Pipeline[T], passable T) (T, error)
}

// NewHub is Hub::__construct. It answers `new Hub($container)` without the
// container, which ADR 0002 rejected: the callback that defines a pipeline
// closes over whatever its pipes need.
func NewHub[T any]() *Hub[T] {
	return &Hub[T]{}
}

// Pipeline is Hub::pipeline: it defines a named pipeline.
//
// Defining a name twice keeps the second callback, as assigning the array key
// twice does.
func (h *Hub[T]) Pipeline(name string, callback func(p *Pipeline[T], passable T) (T, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.pipelines == nil {
		h.pipelines = make(map[string]func(*Pipeline[T], T) (T, error), 1)
	}
	h.pipelines[name] = callback
}

// Defaults is Hub::defaults: it defines the default named pipeline, which is
// pipeline('default', $callback) -- the pipeline [Hub.Pipe] reaches for when it
// is given no name.
func (h *Hub[T]) Defaults(callback func(p *Pipeline[T], passable T) (T, error)) {
	h.Pipeline("default", callback)
}

// Pipe is Hub::pipe: it sends an object through one of the available
// pipelines.
//
// At most one name may be given, which is PHP's optional argument, and an empty
// one is no name: both reach the pipeline [Hub.Defaults] defined, because PHP
// writes `$pipeline ?: 'default'` and the empty string is the falsy value Go
// has. Each call builds a fresh [Pipeline], so nothing a pipeline was sent
// leaks into the next one.
//
// A name that was never defined fails rather than reaching for a missing key.
// The clone predates that check; the message follows
// reference_laravel/framework/src/Illuminate/Pipeline/Hub.php, which throws
// InvalidArgumentException. A name defined with a nil callback is not defined.
func (h *Hub[T]) Pipe(object T, pipeline ...string) (T, error) {
	name := "default"
	if len(pipeline) > 0 && pipeline[0] != "" {
		name = pipeline[0]
	}

	h.mu.RLock()
	callback, ok := h.pipelines[name]
	h.mu.RUnlock()

	if !ok || callback == nil {
		var zero T
		return zero, fmt.Errorf("pipeline: pipeline [%s] is not defined", name)
	}
	return callback(New[T](), object)
}
