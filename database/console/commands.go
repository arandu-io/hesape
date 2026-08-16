package console

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/arandu-io/hesape/console"
	"github.com/arandu-io/hesape/database"
	dbevents "github.com/arandu-io/hesape/database/events"
)

// Deps is what the database commands need: a connection resolver and a
// dispatcher.
//
// They are a value the application builds where it wires everything else, which
// is also what makes every command below testable with a fake resolver and no
// database.
type Deps struct {
	// Connections is the resolver the commands read through.
	Connections database.ConnectionResolverInterface

	// Events is where DatabaseBusy is dispatched, for db:monitor.
	Events database.Dispatcher

	// Wipe drops every table on a connection. db:wipe refuses to run without
	// it, because "drop everything" is not a capability a framework should
	// assume it has.
	Wipe func(ctx context.Context, connection string) error

	// Tables answers the tables of a connection with their row counts, for
	// db:show and db:table. It is a function because the catalogue query is
	// the schema package's business and this package does not depend on it.
	Tables func(ctx context.Context, connection string) ([]TableInfo, error)

	// Prunables answers what model:prune should prune. Each entry prunes and
	// answers how many rows it removed.
	Prunables map[string]func(ctx context.Context) (int, error)

	// Environment is what the destructive commands check before they run.
	Environment string
}

// TableInfo is one row of the db:show table listing: schema, name, row
// count and size, read off the schema builder.
type TableInfo struct {
	// Schema is the schema the table lives in, empty where the engine has none.
	Schema string

	// Name is the table name.
	Name string

	// Rows is the row count, or -1 when it was not asked for.
	Rows int64

	// Size is the table's size in bytes, or -1 when the engine does not say.
	Size int64
}

// Commands builds every database command in this package against one Deps, so
// an application registers the whole group in a single call rather than naming
// each command and threading the same dependencies through all six.
//
// The group is db, db:show, db:table, db:monitor, db:wipe and model:prune. The
// migration and seed commands are not here: they need a Migrator and a seeder
// registry this package does not have, and they are built by
// database/console/migrations and database/console/seeds.
func Commands(deps Deps) []console.Command {
	return []console.Command{
		DbCommand(deps),
		ShowCommand(deps),
		TableCommand(deps),
		MonitorCommand(deps),
		WipeCommand(deps),
		PruneCommand(deps),
	}
}

// DbCommand builds `aru db`, which prints the invocation of the engine's own
// CLI -- mysql, psql, sqlite3 -- with the password left in an environment
// variable so it does not reach the process list.
//
// It prints the command instead of running it, and the difference is
// deliberate: a framework that execs a binary it did not ship, with
// credentials, from a directory it does not control, is a framework with a
// supply chain problem. Printing it means the person runs their own client,
// with their own history and their own .psqlrc.
func DbCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "db",
		Description: "print the command that opens a database CLI session",
		Run: func(_ context.Context, o *console.IO) error {
			flags := o.Flags()
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			name := ""
			if args := flags.Args(); len(args) > 0 {
				name = args[0]
			}

			connection, err := deps.Connections.Connection(name)
			if err != nil {
				return err
			}

			concrete, ok := connection.(*database.Connection)
			if !ok {
				return fmt.Errorf("db: this connection cannot describe itself")
			}

			o.Line("%s", GetCommand(concrete))
			o.NewLine()
			o.Comment("The password is not printed. Read it from the same place the application does.")
			return nil
		},
	}
}

// GetCommand returns the client invocation for a connection.
//
// It never prints the password: that is passed through the environment
// instead, because a printed command lands in a shell history file that
// several people can read.
func GetCommand(connection *database.Connection) string {
	client := "sqlite3"
	switch connection.GetDriverName() {
	case string(database.DialectPostgres):
		client = "psql"
	case string(database.DialectMySQL):
		client = "mysql"
	}
	return client + " " + strings.Join(CommandArguments(connection), " ")
}

