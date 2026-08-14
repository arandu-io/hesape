package broadcasters

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
)

// LogBroadcaster is Illuminate\Broadcasting\Broadcasters\LogBroadcaster: it
// writes what would have been broadcast to the log.
//
// It is what a developer points the default connection at while the socket
// server is not running yet, and it is why the log line carries the whole
// payload: the line is the only evidence the event happened.
type LogBroadcaster struct {
	Broadcaster

	// logger is the protected $logger. The PHP takes a PSR-3 LoggerInterface;
	// the Go equivalent is *slog.Logger, which is what
	// github.com/arandu-io/hesape/log.New answers.
	logger *slog.Logger
}

// NewLogBroadcaster is LogBroadcaster::__construct.
//
// A nil logger becomes slog.Default, because a driver whose whole job is to
// write somewhere must not be the reason a broadcast panics.
func NewLogBroadcaster(logger *slog.Logger) *LogBroadcaster {
	if logger == nil {
		logger = slog.Default()
	}

	return &LogBroadcaster{logger: logger}
}

// Auth is LogBroadcaster::auth, whose body is a comment.
//
// The zero auth.Grant is the Go shape of the PHP's null, and it fails every
// auth.Grant.Check: a driver that only writes to a file authorizes nobody.
func (l *LogBroadcaster) Auth(ctx context.Context, channel string) (auth.Grant, any, error) {
	return auth.Grant{}, nil, nil
}

// ValidAuthenticationResponse is LogBroadcaster::validAuthenticationResponse,
// whose body is a comment.
func (l *LogBroadcaster) ValidAuthenticationResponse(ctx context.Context, g auth.Grant, channel broadcasting.Channel, result any) (any, error) {
	return nil, nil
}

// Broadcast is LogBroadcaster::broadcast: it writes the event, the channels and
// the payload at info level.
//
// The message is the PHP's, down to the newline before the payload and the
// pretty-printed JSON after it. The channel names are the ones that would go on
// the wire, tenant included -- a log that showed "orders.17" while the broker
// saw "acme:orders.17" would be the wrong evidence.
//
// It names them through the embedded [Broadcaster.FormatChannels] rather than
// calling broadcasting.TenantChannels itself. That is not a rename: the
// promoted method used to be the one that dropped the tenant, so a driver that
// wanted the tenant had to know not to use it. Now it is the one that adds it,
// and this call is what keeps it from going untested and drifting again.
func (l *LogBroadcaster) Broadcast(ctx context.Context, g auth.Grant, channels []broadcasting.Channel, event string, payload map[string]any) error {
	names, err := l.FormatChannels(g, channels)
	if err != nil {
		return err
	}

	encoded, err := json.MarshalIndent(payload, "", "    ")
	if err != nil {
		return broadcasting.WrapBroadcastError(err, "broadcasting: encoding the payload of %s: %v", event, err)
	}

	l.logger.InfoContext(ctx, "Broadcasting ["+event+"] on channels ["+strings.Join(names, ", ")+"] with payload:\n"+string(encoded))

	return nil
}
