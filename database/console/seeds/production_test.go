package seeds_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/config"
	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/database/console/seeds"
)

// TestSeedAsksInProduction: the environment an application passes is its
// config.Env, and production is config.EnvProd. The check compared against the
// literal "production", which no configuration ever produces, so db:seed ran
// in production without asking.
func TestSeedAsksInProduction(t *testing.T) {
	seeded := false
	deps := seeds.Deps{
		Seed: func(_ context.Context, name string, _ []string) (string, error) {
			seeded = true
			return name, nil
		},
		Environment: string(config.EnvProd),
	}

	var out bytes.Buffer
	app := console.NewApplication(&out, &out, strings.NewReader("no\n"))
	app.Add(seeds.Commands(deps)...)
	if err := app.Call(context.Background(), "db:seed"); err != nil {
		t.Fatalf("db:seed: %v", err)
	}
	if seeded {
		t.Fatal("db:seed seeded in production without the confirmation being given")
	}
	if printed := out.String() + app.Output(); !strings.Contains(printed, "in production") {
		t.Errorf("the question was not asked: %q", printed)
	}

	// --force is the answer given in advance, and outside production there is
	// nothing to ask.
	for _, c := range []struct {
		env  config.Env
		args []string
	}{{config.EnvProd, []string{"--force"}}, {config.EnvDev, nil}, {config.EnvStaging, nil}} {
		seeded = false
		deps.Environment = string(c.env)
		app := console.NewApplication(&out, &out, strings.NewReader(""))
		app.Add(seeds.Commands(deps)...)
		if err := app.Call(context.Background(), "db:seed", c.args...); err != nil {
			t.Fatalf("db:seed in %s %v: %v", c.env, c.args, err)
		}
		if !seeded {
			t.Errorf("db:seed in %s %v did not seed", c.env, c.args)
		}
	}
}