// CommandArguments returns the argument list for the engine's client, as a
// slice rather than one string.
//
// GetCommand joins these for printing; this is what a caller that wants to run
// the client itself passes to exec.Command, where a joined string would have to
// be re-split -- and re-splitting a command line is how a database name with a
// space in it becomes two arguments.
func CommandArguments(connection *database.Connection) []string {
	host := asText(connection.GetConfig("host"))
	port := asText(connection.GetConfig("port"))
	user := asText(connection.GetConfig("username"))
	name := connection.GetDatabaseName()

	switch connection.GetDriverName() {
	case string(database.DialectPostgres):
		return []string{"--host=" + host, "--port=" + port, "--username=" + user, "--dbname=" + name}
	case string(database.DialectMySQL):
		// --password with no value makes the client prompt, so the password
		// never reaches the process list. See CommandEnvironment for the
		// other half of that.
		return []string{"--host=" + host, "--port=" + port, "--user=" + user, "--password", name}
	default:
		return []string{name}
	}
}

// CommandEnvironment returns the variables the client reads a password out
// of, so it never reaches the process list.
//
// It returns the names and not the values, because the caller has the
// connection and this package should not be copying secrets around to be
// helpful.
func CommandEnvironment(connection *database.Connection) []string {
	switch connection.GetDriverName() {
	case string(database.DialectPostgres):
		return []string{"PGPASSWORD"}
	case string(database.DialectMySQL):
		return []string{"MYSQL_PWD"}
	default:
		return nil
	}
}

// ShowCommand builds `aru db:show`.
func ShowCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "db:show",
		Description: "display information about the given database",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			name := flags.String("database", "", "the database connection")
			counts := flags.Bool("counts", false, "show the table row counts")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			connection, err := deps.Connections.Connection(*name)
			if err != nil {
				return err
			}
			concrete, ok := connection.(*database.Connection)
			if !ok {
				return fmt.Errorf("db:show: this connection cannot describe itself")
			}

			version, err := concrete.GetServerVersion(ctx)
			if err != nil {
				return err
			}

			o.TwoColumnDetail("Driver", database.DriverTitle(concrete.GetDriverName()))
			o.TwoColumnDetail("Version", version)
			o.TwoColumnDetail("Database", concrete.GetDatabaseName())
			o.TwoColumnDetail("Host", asText(concrete.GetConfig("host")))
			o.NewLine()

			if deps.Tables == nil {
				o.Comment("This application registered no table listing, so no tables are shown.")
				return nil
			}

			tables, err := deps.Tables(ctx, *name)
			if err != nil {
				return err
			}
			sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })

			rows := make([][]string, 0, len(tables))
			for _, table := range tables {
				row := []string{table.Name}
				if *counts {
					row = append(row, strconv.FormatInt(table.Rows, 10))
				}
				rows = append(rows, row)
			}

			headers := []string{"Table"}
			if *counts {
				headers = append(headers, "Rows")
			}
			o.Table(headers, rows)
			return nil
		},
	}
}

// TableCommand builds `aru db:table`, which prints one table's name, row count
// and size on disk.
//
// The table is the first argument. Called with none, it lists the tables of the
// connection and asks which one, so the command is usable without knowing the
// schema by heart. It reads through Deps.Tables and fails with a message
// saying so when the application registered none.
func TableCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "db:table",
		Description: "display information about the given database table",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			name := flags.String("database", "", "the database connection")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			if deps.Tables == nil {
				return fmt.Errorf("db:table needs a table listing, and this application registered none")
			}

			tables, err := deps.Tables(ctx, *name)
			if err != nil {
				return err
			}

			wanted := ""
			if args := flags.Args(); len(args) > 0 {
				wanted = args[0]
			}
			if wanted == "" {
				names := make([]string, 0, len(tables))
				for _, table := range tables {
					names = append(names, table.Name)
				}
				sort.Strings(names)

				chosen, err := o.Choice("Which table?", names, "")
				if err != nil {
					return err
				}
				wanted = chosen
			}

			for _, table := range tables {
				if table.Name != wanted {
					continue
				}
				o.TwoColumnDetail("Table", table.Name)
				o.TwoColumnDetail("Rows", strconv.FormatInt(table.Rows, 10))
				if table.Size >= 0 {
					o.TwoColumnDetail("Size", strconv.FormatInt(table.Size, 10)+" bytes")
				}
				return nil
			}

			return fmt.Errorf("table [%s] does not exist", wanted)
		},
	}
}

