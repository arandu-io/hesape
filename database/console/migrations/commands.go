package migrations

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/database/migrations"
)

// Deps is what the migration commands need to do their work.
//
// In PHP each command takes its collaborators through the constructor and the
// container fills them in. There is no container (ADR 0001), so the
// collaborators are a value the application builds where it wires everything
// else -- which is also what makes every command below testable with a fake
// migrator and no database.
type Deps struct {
	// Migrator is the migrator the commands drive.
	Migrator *migrations.Migrator

	// Creator is what MigrateMakeCommand writes files with.
	Creator *migrations.MigrationCreator

	// MigrationPath is where make:migration writes, and the group
	// `aru migrate` reads when no --path is given.
	MigrationPath string

	// Seed runs the seeders after a migration, for --seed. Nil means the
	// application wired no seeders, and --seed then says so rather than
	// quietly doing nothing.
	Seed func(ctx context.Context, name string) error

	// Wipe drops every table, for migrate:fresh. It is a function rather than
	// a schema builder because the schema package is not a dependency of this
	// one, and because "drop everything" is the one operation an application
	// should have to hand over deliberately.
	Wipe func(ctx context.Context, connection string) error
}

// Commands answers the ten commands of
// Illuminate\Database\Console\Migrations, as values the console registry takes.
//
// PHP discovers commands by scanning a namespace; here they are a slice,
// because the listing and the compiler should read the same thing -- a command
// missing from it does not exist, and one in it with a broken Run does not
// build.
func Commands(deps Deps) []console.Command {
	return []console.Command{
		MigrateCommand(deps),
		RollbackCommand(deps),
		ResetCommand(deps),
		RefreshCommand(deps),
		FreshCommand(deps),
		StatusCommand(deps),
		InstallCommand(deps),
		MigrateMakeCommand(deps),
	}
}

// MigrateCommand answers
// Illuminate\Database\Console\Migrations\MigrateCommand: `aru migrate`.
//
// It is isolated, which is Laravel's `implements Isolatable` and RULE 16's
// other half: two migrators against one database is the failure the rule exists
// to prevent, and naming the lock is what stops the second one starting.
func MigrateCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "migrate",
		Description: "run the database migrations",
		Isolated:    "migrate",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			database := flags.String("database", "", "the database connection to use")
			pretend := flags.Bool("pretend", false, "dump the SQL queries that would be run")
			step := flags.Bool("step", false, "force the migrations to be run so they can be rolled back individually")
			seed := flags.Bool("seed", false, "indicates if the seed task should be re-run")
			seeder := flags.String("seeder", "", "the name of the root seeder")
			path := flags.String("path", "", "the path to the migrations")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			migrator := deps.Migrator
			migrator.SetOutput(o.Flags().Output())
			migrator.SetConnection(*database)

			if !migrator.RepositoryExists(ctx) {
				if err := migrator.GetRepository().CreateRepository(ctx); err != nil {
					return err
				}
				o.Info("Migration table created successfully.")
			}

			ran, err := migrator.Run(ctx, pathsOf(*path, deps.MigrationPath), migrations.Options{
				Pretend: *pretend,
				Step:    *step,
			})
			if err != nil {
				return err
			}

			for _, name := range ran {
				o.TwoColumnDetail(name, "DONE")
			}

			if *seed && !*pretend {
				if deps.Seed == nil {
					return fmt.Errorf("--seed was given and this application registered no seeders")
				}
				return deps.Seed(ctx, *seeder)
			}
			return nil
		},
	}
}

// RollbackCommand builds `aru migrate:rollback`, which undoes the last batch of
// migrations by running their Down in reverse order.
//
// The unit is the batch, not the migration: everything `aru migrate` applied in
// one run comes off in one rollback. --step=N undoes that many migrations
// instead, and --batch=N undoes one named batch. --pretend prints the
// statements and runs none. It is isolated under the "migrate" lock, so two
// deploys cannot roll back the same batch at once, and it says so and stops
// when the migration table does not exist yet.
//
// Answers Illuminate\Database\Console\Migrations\RollbackCommand.
func RollbackCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "migrate:rollback",
		Description: "roll back the last database migration",
		Isolated:    "migrate",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			database := flags.String("database", "", "the database connection to use")
			pretend := flags.Bool("pretend", false, "dump the SQL queries that would be run")
			step := flags.Int("step", 0, "the number of migrations to be reverted")
			batch := flags.Int("batch", 0, "the batch of migrations to be reverted")
			path := flags.String("path", "", "the path to the migrations")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			migrator := deps.Migrator
			migrator.SetOutput(flags.Output())
			migrator.SetConnection(*database)

			if !migrator.RepositoryExists(ctx) {
				o.Comment("Migration table not found.")
				return nil
			}

			reverted, err := migrator.Rollback(ctx, pathsOf(*path, deps.MigrationPath), migrations.Options{
				Pretend: *pretend,
				Steps:   *step,
				Batch:   *batch,
			})
			if err != nil {
				return err
			}
			for _, name := range reverted {
				o.TwoColumnDetail(name, "REVERTED")
			}
			return nil
		},
	}
}

