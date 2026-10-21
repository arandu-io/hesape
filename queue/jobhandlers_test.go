package queue_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue"
	"github.com/arandu-io/hesape/queue/jobs"
	"github.com/arandu-io/hesape/routing"
)

// jobbing is a module that runs jobs of its own.
type jobbing struct {
	name string
	jobs []string
	err  error
}

func (m jobbing) Name() string         { return m.name }
func (jobbing) Routes(*routing.Router) {}
func (m jobbing) RegisterJobHandlers(w *queue.Worker) error {
	for _, name := range m.jobs {
		w.HandleFunc(name, func(context.Context, auth.Grant, *jobs.Job) error { return nil })
	}
	return m.err
}

// silent runs no jobs.
type silent struct{}

func (silent) Name() string           { return "silent" }
func (silent) Routes(*routing.Router) {}

var _ queue.JobHandlers = jobbing{}

// TestRegisterJobHandlersGivesTheWorkerEveryModulesJobs: a module that runs
// none is skipped, and every one that runs some is registered.
func TestRegisterJobHandlersGivesTheWorkerEveryModulesJobs(t *testing.T) {
	w := registry()
	err := queue.RegisterJobHandlers(w,
		jobbing{name: "webhook", jobs: []string{"webhook.deliver", "webhook.process"}},
		silent{},
		jobbing{name: "billing", jobs: []string{"billing.charge"}},
	)
	if err != nil {
		t.Fatalf("RegisterJobHandlers: %v", err)
	}
	got := w.Names()
	slices.Sort(got)
	if want := []string{"billing.charge", "webhook.deliver", "webhook.process"}; !slices.Equal(got, want) {
		t.Fatalf("the worker handles %v, want %v", got, want)
	}
}

// TestRegisterJobHandlersNamesTheModuleThatFailed: the boot has to say whose
// handlers are missing.
func TestRegisterJobHandlersNamesTheModuleThatFailed(t *testing.T) {
	broken := errors.New("no sender configured")
	err := queue.RegisterJobHandlers(registry(), jobbing{name: "webhook", err: broken})
	if !errors.Is(err, broken) || !strings.Contains(err.Error(), "webhook") {
		t.Fatalf("err = %v, want the module's failure, naming it", err)
	}
	if err := queue.RegisterJobHandlers(nil, silent{}); err == nil {
		t.Fatal("a nil worker was accepted")
	}
}
