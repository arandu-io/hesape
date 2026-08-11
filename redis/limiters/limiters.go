package limiters

import (
	"context"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// ErrLimiterTimeout answers Illuminate\Contracts\Redis\LimiterTimeoutException.
//
// It is the one error a limiter raises on its own: the caller waited as long as
// it said it would and the slot never came free. Everything else that comes
// back is the driver's.
var ErrLimiterTimeout = errors.New("redis: timed out waiting for a limiter slot")

// Connection is the slice of a Redis connection a limiter needs.
//
// It is declared here, rather than imported from connections, because
// connections offers Funnel and Throttle and therefore imports this package.
// connections.Connection satisfies it as written.
type Connection interface {
	// Client is the driver, for the commands a limiter runs.
	Client() goredis.UniversalClient
	// Key puts the application prefix in front of a key, so two applications
	// sharing a server do not share a limiter.
	Key(key string) string
}

// sleep waits for d, or returns false when ctx is cancelled first.
//
// Every limiter that blocks goes through it, so a cancelled request stops
// waiting for a slot instead of holding a goroutine until the timeout. Laravel
// has no equivalent because PHP has no cancellation.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
