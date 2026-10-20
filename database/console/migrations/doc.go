// Package migrations holds the eight migration commands as console.Command
// values, with the flags --database, --path, --pretend, --step, --batch, --seed
// and --force. Commands(deps) answers them all.
//
// migrate, migrate:rollback, migrate:reset, migrate:refresh, migrate:fresh and
// db:wipe all name the same isolation lock, because `aru migrate` is a pipeline
// step and two of them against one database is a race. A registry with no lock
// issuer refuses to run them rather than running them unprotected.
//
// migrate:fresh and db:wipe drop every table, and both refuse to run unless the
// application handed over a Wipe deliberately. "Drop everything" is not a
// capability a framework should assume it has.
//
// migrate:fresh, migrate:reset and migrate:refresh each empty the whole schema,
// and no argument narrows them, so they run only when Deps.Environment is
// config.EnvDev. The refusal is the command's own: a binary that wires these
// commands cannot forget it, and migrate:rollback -- one batch, chosen by a
// person -- stays available everywhere.
//
// Resolving the migration paths is the pathsOf helper rather than a shared base:
// Go has embedding, and one helper is a function.
package migrations
