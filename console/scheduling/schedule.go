package scheduling

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/console"
)

// Dispatcher is what Job pushes onto the queue.
//
// It is declared here rather than imported: the queue is not a dependency of the
// scheduler, and what a schedule needs of a dispatcher is one method.
type Dispatcher interface {
	// Dispatch pushes the job, on the named queue and connection. Either may be
	// empty, and then the job's own is used.
	Dispatch(ctx context.Context, job any, queue, connection string) error
}

// Tenants returns the tenants a per-tenant event expands to.
//
// The tenant comes from the Grant, and the core does not know where the
// application keeps its tenants -- a table, a config file, a control plane
// -- so the list is injected. Returning an empty list is valid and means
// those events simply do not run.
type Tenants func(ctx context.Context) ([]string, error)

// Schedule is every event an application runs on a clock.
//
// It is built at wiring time and read afterwards: it is not safe to declare
// an event on one goroutine while another is running the schedule.
type Schedule struct {
	events []*Event

	eventMutex      EventMutex
	schedulingMutex SchedulingMutex

	timezone   *time.Location
	dispatcher Dispatcher

	// Tenants expands per-tenant events. Nil means those events do not run,
	// which the runner reports rather than swallowing.
	Tenants Tenants

	// Environment is what an event's Environments list is matched against.
	Environment string

	// DownForMaintenance reports whether the application is down. Nil means it
	// is not.
	DownForMaintenance func() bool

	// mutexCache remembers what ServerShouldRun already answered for one tick,
	// so an event asked about twice does not claim the window twice.
	mutexCache map[string]bool

	attributes *PendingEventAttributes
	groupStack []*PendingEventAttributes
}

// NewSchedule returns an empty schedule.
//
// The two mutexes are passed explicitly rather than resolved implicitly;
// nil for either means the events that need it are refused rather than run
// unprotected.
func NewSchedule(eventMutex EventMutex, schedulingMutex SchedulingMutex, timezone *time.Location) *Schedule {
	return &Schedule{
		eventMutex:      eventMutex,
		schedulingMutex: schedulingMutex,
		timezone:        timezone,
		mutexCache:      map[string]bool{},
	}
}

// UseCache points both mutexes at another lock issuer.
//
// The issuer is passed here, and a mutex that is not CacheAware is left
// alone.
func (s *Schedule) UseCache(locks *cache.Locks) *Schedule {
	if aware, ok := s.eventMutex.(CacheAware); ok {
		aware.UseStore(locks)
	}
	if aware, ok := s.schedulingMutex.(CacheAware); ok {
		aware.UseStore(locks)
	}
	return s
}

// UseDispatcher gives the schedule the queue Job pushes onto.
func (s *Schedule) UseDispatcher(dispatcher Dispatcher) *Schedule {
	s.dispatcher = dispatcher
	return s
}

// Call schedules a closure.
//
// The closure receives the Grant the event was declared with, which is
// required of any path that can reach a repository.
func (s *Schedule) Call(callback Callback) *CallbackEvent {
	event := NewCallbackEvent(s.eventMutex, callback, s.timezone)
	s.events = append(s.events, event.Event)
	s.mergePendingAttributes(event.Event)
	return event
}

// Command schedules one of the application's own console commands.
func (s *Schedule) Command(command string, parameters ...string) *Event {
	return s.Exec(console.FormatCommandString(command), parameters...)
}

// Job schedules a queued job.
//
// A job that reaches the queue with no dispatcher wired fails the run rather
// than being dropped, because a scheduled job that silently never enqueues
// is the failure nobody notices for a month.
func (s *Schedule) Job(job any, queue, connection string) *CallbackEvent {
	event := s.Call(func(ctx context.Context, _ auth.Grant) error {
		if s.dispatcher == nil {
			return fmt.Errorf("scheduling: the job %T is scheduled and no dispatcher is wired", job)
		}
		return s.dispatcher.Dispatch(ctx, job, queue, connection)
	})
	event.Name(fmt.Sprintf("%T", job))
	return event
}

// Exec schedules a command line.
//
// An expression that does not parse is not possible here -- the event
// starts on every minute -- but a parameter list that would change the
// meaning of the line is escaped, which is what compileParameters is for.
func (s *Schedule) Exec(command string, parameters ...string) *Event {
	if len(parameters) > 0 {
		command += " " + s.compileParameters(parameters)
	}
	event := NewEvent(s.eventMutex, command, s.timezone)
	s.events = append(s.events, event)
	s.mergePendingAttributes(event)
	return event
}

