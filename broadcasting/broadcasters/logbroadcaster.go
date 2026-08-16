package broadcasters

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/broadcasting"
)

// LogBroadcaster writes what would have been broadcast to the log.
//
// It is what a developer points the default connection at while the socket
// server is not running yet, and it is why the log line carries the whole
// payload: the line is the only evidence the event happened.
type LogBroadcaster struct {
	Broadcaster

	// logger is where the payload is written. It is *slog.Logger, which is what
	// github.com/arandu-io/hesape/log.New answers.
	logger *slog.Logger
}

// NewLogBroadcaster builds the driver over the logger it writes to.
//
// A nil logger becomes slog.Default, because a driver whose whole job is to
// write somewhere must not be the reason a broadcast panics.
func NewLogBroadcaster(logger *slog.Logger) *LogBroadcaster {
	if logger == nil {
		logger = slog.Default()
	}

	return &LogBroadcaster{logger: logger}
}

// Auth authorizes nobody: it answers the zero auth.Grant, and that fails every
// auth.Grant.Check. A driver that only writes to a file decides nothing.
func (l *LogBroadcaster) Auth(ctx context.Context, channel string) (auth.Grant, any, error) {
	return auth.Grant{}, nil, nil
}

// ValidAuthenticationResponse answers nothing, because [LogBroadcaster.Auth]
// authorizes nobody.
func (l *LogBroadcaster) ValidAuthenticationResponse(ctx context.Context, g auth.Grant, channel broadcasting.Channel, result any) (any, error) {
	return nil, nil
}

// Broadcast writes the event, the channels and the payload at info level, with
// the payload pretty-printed after a newline.
//
// The channel names are the ones that would go on the wire, tenant included --
// a log that showed "orders.17" while the broker saw "acme:orders.17" would be
// the wrong evidence. They come from the embedded [Broadcaster.FormatChannels],
// which is the one place a channel name is built.
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
