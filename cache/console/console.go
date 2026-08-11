package console

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/cache"
	"github.com/arandu-io/hesape/console"
)

// clearAction is the action the cache commands hold a system grant for.
//
// Every one of them writes -- clearing, forgetting, pruning -- and a grant is
// issued for one action and refused on any other (auth.Grant.Check), so naming
// it once here is what keeps the four commands from drifting into four spellings
// of the same permission.
const clearAction auth.Action = "cache:clear"

// ClearCommand flushes one tenant's cache.
//
// It answers Illuminate\Cache\Console\ClearCommand: `cache:clear`, with the
// store as an argument and --tags and --locks as options.
//
// It takes a tenant that Laravel's does not, and it is not optional: Flush
// empties one tenant's slice of one namespace, and a cache:clear that emptied
// the store would clear every other customer on the way past (RULE 14). In a
// SaaS that is an outage caused by a support request.
type ClearCommand struct {
	cache *cache.CacheManager
}

// NewClearCommand returns the command.
func NewClearCommand(m *cache.CacheManager) *ClearCommand { return &ClearCommand{cache: m} }

// Command is the registry entry for cache:clear.
func (c *ClearCommand) Command() console.Command {
	return console.Command{
		Name:        "cache:clear",
		Description: "flush the application cache for one tenant",
		Run:         c.Handle,
	}
}

// Handle runs the command.
//
// It answers ClearCommand::handle(), including the order: the locks branch comes
// first and does nothing else, because clearing the locks and clearing the cache
// are different operations and doing both would release a lock somebody is
// holding on the way to emptying a cache.
func (c *ClearCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	tenant := flags.String("tenant", "", "the tenant whose cache to clear")
	tags := flags.String("tags", "", "the cache tags to clear, comma separated")
	locks := flags.Bool("locks", false, "only clear cache locks")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	store := ""
	if rest := flags.Args(); len(rest) > 0 {
		store = rest[0]
	}

	repository, err := c.cache.Store(store)
	if err != nil {
		return err
	}

	if *locks {
		if *tags != "" {
			return errors.New("cache: tags cannot be used when clearing locks: a lock has no tenant and no tags")
		}
		if err := repository.FlushLocks(ctx); err != nil {
			return fmt.Errorf("cache: clearing the locks: %w", err)
		}
		o.Info("Application cache locks cleared successfully.")
		return nil
	}

	grant := auth.SystemGrant(clearAction, *tenant)
	if err := grant.Check(clearAction); err != nil {
		return err
	}

	if names := c.tags(*tags); len(names) > 0 {
		tagged, err := repository.Tags(names...)
		if err != nil {
			return err
		}
		if err := tagged.Flush(ctx, grant); err != nil {
			return err
		}
		o.Info("Application cache cleared successfully.")
		return nil
	}

	if err := repository.Flush(ctx, grant); err != nil {
		return fmt.Errorf("cache: clearing the cache: %w", err)
	}
	o.Info("Application cache cleared successfully.")
	return nil
}

