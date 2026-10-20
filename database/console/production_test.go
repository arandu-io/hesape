package console_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/config"
	"github.com/arandu-io/hesape/console"
	dbconsole "github.com/arandu-io/hesape/database/console"
)

// TestWipeAsksInProduction: the environment an application passes is its
// config.Env, and production is config.EnvProd. The check compared against the
// literal "production", which no configuration ever produces, so db:wipe
// dropped every table in production without asking.
func TestWipeAsksInProduction(t *testing.T) {
	wiped := false
	deps := dbconsole.Deps{
		Wipe: func(context.Context, string) error {
			wiped = true
			return nil
		},
		Environment: string(config.EnvProd),
	}
	wipe := func(input string, args ...string) string {
		t.Helper()
		var out bytes.Buffer
		app := console.NewApplication(&out, &out, strings.NewReader(input))
		app.WithLocks(cache.NewLocks(cache.NewArrayStore()), time.Minute)
		app.Add(dbconsole.Commands(deps)...)
		if err := app.Call(context.Background(), "db:wipe", args...); err != nil {
			t.Fatalf("db:wipe %v: %v", args, err)
		}
		return out.String() + app.Output()
	}

	printed := wipe("no\n")
	if wiped {
		t.Fatal("db:wipe dropped the tables in production without the confirmation being given")
	}
	if !strings.Contains(printed, "in production") {
		t.Errorf("the question was not asked: %q", printed)
	}

	wipe("", "--force")
	if !wiped {
		t.Error("db:wipe --force did not wipe in production")
	}

	wiped = false
	deps.Environment = string(config.EnvDev)
	wipe("")
	if !wiped {
		t.Error("db:wipe did not wipe in development")
	}
}
