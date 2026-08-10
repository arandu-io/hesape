package console_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/config"
	"github.com/arandu-io/hesape/console"
)

func TestExitCarriesTheStatusAndTheMessage(t *testing.T) {
	err := console.Exit(3, "the queue %q is empty", "default")

	var exit *console.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("Exit returned %T, want *console.ExitError", err)
	}
	if exit.Code != 3 {
		t.Errorf("Code = %d, want 3", exit.Code)
	}
	if got, want := exit.Error(), `the queue "default" is empty`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestExitRefusesZero: an error that reports success would end the command
// while telling the shell everything went well.
func TestExitRefusesZero(t *testing.T) {
	if got := console.ExitCode(console.Exit(0, "it failed")); got != 1 {
		t.Errorf("ExitCode of Exit(0) = %d, want 1", got)
	}
}

func TestExitErrorWithoutMessageStillReads(t *testing.T) {
	err := &console.ExitError{Code: 2}
	if got, want := err.Error(), "exit status 2"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestExitCode(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"nil is success":     {err: nil, want: 0},
		"plain error is one": {err: errors.New("broken"), want: 1},
		"exit error":         {err: console.Exit(7, "no"), want: 7},
		"wrapped exit error": {err: fmt.Errorf("running migrate: %w", console.Exit(7, "no")), want: 7},
	} {
		t.Run(name, func(t *testing.T) {
			if got := console.ExitCode(tc.err); got != tc.want {
				t.Errorf("ExitCode = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestGuardEnvAllowsTheEnvironmentItWasGiven(t *testing.T) {
	if err := console.GuardEnv(config.EnvDev, config.EnvDev, "dropping every table"); err != nil {
		t.Fatalf("GuardEnv refused the environment it wanted: %v", err)
	}
}

// TestGuardEnvRefusesElsewhere is the check that stands between migrate:fresh
// and a production database.
func TestGuardEnvRefusesElsewhere(t *testing.T) {
	err := console.GuardEnv(config.EnvProd, config.EnvDev, "dropping every table")
	if err == nil {
		t.Fatal("GuardEnv allowed a development-only action in production")
	}
	if got := console.ExitCode(err); got != 1 {
		t.Errorf("ExitCode = %d, want 1", got)
	}
	for _, want := range []string{"dropping every table", "prod", "dev"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message does not say %q: %s", want, err)
		}
	}
}