// Attributes returns the attributes waiting to be merged into the next event,
// creating them if there are none.
//
// Go has no fallthrough for a missing method, so the caller asks for the
// attributes explicitly instead of one being created implicitly. Chaining a
// frequency onto the result is what Group then reads.
func (s *Schedule) Attributes() *PendingEventAttributes {
	if s.attributes == nil {
		if len(s.groupStack) > 0 {
			clone := *s.groupStack[len(s.groupStack)-1]
			cloneEvent := *clone.Event
			clone.Event = &cloneEvent
			s.attributes = &clone
		} else {
			s.attributes = NewPendingEventAttributes(s)
		}
	}
	return s.attributes
}

// Group declares several events that share the attributes named before it.
//
// Calling it without having named an attribute first panics: a group with
// nothing to merge is a group that does nothing, and it is a mistake in the
// schedule rather than a condition to handle.
func (s *Schedule) Group(events func(*Schedule)) {
	if s.attributes == nil {
		panic("scheduling: call an attribute method such as Schedule.Attributes().Daily() before defining a schedule group")
	}

	s.groupStack = append(s.groupStack, s.attributes)
	s.attributes = nil

	events(s)

	s.groupStack = s.groupStack[:len(s.groupStack)-1]
}

// mergePendingAttributes merges the enclosing group first, then whatever was
// named for this one event.
func (s *Schedule) mergePendingAttributes(event *Event) {
	if len(s.groupStack) > 0 {
		s.groupStack[len(s.groupStack)-1].MergeAttributes(event)
	}
	if s.attributes != nil {
		s.attributes.MergeAttributes(event)
		s.attributes = nil
	}
}

// compileParameters escapes every value that is not a number and not a
// flag, and a key=value pair keeps its key.
func (s *Schedule) compileParameters(parameters []string) string {
	compiled := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		if isNumeric(parameter) || strings.HasPrefix(parameter, "-") {
			compiled = append(compiled, parameter)
			continue
		}
		if key, value, found := strings.Cut(parameter, "="); found && !strings.HasPrefix(key, "-") {
			compiled = append(compiled, key+"="+escapeArgument(value))
			continue
		}
		compiled = append(compiled, escapeArgument(parameter))
	}
	return strings.Join(compiled, " ")
}

// CompileArrayInput renders a repeated parameter as a command line fragment.
//
// A long flag repeats as --key=value, a short one as -k value, and a
// positional list is just the values.
func (s *Schedule) CompileArrayInput(key string, values []string) string {
	rendered := make([]string, 0, len(values))
	for _, value := range values {
		escaped := escapeArgument(value)
		switch {
		case strings.HasPrefix(key, "--"):
			rendered = append(rendered, key+"="+escaped)
		case strings.HasPrefix(key, "-"):
			rendered = append(rendered, key+" "+escaped)
		default:
			rendered = append(rendered, escaped)
		}
	}
	return strings.Join(rendered, " ")
}

// ServerShouldRun reports whether this replica is the one that runs the event.
//
// An event asked about twice in one tick gets the same answer, because the
// second ask would otherwise find the window already claimed by the first
// and say no.
func (s *Schedule) ServerShouldRun(ctx context.Context, event *Event, at time.Time) (bool, error) {
	name := event.MutexName()
	if answer, asked := s.mutexCache[name]; asked {
		return answer, nil
	}
	if s.schedulingMutex == nil {
		return false, fmt.Errorf(
			"scheduling: the event %s is limited to one server and no scheduling mutex is wired; "+
				"pass one to NewSchedule, or every replica would run it", event.GetSummaryForDisplay())
	}

	answer, err := s.schedulingMutex.Create(ctx, event, at)
	if err != nil {
		return false, err
	}
	s.mutexCache[name] = answer
	return answer, nil
}

// ForgetMutexCache drops what ServerShouldRun remembered.
//
// The cache is scoped to one tick: the runner calls this between them, and
// without it a replica that lost one window would lose every window afterwards.
func (s *Schedule) ForgetMutexCache() { clear(s.mutexCache) }

// DueEvents returns the events that should run at the given time.
func (s *Schedule) DueEvents(at time.Time) []*Event {
	down := s.downForMaintenance()

	due := make([]*Event, 0, len(s.events))
	for _, event := range s.events {
		if event.IsDue(at, s.Environment, down) {
			due = append(due, event)
		}
	}
	return due
}

// downForMaintenance reports whether the application is down, and reads false
// when nobody said.
func (s *Schedule) downForMaintenance() bool {
	return s.DownForMaintenance != nil && s.DownForMaintenance()
}

// Events returns every declared event.
func (s *Schedule) Events() []*Event { return s.events }

// isNumeric reports whether every rune is a digit, which is the test
// compileParameters makes before it decides to escape.
func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
