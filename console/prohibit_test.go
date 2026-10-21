package console_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/console"
)

// wiping is a destructive command that records whether it ran.
func wiping(ran *bool) console.Command {
	return console.Command{
		Name:        "db:wipe",
		Description: "drop every table",
		Run:         func(context.Context, *console.IO) error { *ran = true; return nil },
	}
}

// TestAProhibitedCommandRefusesInsteadOfRunning: it stays listed, and running
// it is an exit error that says why, with the work not done.
func TestAProhibitedCommandRefusesInsteadOfRunning(t *testing.T) {
	var ran bool
	r, _, _ := newRegistry(t, "")
	r.Add(wiping(&ran)).Prohibit("db:wipe")

	err := r.Handle(context.Background(), []string{"db:wipe"})
	if console.ExitCode(err) != 1 || !strings.Contains(err.Error(), "prohibited") {
		t.Fatalf("err = %v, want the refusal", err)
	}
	if ran {
		t.Fatal("a prohibited command ran")
	}
	if !r.Has("db:wipe") {
		t.Fatal("a prohibited command left the listing")
	}
	if err := r.Call(context.Background(), "db:wipe"); err == nil || ran {
		t.Fatalf("Call ran a prohibited command: err = %v", err)
	}
}

// TestAProhibitionReachesOneApplication: a second application built in the
// same process runs the command.
func TestAProhibitionReachesOneApplication(t *testing.T) {
	var ran bool
	prohibiting, _, _ := newRegistry(t, "")
	prohibiting.Add(wiping(new(bool))).Prohibit("db:wipe")
	other, _, _ := newRegistry(t, "")
	other.Add(wiping(&ran))

	if err := other.Handle(context.Background(), []string{"db:wipe"}); err != nil || !ran {
		t.Fatalf("the other application refused: err = %v, ran = %v", err, ran)
	}
}

// TestThePackageLevelProhibitionIsEnforced: the deprecated Prohibit said it
// stopped a command and nothing read it on the way in, so the guard an
// application turned on in production stopped nothing.
func TestThePackageLevelProhibitionIsEnforced(t *testing.T) {
	console.Prohibit("db:wipe")
	t.Cleanup(func() { console.Prohibit("db:wipe", false) })

	var ran bool
	r, _, _ := newRegistry(t, "")
	r.Add(wiping(&ran))
	if err := r.Handle(context.Background(), []string{"db:wipe"}); err == nil || ran {
		t.Fatalf("a command prohibited for the process ran: err = %v", err)
	}
}
