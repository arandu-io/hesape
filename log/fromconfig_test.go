package log_test

import (
	"context"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/log"
)

// TestFromConfigWritesToTheDefaultChannel: the configuration described channels
// that nothing built, so a process that set LOG_CHANNEL kept writing wherever
// New wrote. FromConfig is the path that makes the default channel the one the
// root logger writes to.
func TestFromConfigWritesToTheDefaultChannel(t *testing.T) {
	app, other := &buffer{}, &buffer{}
	logger, err := log.FromConfig(log.Config{
		Default: "app",
		Env:     "production",
		Channels: map[string]log.ChannelConfig{
			"app":   {Driver: "stdout", Name: "app", Writer: app},
			"other": {Driver: "stderr", Name: "other", Writer: other},
		},
	}, nil)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	logger.InfoContext(context.Background(), "order placed", "order", "abc")

	if !strings.Contains(app.String(), `"msg":"order placed"`) || !strings.Contains(app.String(), `"channel":"app"`) {
		t.Fatalf("the default channel got %q, want the line stamped with its name", app.String())
	}
	if other.String() != "" {
		t.Fatalf("a channel that is not the default got %q", other.String())
	}
}

// TestFromConfigHonoursTheChannelLevel: the level is the channel's, which is
// how LOG_LEVEL reaches the root logger.
func TestFromConfigHonoursTheChannelLevel(t *testing.T) {
	out := &buffer{}
	logger, err := log.FromConfig(log.Config{
		Default:  "app",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "stdout", Level: "warning", Writer: out}},
	}, nil)
	if err != nil {
		t.Fatalf("FromConfig: %v", err)
	}
	logger.Info("quiet")
	logger.Warn("loud")
	if strings.Contains(out.String(), "quiet") || !strings.Contains(out.String(), "loud") {
		t.Fatalf("output = %q, want only the warning", out.String())
	}
}

// TestFromConfigReturnsALoggerBesideTheError: a broken configuration must not
// leave the process without a logger.
func TestFromConfigReturnsALoggerBesideTheError(t *testing.T) {
	logger, err := log.FromConfig(log.Config{Default: "missing"}, nil)
	if err == nil {
		t.Fatal("a default channel that is not defined must be an error")
	}
	if logger == nil {
		t.Fatal("the emergency logger must come back beside the error")
	}
}

// TestTheStdoutDriverIsADriver: the stdout driver is what New writes to, so the
// configuration can describe New's destination.
func TestTheStdoutDriverIsADriver(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Channels: map[string]log.ChannelConfig{"out": {Driver: "stdout", Format: "text", Writer: out}},
	}, nil)
	channel, err := manager.Channel("out")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Info(context.Background(), "hello")
	if !strings.Contains(out.String(), "msg=hello") {
		t.Fatalf("output = %q, want the text line", out.String())
	}
}
