package connections

import (
	"context"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ErrNil is what a command returns when the key does not exist.
//
// An absent key is an error rather than a nil value, so a caller checks the
// error it already has to check. It is the driver's own sentinel, so
// errors.Is(err, redis.Nil) from a caller that holds the driver still
// matches.
var ErrNil = goredis.Nil

// Get returns the value of the given key.
//
// A key that is not there returns ErrNil.
func (c *Connection) Get(ctx context.Context, key string) (string, error) {
	full := c.Key(key)
	return timed(c, "get", []any{full}, func() (string, error) {
		return c.client.Get(ctx, full).Result()
	})
}

// Set sets the string value in the argument as the value of the key.
//
// expireResolution is "EX" or "PX" (empty for no expiry), expireTTL is its
// value in that unit, and flag is "NX" or "XX" (empty for neither). Passing "",
// 0 and "" is a plain SET.
func (c *Connection) Set(ctx context.Context, key string, value any, expireResolution string, expireTTL int64, flag string) (string, error) {
	full := c.Key(key)

	args := goredis.SetArgs{Mode: strings.ToUpper(flag)}
	switch strings.ToUpper(expireResolution) {
	case "EX":
		args.TTL = time.Duration(expireTTL) * time.Second
	case "PX":
		args.TTL = time.Duration(expireTTL) * time.Millisecond
	case "":
	default:
		return "", fmt.Errorf("redis: unknown expire resolution %q: it is EX for seconds or PX for milliseconds", expireResolution)
	}

	return timed(c, "set", []any{full, value, expireResolution, expireTTL, flag}, func() (string, error) {
		return c.client.SetArgs(ctx, full, value, args).Result()
	})
}

// SetEx sets the key with a lifetime in seconds.
//
// It is the SETEX command, spelled out here for the reason the others are: a
// method that exists is a method the compiler can check.
func (c *Connection) SetEx(ctx context.Context, key string, seconds int64, value any) (string, error) {
	full := c.Key(key)
	return timed(c, "setex", []any{full, seconds, value}, func() (string, error) {
		return c.client.SetEx(ctx, full, value, time.Duration(seconds)*time.Second).Result()
	})
}

// PSetEx sets the key with a lifetime in milliseconds. It is the PSETEX
// command.
func (c *Connection) PSetEx(ctx context.Context, key string, milliseconds int64, value any) (string, error) {
	full := c.Key(key)
	return timed(c, "psetex", []any{full, milliseconds, value}, func() (string, error) {
		return c.client.Set(ctx, full, value, time.Duration(milliseconds)*time.Millisecond).Result()
	})
}

// SetNx sets the given key if it does not exist, and reports whether it did.
//
// The server answers 1 or 0; this returns the bool those two stand for.
func (c *Connection) SetNx(ctx context.Context, key string, value any) (bool, error) {
	full := c.Key(key)
	return timed(c, "setnx", []any{full, value}, func() (bool, error) {
		return c.client.SetNX(ctx, full, value, 0).Result()
	})
}

// MSet sets many keys at once. It is the MSET command.
func (c *Connection) MSet(ctx context.Context, values map[string]any) (string, error) {
	full := c.keyed(values)
	return timed(c, "mset", []any{full}, func() (string, error) {
		return c.client.MSet(ctx, full).Result()
	})
}

// MSetNx sets many keys at once, and only if none of them exists. It is the
// MSETNX command.
func (c *Connection) MSetNx(ctx context.Context, values map[string]any) (bool, error) {
	full := c.keyed(values)
	return timed(c, "msetnx", []any{full}, func() (bool, error) {
		return c.client.MSetNX(ctx, full).Result()
	})
}

// MGet gets the values of all the given keys.
//
// A key that is not there is nil in the result, in the position it was asked
// for.
func (c *Connection) MGet(ctx context.Context, keys []string) ([]any, error) {
	full := c.keys(keys)
	return timed(c, "mget", toAny(full), func() ([]any, error) {
		return c.client.MGet(ctx, full...).Result()
	})
}

// HMGet gets the value of the given hash fields.
//
// It answers Connection::hmget(). A field that is not set is nil, in the
// position it was asked for.
func (c *Connection) HMGet(ctx context.Context, key string, fields ...string) ([]any, error) {
	full := c.Key(key)
	return timed(c, "hmget", append([]any{full}, toAny(fields)...), func() ([]any, error) {
		return c.client.HMGet(ctx, full, fields...).Result()
	})
}

// HMSet sets the given hash fields to their respective values.
//
// It takes a map rather than a flat list of alternating field and value: a map
// is the shape the compiler can check, and the one every call site used.
func (c *Connection) HMSet(ctx context.Context, key string, dictionary map[string]any) (bool, error) {
	full := c.Key(key)
	pairs := make([]any, 0, len(dictionary)*2)
	for field, value := range dictionary {
		pairs = append(pairs, field, value)
	}
	return timed(c, "hmset", append([]any{full}, pairs...), func() (bool, error) {
		return c.client.HMSet(ctx, full, pairs...).Result()
	})
}

// HSetNx sets the given hash field if it does not exist, and reports whether it
// did.
func (c *Connection) HSetNx(ctx context.Context, hash, key string, value any) (bool, error) {
	full := c.Key(hash)
	return timed(c, "hsetnx", []any{full, key, value}, func() (bool, error) {
		return c.client.HSetNX(ctx, full, key, value).Result()
	})
}

// LRem removes the first count occurrences of value from the list.
//
// Note the argument order: (key, count, value), which is not the order the
// wire protocol puts them in.
func (c *Connection) LRem(ctx context.Context, key string, count int64, value any) (int64, error) {
	full := c.Key(key)
	return timed(c, "lrem", []any{full, count, value}, func() (int64, error) {
		return c.client.LRem(ctx, full, count, value).Result()
	})
}

// Blpop removes and returns the first element of the first non-empty list,
// waiting up to timeout.
//
// The result is the two-element slice the server returns -- the list the
// element came from, and the element. A timeout that expires empty-handed
// returns ErrNil.
func (c *Connection) Blpop(ctx context.Context, timeout time.Duration, keys ...string) ([]string, error) {
	full := c.keys(keys)
	return timed(c, "blpop", append(toAny(full), timeout), func() ([]string, error) {
		return c.client.BLPop(ctx, timeout, full...).Result()
	})
}

// Brpop removes and returns the last element of the first non-empty list,
// waiting up to timeout. It answers Connection::brpop().
func (c *Connection) Brpop(ctx context.Context, timeout time.Duration, keys ...string) ([]string, error) {
	full := c.keys(keys)
	return timed(c, "brpop", append(toAny(full), timeout), func() ([]string, error) {
		return c.client.BRPop(ctx, timeout, full...).Result()
	})
}

// Spop removes and returns count random elements from the set at key.
//
// A count of zero is read as one.
func (c *Connection) Spop(ctx context.Context, key string, count int64) ([]string, error) {
	if count <= 0 {
		count = 1
	}
	full := c.Key(key)
	return timed(c, "spop", []any{full, count}, func() ([]string, error) {
		return c.client.SPopN(ctx, full, count).Result()
	})
}

// ZAdd adds members to a sorted set, or updates their score.
//
// options are the flags "NX", "XX", "CH", "GT" and "LT", and they are
// case-insensitive.
//
// "INCR" is refused rather than accepted and ignored: it changes what the
// command returns, from a count of added members to the new score, and a method
// that answers a different question under a flag is a method nobody can read.
// Use ZIncrBy through Command.
func (c *Connection) ZAdd(ctx context.Context, key string, members map[string]float64, options ...string) (int64, error) {
	full := c.Key(key)

	args := goredis.ZAddArgs{Members: make([]goredis.Z, 0, len(members))}
	for member, score := range members {
		args.Members = append(args.Members, goredis.Z{Score: score, Member: member})
	}
	for _, option := range options {
		switch strings.ToUpper(option) {
		case "NX":
			args.NX = true
		case "XX":
			args.XX = true
		case "CH":
			args.Ch = true
		case "GT":
			args.GT = true
		case "LT":
			args.LT = true
		default:
			return 0, fmt.Errorf("redis: unknown zadd option %q: it is NX, XX, CH, GT or LT", option)
		}
	}

	return timed(c, "zadd", []any{full, args}, func() (int64, error) {
		return c.client.ZAddArgs(ctx, full, args).Result()
	})
}

// RangeOptions is the options array of zrangebyscore and zrevrangebyscore.
//
// It is a struct rather than a map of strings, so the two fields a caller can
// set are the two fields the compiler shows.
type RangeOptions struct {
	// Offset and Count are the LIMIT clause. A Count of zero means no LIMIT
	// clause at all.
	Offset, Count int64
}

// ZRangeByScore returns the members with a score between min and max.
//
// min and max are the strings the server takes, so "-inf", "+inf" and the
// exclusive "(5" all work.
func (c *Connection) ZRangeByScore(ctx context.Context, key, min, max string, options RangeOptions) ([]string, error) {
	full := c.Key(key)
	by := &goredis.ZRangeBy{Min: min, Max: max, Offset: options.Offset, Count: options.Count}
	return timed(c, "zrangebyscore", []any{full, min, max, options}, func() ([]string, error) {
		return c.client.ZRangeByScore(ctx, full, by).Result()
	})
}

// ZRevRangeByScore returns the members with a score between max and min,
// highest first. It answers Connection::zrevrangebyscore().
func (c *Connection) ZRevRangeByScore(ctx context.Context, key, max, min string, options RangeOptions) ([]string, error) {
	full := c.Key(key)
	by := &goredis.ZRangeBy{Min: min, Max: max, Offset: options.Offset, Count: options.Count}
	return timed(c, "zrevrangebyscore", []any{full, max, min, options}, func() ([]string, error) {
		return c.client.ZRevRangeByScore(ctx, full, by).Result()
	})
}

// StoreOptions is the options array of zinterstore and zunionstore.
type StoreOptions struct {
	// Weights multiplies each input set's scores, in the order the keys were
	// given. Empty means every set weighs one.
	Weights []float64
	// Aggregate is "sum", "min" or "max". Empty means "sum".
	Aggregate string
}

// ZInterStore stores the intersection of the given sorted sets in output, and
// returns how many members it holds.
func (c *Connection) ZInterStore(ctx context.Context, output string, keys []string, options StoreOptions) (int64, error) {
	dest, store := c.zstore(output, keys, options)
	return timed(c, "zinterstore", []any{dest, store}, func() (int64, error) {
		return c.client.ZInterStore(ctx, dest, store).Result()
	})
}

// ZUnionStore stores the union of the given sorted sets in output, and returns
// how many members it holds.
func (c *Connection) ZUnionStore(ctx context.Context, output string, keys []string, options StoreOptions) (int64, error) {
	dest, store := c.zstore(output, keys, options)
	return timed(c, "zunionstore", []any{dest, store}, func() (int64, error) {
		return c.client.ZUnionStore(ctx, dest, store).Result()
	})
}

// zstore prefixes the destination and the inputs and fills in the default
// "sum".
func (c *Connection) zstore(output string, keys []string, options StoreOptions) (string, *goredis.ZStore) {
	aggregate := options.Aggregate
	if aggregate == "" {
		aggregate = "sum"
	}
	return c.Key(output), &goredis.ZStore{
		Keys:      c.keys(keys),
		Weights:   options.Weights,
		Aggregate: aggregate,
	}
}

// ScanOptions is the options array of the four scan commands.
type ScanOptions struct {
	// Match is the glob the server filters by. Empty means "*". It is prefixed
	// like a key, so a scan of one application does not walk another's.
	Match string
	// Count is the hint for how much work the server does per round trip. Zero
	// means ten.
	Count int64
}

// Scan walks the keyspace from cursor, and returns the keys it found and the
// cursor to continue from.
//
// A returned cursor of zero means the walk is over.
//
// The keys come back WITH the application prefix, because that is the name they
// have on the server and the name any follow-up command needs.
func (c *Connection) Scan(ctx context.Context, cursor uint64, options ScanOptions) ([]string, uint64, error) {
	match, count := c.scanArgs(options)

	start := time.Now()
	keys, next, err := c.client.Scan(ctx, cursor, match, count).Result()
	c.fireCommandExecuted("scan", []any{cursor, match, count}, time.Since(start))
	return keys, next, err
}

// ZScan walks the sorted set at key, returning member and score alternating.
func (c *Connection) ZScan(ctx context.Context, key string, cursor uint64, options ScanOptions) ([]string, uint64, error) {
	full := c.Key(key)
	match, count := c.scanArgs(options)

	start := time.Now()
	values, next, err := c.client.ZScan(ctx, full, cursor, match, count).Result()
	c.fireCommandExecuted("zscan", []any{full, cursor, match, count}, time.Since(start))
	return values, next, err
}

// HScan walks the hash at key, returning field and value alternating.
func (c *Connection) HScan(ctx context.Context, key string, cursor uint64, options ScanOptions) ([]string, uint64, error) {
	full := c.Key(key)
	match, count := c.scanArgs(options)

	start := time.Now()
	values, next, err := c.client.HScan(ctx, full, cursor, match, count).Result()
	c.fireCommandExecuted("hscan", []any{full, cursor, match, count}, time.Since(start))
	return values, next, err
}

// SScan walks the set at key.
func (c *Connection) SScan(ctx context.Context, key string, cursor uint64, options ScanOptions) ([]string, uint64, error) {
	full := c.Key(key)
	match, count := c.scanArgs(options)

	start := time.Now()
	values, next, err := c.client.SScan(ctx, full, cursor, match, count).Result()
	c.fireCommandExecuted("sscan", []any{full, cursor, match, count}, time.Since(start))
	return values, next, err
}

// scanArgs fills in the glob and the count defaults.
func (c *Connection) scanArgs(options ScanOptions) (string, int64) {
	match := options.Match
	if match == "" {
		match = "*"
	}
	count := options.Count
	if count <= 0 {
		count = 10
	}
	return c.Key(match), count
}

// Pipeline executes the commands queued by callback in one round trip.
//
// It takes the callback and executes it, rather than handing the pipeline back
// for the caller to run: a pipeline handed back is a pipeline nobody executed
// when the caller forgets.
//
// The commands are handed to the callback unprefixed, because they are the
// driver's: build keys with Key.
func (c *Connection) Pipeline(ctx context.Context, callback func(goredis.Pipeliner) error) ([]goredis.Cmder, error) {
	return timed(c, "pipeline", nil, func() ([]goredis.Cmder, error) {
		return c.client.Pipelined(ctx, callback)
	})
}

// Transaction executes the commands queued by callback inside MULTI/EXEC.
func (c *Connection) Transaction(ctx context.Context, callback func(goredis.Pipeliner) error) ([]goredis.Cmder, error) {
	return timed(c, "transaction", nil, func() ([]goredis.Cmder, error) {
		return c.client.TxPipelined(ctx, callback)
	})
}

// Eval evaluates a script server-side and returns its result.
//
// The first numberOfKeys arguments are keys and are prefixed; the rest are
// passed through.
//
// # Nothing in this collection may call it
//
// It is here because an application that has already chosen its server may want
// it. Nothing inside this adapter uses it, and nothing may: Dragonfly, Redis,
// Valkey and KeyDB stay interchangeable only while no script is required, and a
// script is the first thing that stops being true of all four. The lock, the
// session index and both limiters are written without one for that reason.
func (c *Connection) Eval(ctx context.Context, script string, numberOfKeys int, arguments ...any) (any, error) {
	keys, args := c.splitEvalArguments(numberOfKeys, arguments)
	return timed(c, "eval", arguments, func() (any, error) {
		return c.client.Eval(ctx, script, keys, args...).Result()
	})
}

// EvalSha evaluates a script server-side from its SHA1 hash.
func (c *Connection) EvalSha(ctx context.Context, sha string, numberOfKeys int, arguments ...any) (any, error) {
	keys, args := c.splitEvalArguments(numberOfKeys, arguments)
	return timed(c, "evalsha", arguments, func() (any, error) {
		return c.client.EvalSha(ctx, sha, keys, args...).Result()
	})
}

// splitEvalArguments takes the first numberOfKeys arguments as keys, prefixes
// them, and leaves the rest alone.
func (c *Connection) splitEvalArguments(numberOfKeys int, arguments []any) ([]string, []any) {
	if numberOfKeys < 0 {
		numberOfKeys = 0
	}
	if numberOfKeys > len(arguments) {
		numberOfKeys = len(arguments)
	}

	keys := make([]string, 0, numberOfKeys)
	for _, argument := range arguments[:numberOfKeys] {
		keys = append(keys, c.Key(fmt.Sprint(argument)))
	}
	return keys, arguments[numberOfKeys:]
}

// FlushDB flushes the selected Redis database.
func (c *Connection) FlushDB(ctx context.Context, async bool) (string, error) {
	return timed(c, "flushdb", []any{async}, func() (string, error) {
		if async {
			return c.client.FlushDBAsync(ctx).Result()
		}
		return c.client.FlushDB(ctx).Result()
	})
}

// ExecuteRaw executes a raw command, argument by argument.
//
// It answers Connection::executeRaw(). Nothing is prefixed and nothing is
// checked -- it is the door for the commands this type does not name, and the
// caller builds every key with Key itself.
func (c *Connection) ExecuteRaw(ctx context.Context, parameters ...any) (any, error) {
	return timed(c, "rawcommand", parameters, func() (any, error) {
		return c.client.Do(ctx, parameters...).Result()
	})
}

// Publish sends a message to a channel and returns how many subscribers got it.
//
// It is the PUBLISH command, spelled out because the broadcaster needs it by
// name. The channel is prefixed like a key, so two applications on one server
// do not hear each other.
func (c *Connection) Publish(ctx context.Context, channel string, message any) (int64, error) {
	full := c.Key(channel)
	return timed(c, "publish", []any{full, message}, func() (int64, error) {
		return c.client.Publish(ctx, full, message).Result()
	})
}

// keys prefixes a list of keys.
func (c *Connection) keys(keys []string) []string {
	full := make([]string, len(keys))
	for i, key := range keys {
		full[i] = c.Key(key)
	}
	return full
}

// keyed prefixes the keys of a map, leaving the values alone.
func (c *Connection) keyed(values map[string]any) map[string]any {
	full := make(map[string]any, len(values))
	for key, value := range values {
		full[c.Key(key)] = value
	}
	return full
}

// toAny widens a list of keys for the event payload.
func toAny(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}