// tags are the tags passed to the command.
//
// It answers ClearCommand::tags(): split on the comma, drop the empties.
func (c *ClearCommand) tags(option string) []string {
	out := make([]string, 0, 2)
	for _, name := range strings.Split(option, ",") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// ForgetCommand removes one key from the cache.
//
// It answers Illuminate\Cache\Console\ForgetCommand: `cache:forget {key}
// {store?}`, with the tenant added for the reason ClearCommand adds it.
type ForgetCommand struct {
	cache *cache.CacheManager
}

// NewForgetCommand returns the command.
func NewForgetCommand(m *cache.CacheManager) *ForgetCommand { return &ForgetCommand{cache: m} }

// Command is the registry entry for cache:forget.
func (c *ForgetCommand) Command() console.Command {
	return console.Command{
		Name:        "cache:forget",
		Description: "remove an item from the cache",
		Run:         c.Handle,
	}
}

// Handle runs the command. It answers ForgetCommand::handle().
func (c *ForgetCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	tenant := flags.String("tenant", "", "the tenant whose key to remove")
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	rest := flags.Args()
	if len(rest) == 0 {
		return errors.New("cache: cache:forget needs the key to remove")
	}
	key := rest[0]
	store := ""
	if len(rest) > 1 {
		store = rest[1]
	}

	repository, err := c.cache.Store(store)
	if err != nil {
		return err
	}

	grant := auth.SystemGrant(clearAction, *tenant)
	if err := grant.Check(clearAction); err != nil {
		return err
	}
	if err := repository.Forget(ctx, grant, key); err != nil {
		return err
	}

	o.Info("The [%s] key has been removed from the cache.", key)
	return nil
}

// PruneStaleTagsCommand removes the tag entries nothing points at any more.
//
// It answers Illuminate\Cache\Console\PruneStaleTagsCommand:
// `cache:prune-stale-tags {store?}`.
//
// It is a no-op on every store this package ships, and says so rather than
// pretending it did something. A tag generation here is an ordinary entry with a
// ttl, so it prunes itself; the store that keeps a set beside them, and
// therefore has something to prune, is the RESP one in arandu-io/kv -- which is
// exactly what Laravel's "(Redis only)" in the description means.
type PruneStaleTagsCommand struct {
	cache *cache.CacheManager
}

// NewPruneStaleTagsCommand returns the command.
func NewPruneStaleTagsCommand(m *cache.CacheManager) *PruneStaleTagsCommand {
	return &PruneStaleTagsCommand{cache: m}
}

// Command is the registry entry for cache:prune-stale-tags.
func (c *PruneStaleTagsCommand) Command() console.Command {
	return console.Command{
		Name:        "cache:prune-stale-tags",
		Description: "prune stale cache tags from the cache",
		Run:         c.Handle,
	}
}

// Handle runs the command. It answers PruneStaleTagsCommand::handle().
func (c *PruneStaleTagsCommand) Handle(ctx context.Context, o *console.IO) error {
	flags := o.Flags()
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	store := ""
	if rest := flags.Args(); len(rest) > 0 {
		store = rest[0]
	}

	repository, err := c.cache.Store(store)
	if err != nil {
		return err
	}

	flusher, ok := repository.GetStore().(interface{ FlushStaleTags(context.Context) error })
	if !ok {
		o.Comment("This cache store keeps no stale tags: nothing to prune.")
		return nil
	}
	if err := flusher.FlushStaleTags(ctx); err != nil {
		return err
	}

	o.Info("Stale cache tags pruned successfully.")
	return nil
}

// CacheTableCommand writes the migration for the cache tables.
//
// It answers Illuminate\Cache\Console\CacheTableCommand: `make:cache-table`,
// which Laravel also answers to as `cache:table`.
//
// It writes SQL and not a migration type, because there is no migration type to
// write against yet: hesape/database is being built in parallel and this command
// belongs to whoever needs the table today. When it lands, this is the file that
// changes.
type CacheTableCommand struct {
	// Directory is where the migration is written. Empty means the working
	// directory's database/migrations, which is where the rest of them live.
	Directory string
}

// NewCacheTableCommand returns the command.
func NewCacheTableCommand(directory string) *CacheTableCommand {
	return &CacheTableCommand{Directory: directory}
}

// Command is the registry entry for make:cache-table.
func (c *CacheTableCommand) Command() console.Command {
	return console.Command{
		Name:        "make:cache-table",
		Description: "create a migration for the cache database table",
		Run:         c.Handle,
	}
}

// Handle runs the command. It answers the migration
// MigrationGeneratorCommand::handle() writes for CacheTableCommand.
//
// It refuses to overwrite: a migration that has already run and is rewritten
// underneath is a schema nobody can reproduce.
func (c *CacheTableCommand) Handle(_ context.Context, o *console.IO) error {
	flags := o.Flags()
	if err := flags.Parse(o.Args()); err != nil {
		return err
	}

	directory := c.Directory
	if directory == "" {
		directory = filepath.Join("database", "migrations")
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}

	name := time.Now().UTC().Format("2006_01_02_150405") + "_create_cache_table.sql"
	path := filepath.Join(directory, name)

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("cache: %s already exists", path)
		}
		return err
	}
	defer func() { _ = file.Close() }()

	if _, err := file.WriteString(cacheTableMigration); err != nil {
		return err
	}

	o.Info("Migration created: %s", path)
	return nil
}

// cacheTableMigration is the two tables DatabaseStore reads and writes.
//
// It is Laravel's stub, column for column: key is the primary key, value holds
// the payload, and expiration is a unix timestamp with an index on it -- the
// index being what makes pruning the expired rows a scan of the ones that are.
//
// The one difference is that expiration is unix milliseconds in a BIGINT rather
// than unix seconds in an INTEGER. See cache.DatabaseStore for why.
const cacheTableMigration = `-- The cache tables.
--
-- cache holds the entries and cache_locks holds the locks. They are two tables
-- and not one because flushing the locks must not flush the cache, and a store
-- whose locks live in the cache table refuses to do it at all -- see
-- cache.DatabaseStore.HasSeparateLockStore.

-- expiration is unix milliseconds, not seconds: a cache whose resolution is a
-- second cannot honour a ttl shorter than one.

CREATE TABLE cache (
    key        VARCHAR(255) NOT NULL PRIMARY KEY,
    value      TEXT         NOT NULL,
    expiration BIGINT       NOT NULL
);

CREATE INDEX cache_expiration_index ON cache (expiration);

CREATE TABLE cache_locks (
    key        VARCHAR(255) NOT NULL PRIMARY KEY,
    owner      VARCHAR(255) NOT NULL,
    expiration BIGINT       NOT NULL
);

CREATE INDEX cache_locks_expiration_index ON cache_locks (expiration);
`
