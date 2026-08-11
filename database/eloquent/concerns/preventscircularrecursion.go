package concerns

import "sync"

// PreventsCircularRecursion answers
// Illuminate\Database\Eloquent\Concerns\PreventsCircularRecursion.
//
// Two models that hold each other -- a post whose author holds their posts --
// turn any walk of the object graph into an infinite one. The PHP keeps a
// WeakMap of the calls already on the stack and returns a default instead of
// recursing; this keeps a set of call names per instance, which is the same
// guard without the weak reference Go does not need: the set is cleared when
// the outermost call returns.
type PreventsCircularRecursion struct {
	stack map[string]struct{}
	mutex sync.Mutex
}

// WithoutRecursion answers PreventsCircularRecursion::withoutRecursion.
//
// The first call with a given name runs the callback; a call with the same name
// while that one is still on the stack answers the default instead. That is how
// toArray on a cycle terminates rather than filling the stack.
func (p *PreventsCircularRecursion) WithoutRecursion(name string, callback func() any, def any) any {
	p.mutex.Lock()
	if p.stack == nil {
		p.stack = map[string]struct{}{}
	}
	if _, running := p.stack[name]; running {
		p.mutex.Unlock()
		return def
	}
	p.stack[name] = struct{}{}
	p.mutex.Unlock()

	defer p.clearRecursiveCallValue(name)

	return callback()
}

// IsRecursing reports whether a call of that name is already on the stack.
func (p *PreventsCircularRecursion) IsRecursing(name string) bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	_, running := p.stack[name]
	return running
}

// clearRecursiveCallValue answers
// PreventsCircularRecursion::clearRecursiveCallValue.
func (p *PreventsCircularRecursion) clearRecursiveCallValue(name string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	delete(p.stack, name)
}
