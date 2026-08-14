package console

import (
	"os"
	"os/signal"
	"sync"
)

// availabilityResolver decides whether signal handling is available.
//
// It answers Signals::$availabilityResolver, and it is static there for the same
// reason it is package level here: the answer is a property of the process, not
// of one command. Nil means available, which on a Go binary it always is -- the
// PHP has to ask because pcntl is an optional extension.
var availabilityResolver func() bool

// ResolveAvailabilityUsing sets how availability is decided.
//
// It answers Signals::resolveAvailabilityUsing. It is what a test uses to run
// the trap path on a platform where taking a signal would kill the test binary.
func ResolveAvailabilityUsing(resolver func() bool) { availabilityResolver = resolver }

// WhenAvailable runs the callback if signal handling is available.
//
// It answers Signals::whenAvailable.
func WhenAvailable(callback func()) {
	if availabilityResolver != nil && !availabilityResolver() {
		return
	}
	if callback != nil {
		callback()
	}
}

// Signals is the set of handlers one command installed.
//
// It answers Illuminate\Console\Signals. It exists so the handlers a command
// registers are undone when it finishes: a command that traps SIGINT and leaves
// the handler behind changes what ctrl-c does for everything the process runs
// afterwards, which in a long-lived binary is the rest of its life.
type Signals struct {
	mu       sync.Mutex
	channels []chan os.Signal
	done     chan struct{}
	wg       sync.WaitGroup
}

// NewSignals is Signals::__construct, over nothing: PHP is handed Symfony's
// SignalRegistry and snapshots the handlers already on it, and here the previous
// handlers stay where they are because Go delivers a signal to every channel
// that asked for it.
func NewSignals() *Signals { return &Signals{done: make(chan struct{})} }

// Register installs a handler for one signal.
//
// It answers Signals::register. The callback runs on a goroutine of the
// registrar's, so a handler that blocks holds nothing but itself; PHP has no
// choice about this and runs it inside the signal dispatch.
//
// The handler that was there before is not replaced: Go delivers a signal to
// every channel that asked for it, which is the behaviour the PHP goes to some
// length to reproduce by keeping the previous handlers and putting the new one
// first.
func (s *Signals) Register(sig os.Signal, callback func(os.Signal)) {
	if callback == nil {
		return
	}

	channel := make(chan os.Signal, 1)
	signal.Notify(channel, sig)

	s.mu.Lock()
	s.channels = append(s.channels, channel)
	done := s.done
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case received, open := <-channel:
				if !open {
					return
				}
				callback(received)
			case <-done:
				return
			}
		}
	}()
}

// Unregister takes every handler back off.
//
// It answers Signals::unregister, and it waits for the goroutines to stop so a
// handler is never running after the command that installed it returned.
func (s *Signals) Unregister() {
	s.mu.Lock()
	channels := s.channels
	s.channels = nil
	if s.done != nil {
		close(s.done)
		s.done = make(chan struct{})
	}
	s.mu.Unlock()

	for _, channel := range channels {
		signal.Stop(channel)
	}
	s.wg.Wait()
}

// Trap runs the callback when any of the signals arrives.
//
// It answers Concerns\InteractsWithSignals::trap. The registrar is created on
// first use and lives on the IO, which is one command's handle: Untrap is what
// the run calls when the command returns.
func (o *IO) Trap(callback func(os.Signal), signals ...os.Signal) {
	WhenAvailable(func() {
		if o.signals == nil {
			o.signals = NewSignals()
		}
		for _, sig := range signals {
			o.signals.Register(sig, callback)
		}
	})
}

// Untrap takes back every handler this command installed.
//
// It answers InteractsWithSignals::untrap, which the PHP calls in the finally
// of Command::run.
func (o *IO) Untrap() {
	if o.signals == nil {
		return
	}
	o.signals.Unregister()
	o.signals = nil
}