// ResetCommand builds `aru migrate:reset`, which rolls back every migration
// that ever ran, newest first, leaving an empty schema.
//
// It is rollback without a stopping point: every Down, in one pass. That makes
// it a development command -- on a database with data in it, the Downs are what
// drop the tables. --pretend prints the statements and runs none, which is the
// only safe way to point it at anything shared. It is isolated under the
// "migrate" lock and does nothing when the migration table does not exist.
//
// Answers Illuminate\Database\Console\Migrations\ResetCommand.
func ResetCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "migrate:reset",
		Description: "roll back all database migrations",
		Isolated:    "migrate",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			database := flags.String("database", "", "the database connection to use")
			pretend := flags.Bool("pretend", false, "dump the SQL queries that would be run")
			path := flags.String("path", "", "the path to the migrations")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			migrator := deps.Migrator
			migrator.SetOutput(flags.Output())
			migrator.SetConnection(*database)

			if !migrator.RepositoryExists(ctx) {
				o.Comment("Migration table not found.")
				return nil
			}

			_, err := migrator.Reset(ctx, pathsOf(*path, deps.MigrationPath), *pretend)
			return err
		},
	}
}

// RefreshCommand answers
// Illuminate\Database\Console\Migrations\RefreshCommand: `aru migrate:refresh`.
//
// It resets and re-runs, which is not the same as fresh: refresh goes through
// every Down, so a migration whose Down is wrong fails here rather than
// silently never being exercised.
func RefreshCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "migrate:refresh",
		Description: "reset and re-run all migrations",
		Isolated:    "migrate",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			database := flags.String("database", "", "the database connection to use")
			step := flags.Int("step", 0, "the number of migrations to be reverted and re-run")
			seed := flags.Bool("seed", false, "indicates if the seed task should be re-run")
			seeder := flags.String("seeder", "", "the name of the root seeder")
			path := flags.String("path", "", "the path to the migrations")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			migrator := deps.Migrator
			migrator.SetOutput(flags.Output())
			migrator.SetConnection(*database)

			paths := pathsOf(*path, deps.MigrationPath)

			if *step > 0 {
				if _, err := migrator.Rollback(ctx, paths, migrations.Options{Steps: *step}); err != nil {
					return err
				}
			} else if _, err := migrator.Reset(ctx, paths, false); err != nil {
				return err
			}

			if _, err := migrator.Run(ctx, paths, migrations.Options{}); err != nil {
				return err
			}

			if *seed {
				if deps.Seed == nil {
					return fmt.Errorf("--seed was given and this application registered no seeders")
				}
				return deps.Seed(ctx, *seeder)
			}
			return nil
		},
	}
}

// FreshCommand answers
// Illuminate\Database\Console\Migrations\FreshCommand: `aru migrate:fresh`.
//
// It drops every table and migrates from nothing, which is why it refuses to
// run without a Wipe the application handed over deliberately: "drop
// everything" is not a capability a framework should assume it has.
func FreshCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "migrate:fresh",
		Description: "drop all tables and re-run all migrations",
		Isolated:    "migrate",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			database := flags.String("database", "", "the database connection to use")
			seed := flags.Bool("seed", false, "indicates if the seed task should be re-run")
			seeder := flags.String("seeder", "", "the name of the root seeder")
			step := flags.Bool("step", false, "force the migrations to be run so they can be rolled back individually")
			path := flags.String("path", "", "the path to the migrations")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			if deps.Wipe == nil {
				return fmt.Errorf("migrate:fresh drops every table, and this application did not register a Wipe for it")
			}
			if err := deps.Wipe(ctx, *database); err != nil {
				return err
			}
			o.Info("Dropped all tables successfully.")

			migrator := deps.Migrator
			migrator.SetOutput(flags.Output())
			migrator.SetConnection(*database)

			if err := migrator.GetRepository().CreateRepository(ctx); err != nil {
				return err
			}

			if _, err := migrator.Run(ctx, pathsOf(*path, deps.MigrationPath), migrations.Options{Step: *step}); err != nil {
				return err
			}

			if *seed {
				if deps.Seed == nil {
					return fmt.Errorf("--seed was given and this application registered no seeders")
				}
				return deps.Seed(ctx, *seeder)
			}
			return nil
		},
	}
}

