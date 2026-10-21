package queue

import (
	"errors"
	"fmt"

	"github.com/arandu-io/hesape/foundation"
)

// JobHandlers is optional on a foundation.Module: the module runs jobs of its
// own and gives the worker their handlers, so the kernel registers them on the
// application's worker instead of every application calling each module by
// hand.
//
// It is declared here and not beside foundation.Module because a handler is
// registered on a [Worker], and this package already builds on foundation.
type JobHandlers interface {
	RegisterJobHandlers(w *Worker) error
}

// RegisterJobHandlers gives w the handlers of every module that declares
// some, in registration order.
//
// A failure names the module it came from and stops there, because a worker
// missing one module's handlers parks that module's jobs as unhandled -- the
// boot is where that belongs, not the first job.
func RegisterJobHandlers(w *Worker, modules ...foundation.Module) error {
	if w == nil {
		return errors.New("queue: RegisterJobHandlers needs a worker")
	}
	for _, m := range modules {
		handlers, ok := m.(JobHandlers)
		if !ok {
			continue
		}
		if err := handlers.RegisterJobHandlers(w); err != nil {
			return fmt.Errorf("queue: registering the job handlers of module %s: %w", m.Name(), err)
		}
	}
	return nil
}
