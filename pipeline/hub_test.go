package pipeline_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/pipeline"
)

// upper is the pipe the hub tests send strings through.
func upper(passable string, next pipeline.Destination[string]) (string, error) {
	return next(strings.ToUpper(passable))
}

func TestHubSendsThroughANamedPipeline(t *testing.T) {
	t.Parallel()

	hub := pipeline.NewHub[string]()
	hub.Pipeline("shout", func(p *pipeline.Pipeline[string], passable string) (string, error) {
		return p.Send(passable).Through(upper).ThenReturn()
	})

	got, err := hub.Pipe("quiet", "shout")
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	if got != "QUIET" {
		t.Fatalf("got %q, want QUIET", got)
	}
}

func TestHubWithoutANameReachesTheDefaultPipeline(t *testing.T) {
	t.Parallel()

	hub := pipeline.NewHub[string]()
	hub.Defaults(func(p *pipeline.Pipeline[string], passable string) (string, error) {
		return p.Send(passable).Through(upper).ThenReturn()
	})

	// `$pipeline ?: 'default'`: no argument and an empty one are the same
	// thing, and both are the pipeline Defaults named.
	for _, name := range [][]string{nil, {""}} {
		got, err := hub.Pipe("quiet", name...)
		if err != nil {
			t.Fatalf("Pipe(%v): %v", name, err)
		}
		if got != "QUIET" {
			t.Fatalf("Pipe(%v): got %q, want QUIET", name, got)
		}
	}

	// Defaults is pipeline('default', $callback), so the name is reachable.
	got, err := hub.Pipe("quiet", "default")
	if err != nil {
		t.Fatalf("Pipe(default): %v", err)
	}
	if got != "QUIET" {
		t.Fatalf("got %q, want QUIET", got)
	}
}

func TestHubPassesAFreshPipelineEveryTime(t *testing.T) {
	t.Parallel()

	hub := pipeline.NewHub[string]()
	var seen []*pipeline.Pipeline[string]
	hub.Pipeline("collect", func(p *pipeline.Pipeline[string], passable string) (string, error) {
		seen = append(seen, p)
		// A pipe pushed here must not survive into the next call.
		return p.Send(passable).Pipe(upper).ThenReturn()
	})

	for _, in := range []string{"one", "two"} {
		got, err := hub.Pipe(in, "collect")
		if err != nil {
			t.Fatalf("Pipe: %v", err)
		}
		if got != strings.ToUpper(in) {
			t.Fatalf("got %q, want %q", got, strings.ToUpper(in))
		}
	}
	if len(seen) != 2 || seen[0] == seen[1] {
		t.Fatal("the second call was handed the pipeline the first one built")
	}
}

func TestHubKeepsTheSecondCallbackForAName(t *testing.T) {
	t.Parallel()

	hub := pipeline.NewHub[string]()
	hub.Pipeline("shout", func(p *pipeline.Pipeline[string], passable string) (string, error) {
		return "first", nil
	})
	hub.Pipeline("shout", func(p *pipeline.Pipeline[string], passable string) (string, error) {
		return "second", nil
	})

	got, err := hub.Pipe("x", "shout")
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	if got != "second" {
		t.Fatalf("got %q, want second", got)
	}
}

func TestHubFailsOnAPipelineThatWasNeverDefined(t *testing.T) {
	t.Parallel()

	hub := pipeline.NewHub[string]()

	_, err := hub.Pipe("x", "missing")
	if err == nil {
		t.Fatal("an undefined pipeline answered without an error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Fatalf("the message lost the name: %v", err)
	}

	// No pipeline at all is the default one being undefined.
	if _, err := hub.Pipe("x"); err == nil {
		t.Fatal("an undefined default pipeline answered without an error")
	}
}

func TestHubTreatsANilCallbackAsUndefined(t *testing.T) {
	t.Parallel()

	hub := pipeline.NewHub[string]()
	hub.Pipeline("shout", nil)

	if _, err := hub.Pipe("x", "shout"); err == nil {
		t.Fatal("a nil callback answered without an error")
	}
}

func TestHubCarriesTheErrorFromThePipeline(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")
	hub := pipeline.NewHub[int]()
	hub.Defaults(func(p *pipeline.Pipeline[int], passable int) (int, error) {
		return p.Send(passable).Through(func(int, pipeline.Destination[int]) (int, error) {
			return 0, boom
		}).ThenReturn()
	})

	if _, err := hub.Pipe(1); !errors.Is(err, boom) {
		t.Fatalf("got %v, want %v", err, boom)
	}
}

func TestZeroValueHubIsUsable(t *testing.T) {
	t.Parallel()

	var hub pipeline.Hub[string]
	hub.Defaults(func(p *pipeline.Pipeline[string], passable string) (string, error) {
		return p.Send(passable).Through(upper).ThenReturn()
	})

	got, err := hub.Pipe("quiet")
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	if got != "QUIET" {
		t.Fatalf("got %q, want QUIET", got)
	}
}

func TestHubIsUsedFromSeveralGoroutines(t *testing.T) {
	t.Parallel()

	// Pipelines are defined at boot and sent through while serving.
	hub := pipeline.NewHub[string]()
	hub.Defaults(func(p *pipeline.Pipeline[string], passable string) (string, error) {
		return p.Send(passable).Through(upper).ThenReturn()
	})

	const n = 8
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := hub.Pipe("quiet")
			if err != nil {
				t.Errorf("Pipe: %v", err)
				return
			}
			if got != "QUIET" {
				t.Errorf("got %q, want QUIET", got)
			}
		}()
	}
	// Defining a name while others are sending is the boot of a second module,
	// and it must not race with them.
	wg.Add(1)
	go func() {
		defer wg.Done()
		hub.Pipeline("late", func(p *pipeline.Pipeline[string], passable string) (string, error) {
			return passable, nil
		})
	}()
	wg.Wait()
}