// StatusCommand builds `aru migrate:status`, which prints a table of every
// migration file with its batch number and whether it has run.
//
// It compares the files on disk against the rows in the migration table, so it
// answers the question a deploy asks before it starts: is this database at the
// version this binary expects. --pending lists only what has not run yet. It
// changes nothing, which is why it takes no isolation lock.
//
// Answers Illuminate\Database\Console\Migrations\StatusCommand.
func StatusCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "migrate:status",
		Description: "show the status of each migration",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			database := flags.String("database", "", "the database connection to use")
			pending := flags.Bool("pending", false, "only list pending migrations")
			path := flags.String("path", "", "the path to the migrations")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			migrator := deps.Migrator
			migrator.SetConnection(*database)

			if !migrator.RepositoryExists(ctx) {
				o.Comment("Migration table not found.")
				return nil
			}

			batches, err := migrator.GetRepository().GetMigrationBatches(ctx)
			if err != nil {
				return err
			}

			files := migrator.GetMigrationFiles(pathsOf(*path, deps.MigrationPath))

			names := make([]string, 0, len(files))
			for name := range files {
				names = append(names, name)
			}
			sortStrings(names)

			rows := make([][]string, 0, len(names))
			for _, name := range names {
				batch, ran := batches[name]
				if *pending && ran {
					continue
				}
				status := "Pending"
				if ran {
					status = "Ran"
				}
				rows = append(rows, []string{name, strconv.Itoa(batch), status})
			}

			o.Table([]string{"Migration", "Batch", "Status"}, rows)
			return nil
		},
	}
}

// InstallCommand builds `aru migrate:install`, which creates the table the
// migrator records applied migrations in.
//
// It exists to create that table on its own, ahead of anything else -- for a
// database where the schema is loaded from a dump and the migrator only has to
// know what has already run. `aru migrate` creates the table itself when it is
// missing, so nothing normally needs this.
//
// Answers Illuminate\Database\Console\Migrations\InstallCommand.
func InstallCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "migrate:install",
		Description: "create the migration repository",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			database := flags.String("database", "", "the database connection to use")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			repository := deps.Migrator.GetRepository()
			repository.SetSource(*database)

			if err := repository.CreateRepository(ctx); err != nil {
				return err
			}
			o.Info("Migration table created successfully.")
			return nil
		},
	}
}

// MigrateMakeCommand answers
// Illuminate\Database\Console\Migrations\MigrateMakeCommand:
// `aru make:migration`.
//
// The name is snake case, and the table is guessed from it exactly as the PHP
// guesses -- create_invoices_table gives a CREATE stub for invoices,
// add_paid_at_to_invoices gives an ALTER stub for the same table.
func MigrateMakeCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "make:migration",
		Description: "create a new migration file",
		Run: func(_ context.Context, o *console.IO) error {
			flags := o.Flags()
			create := flags.String("create", "", "the table to be created")
			table := flags.String("table", "", "the table to migrate")
			path := flags.String("path", "", "the location where the migration file should be created")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			rest := flags.Args()
			if len(rest) == 0 {
				return fmt.Errorf("make:migration needs a name\n\n    aru make:migration create_invoices_table")
			}
			name := strings.TrimSpace(rest[0])

			creating := *create != ""
			guessedTable := *table
			if creating {
				guessedTable = *create
			}

			if guessedTable == "" {
				if guess, guessedCreate, ok := Guess(name); ok {
					guessedTable, creating = guess, guessedCreate
				}
			}

			target := *path
			if target == "" {
				target = deps.MigrationPath
			}

			written, err := deps.Creator.Create(name, target, guessedTable, creating)
			if err != nil {
				return err
			}

			o.Info("Created migration [%s]", written)
			o.Comment("Register it by blank-importing the package it lives in, from main.go")
			return nil
		},
	}
}

// pathsOf answers BaseCommand::getMigrationPaths: the --path when it was given,
// and the application's own otherwise.
func pathsOf(path, fallback string) []string {
	if path != "" {
		return strings.Split(path, ",")
	}
	if fallback == "" {
		return nil
	}
	return []string{fallback}
}

// sortStrings keeps the status table in one order on every run, which a map
// range does not.
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