// MonitorCommand builds `aru db:monitor`.
//
// It dispatches DatabaseBusy when a connection is over its threshold, which is
// the event an alerting listener subscribes to. The threshold has no default: a
// number that means "too many" is per database and per plan, and a framework
// guessing it would page somebody at three in the morning about a number it
// made up.
func MonitorCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "db:monitor",
		Description: "monitor the number of connections on the specified database",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			names := flags.String("databases", "", "the database connections to monitor, comma separated")
			max := flags.Int("max", 0, "the maximum number of connections that can be open")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			for _, name := range splitList(*names) {
				connection, err := deps.Connections.Connection(name)
				if err != nil {
					return err
				}
				concrete, ok := connection.(*database.Connection)
				if !ok {
					continue
				}

				count, err := concrete.ThreadCount(ctx)
				if err != nil {
					return err
				}

				o.TwoColumnDetail(name, strconv.FormatInt(count, 10)+" open connections")

				if *max > 0 && count > int64(*max) && deps.Events != nil {
					deps.Events.Dispatch(dbevents.NewDatabaseBusy(name, int(count)))
				}
			}
			return nil
		},
	}
}

// WipeCommand builds `aru db:wipe`.
func WipeCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "db:wipe",
		Description: "drop all tables, views, and types",
		Isolated:    "migrate",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			name := flags.String("database", "", "the database connection to use")
			force := flags.Bool("force", false, "force the operation to run when in production")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			if deps.Wipe == nil {
				return fmt.Errorf("db:wipe drops every table, and this application did not register a Wipe for it")
			}

			if deps.Environment == "production" && !*force {
				confirmed, err := o.Confirm("The application is in production. Drop every table anyway?", false)
				if err != nil {
					return err
				}
				if !confirmed {
					o.Comment("Command cancelled.")
					return nil
				}
			}

			if err := deps.Wipe(ctx, *name); err != nil {
				return err
			}
			o.Info("Dropped all tables successfully.")
			return nil
		},
	}
}

// PruneCommand builds `aru model:prune`.
//
// There is no directory scan for prunable models -- a type nothing references
// is not in the binary -- so an application registers what it wants pruned,
// which is also the list somebody can read to find out what this command will
// delete.
func PruneCommand(deps Deps) console.Command {
	return console.Command{
		Name:        "model:prune",
		Description: "prune rows that are no longer needed",
		Isolated:    "model:prune",
		Run: func(ctx context.Context, o *console.IO) error {
			flags := o.Flags()
			pretend := flags.Bool("pretend", false, "list what would be pruned, and prune nothing")
			if err := flags.Parse(o.Args()); err != nil {
				return err
			}

			names := make([]string, 0, len(deps.Prunables))
			for name := range deps.Prunables {
				names = append(names, name)
			}
			sort.Strings(names)

			if len(names) == 0 {
				o.Info("No prunable models found.")
				return nil
			}

			if deps.Events != nil {
				deps.Events.Dispatch(dbevents.NewModelPruningStarting(names))
			}

			for _, name := range names {
				if *pretend {
					o.TwoColumnDetail(name, "would be pruned")
					continue
				}

				count, err := deps.Prunables[name](ctx)
				if err != nil {
					return err
				}

				o.TwoColumnDetail(name, strconv.Itoa(count)+" rows")

				if deps.Events != nil {
					deps.Events.Dispatch(dbevents.NewModelsPruned(name, count))
				}
			}

			if deps.Events != nil {
				deps.Events.Dispatch(dbevents.NewModelPruningFinished(names))
			}
			return nil
		},
	}
}

func splitList(value string) []string {
	var out []string
	current := ""
	for _, c := range value {
		if c == ',' {
			if current != "" {
				out = append(out, current)
			}
			current = ""
			continue
		}
		current += string(c)
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

func asText(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	default:
		return fmt.Sprint(v)
	}
}
