package seeds

import (
	"context"
	"fmt"
	"strings"

	"github.com/arandu-io/hesape/console"
)

// Deps is what the seed commands need.
//
// Seed is the application's own database.Seed call, closed over the registry
// and the Deps a seeder receives -- neither of which this package can know,
// because what a seeder is allowed to touch is the application's answer and not
// the framework's.
type Deps struct {
	// Seed runs the named seeder, or the application's root one when the name
	// is empty, and answers the name that ran.
	Seed func(ctx context.Context, name string, args []string) (string, error)

	// Environment is what `db:seed` checks before it runs. It is a string
	// rather than a lookup, so a test can set it.
	Environment string

	// SeederPath is where make:seeder writes.
	SeederPath string
}

// Commands builds the two seed commands against one Deps, so an application
// registers both in a single call: db:seed, which runs a seeder, and
// make:seeder, which writes a new one.
func Commands(deps Deps) []console.Command {
	return []console.Command{SeedCommand(deps), SeederMakeCommand(deps)}
}

// SeedCommand builds `aru db:seed`.
//
// The confirmation in production is not a formality: a seeder is written
// against an empty database and run against a full one exactly once before
// somebody learns why.
func SeedCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "db:seed",
		Description: "seed the database with records",
		Run: func(ctx context.Context, o *console.IO) error {
			// Before the flags are parsed, because --class= is not a flag here
			// and the parser refuses an undefined one with "flag provided but
			// not defined: -class" -- which is true and useless. The sentence
			// below names the word to type instead, and it was unreachable
			// through this command until the check moved above the parse.
			if err := refuseTheClassFlag(o.Args()); err != nil {
				return err
			}

			flags := o.Flags()
			force := flags.Bool("force", false, "force the operation to run when in production")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			if deps.Environment == "production" && !*force {
				confirmed, err := o.Confirm("The application is in production. Seed the database anyway?", false)
				if err != nil {
					return err
				}
				if !confirmed {
					o.Comment("Command cancelled.")
					return nil
				}
			}

			if deps.Seed == nil {
				return fmt.Errorf("this application registered no seeders")
			}

			// The name is positional, and --class= is refused rather than
			// accepted: a name that is sometimes a flag and sometimes a word is
			// two spellings of one thing. database.Seed says the same with the
			// same sentence.
			args := flags.Args()
			name := ""
			if len(args) > 0 {
				name, args = args[0], args[1:]
			}

			ran, err := deps.Seed(ctx, name, args)
			if err != nil {
				return err
			}

			o.Info("Database seeding completed successfully.")
			o.TwoColumnDetail(ran, "DONE")
			return nil
		},
	}
}

// refuseTheClassFlag answers the spelling Laravel uses with the one this does.
//
// A name that is sometimes a flag and sometimes a word is two spellings of one
// thing, and the message is what somebody reads and retypes -- so it comes from
// one place. database.Seed refuses it too, for the caller that reaches the
// engine without going through this command.
func refuseTheClassFlag(args []string) error {
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--class="); ok {
			return fmt.Errorf("--class= is not how a seeder is named any more.\n\n    aru db:seed %s", value)
		}
		if arg == "--class" || arg == "-class" {
			return fmt.Errorf("--class= is not how a seeder is named any more.\n\n    aru db:seed <SeederName>")
		}
	}
	return nil
}

// SeederMakeCommand builds `aru make:seeder`.
//
// It prints the seeder rather than writing it: a seeder here is a value in a
// registry the application declares, so the file's location is the
// application's business and the only thing the generator can usefully supply
// is the shape.
func SeederMakeCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "make:seeder",
		Description: "create a new seeder class",
		Run: func(_ context.Context, o *console.IO) error {
			flags := o.Flags()
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			args := flags.Args()
			if len(args) == 0 {
				return fmt.Errorf("make:seeder needs a name\n\n    aru make:seeder UserSeeder")
			}
			name := args[0]

			o.Line("%s", seederStub(name))
			o.NewLine()
			o.Comment("Add it to the seeder registry in %s", orDefault(deps.SeederPath, "database/seeders"))
			return nil
		},
	}
}

// seederStub is what make:seeder prints.
func seederStub(name string) string {
	return fmt.Sprintf(`// %s seeds the database.
//
// It must be safe to run twice: a seeder that fails on the second run cannot
// be part of a deploy.
type %s struct{}

// Name is how the seeder is addressed on the command line.
func (%s) Name() string { return %q }

// Run performs the seeding.
func (%s) Run(ctx context.Context, d Deps) error {
	return nil
}`, name, name, name, name, name)
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
