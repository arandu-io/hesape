package log_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/arandu-io/hesape/log"
	logcontext "github.com/arandu-io/hesape/log/context"
)

// requestContext is a context carrying a log/context repository with one
// visible and one hidden entry, which is what an application puts there at the
// edge of a request.
func requestContext(t *testing.T) context.Context {
	t.Helper()
	repository := logcontext.New(nil).Add("order", "abc").AddHidden("token", "secret")
	return logcontext.Into(context.Background(), repository)
}

// buffer is an io.Writer a test can read back, locked because a stack writes
// into several handlers and the manager is meant to be used from more than one
// goroutine.
type buffer struct {
	mu  sync.Mutex
	out bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.out.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.out.String()
}

// failing is a writer that refuses everything, which is how a handler inside a
// stack is made to fail.
type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("the writer refused") }

func TestChannelResolvesFromTheConfigurationAndIsCached(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Default:  "app",
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "stderr", Writer: out}},
	}, nil)

	first, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	second, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel a second time: %v", err)
	}
	if first != second {
		t.Fatal("Channel resolved the same name twice instead of caching it")
	}

	// An empty name means the default channel.
	byDefault, err := manager.Channel("")
	if err != nil {
		t.Fatalf("the default channel: %v", err)
	}
	if byDefault != first {
		t.Fatal("the default channel is not the one logging.default names")
	}
	// Driver is what Channel calls, so it must land on the same object.
	byDriver, err := manager.Driver("app")
	if err != nil {
		t.Fatalf("Driver: %v", err)
	}
	if byDriver != first {
		t.Fatal("Driver and Channel resolved to different loggers")
	}
}

func TestTheChannelNameIsStampedOnEveryLine(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"named": {Driver: "stderr", Name: "billing", Writer: out}},
	}, nil)

	channel, err := manager.Channel("named")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Info(context.Background(), "the line")

	if !strings.Contains(out.String(), `"channel":"billing"`) {
		t.Fatalf("the line reads %s", out.String())
	}
}

// TestTheChannelNameFallsBackToTheEnvironment is
// ParsesLogConfiguration::parseChannel with
// LogManager::getFallbackChannelName: the configured name, then the
// environment, then "production".
func TestTheChannelNameFallsBackToTheEnvironment(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string
	}{
		{"the environment names it", "staging", "staging"},
		{"with no environment it is production", "", "production"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := &buffer{}
			manager := log.NewLogManager(log.Config{
				Env:      c.env,
				Channels: map[string]log.ChannelConfig{"app": {Driver: "stderr", Writer: out}},
			}, nil)

			channel, err := manager.Channel("app")
			if err != nil {
				t.Fatalf("Channel: %v", err)
			}
			channel.Info(context.Background(), "the line")

			if !strings.Contains(out.String(), `"channel":"`+c.want+`"`) {
				t.Fatalf("the line reads %s", out.String())
			}
		})
	}
}

// TestAnUndefinedChannelFallsBackToTheEmergencyLogger is the whole reason the
// emergency logger exists: a broken logging configuration does not take the
// request down with it.
func TestAnUndefinedChannelFallsBackToTheEmergencyLogger(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"emergency": {Driver: "stderr", Path: filepath.Join(t.TempDir(), "emergency.log"), Writer: out}},
	}, nil)

	channel, err := manager.Channel("never-configured")
	if err == nil {
		t.Fatal("an undefined channel resolved without an error")
	}
	if !strings.Contains(err.Error(), "never-configured") {
		t.Fatalf("the error is %v, want it to name the channel", err)
	}
	if channel == nil {
		t.Fatal("an undefined channel returned no logger at all")
	}
	// The emergency logger is usable, which is the point of returning it.
	channel.Info(context.Background(), "the request kept going")
}

func TestAnUnsupportedDriverIsAnError(t *testing.T) {
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "papertrail"}},
	}, nil)

	if _, err := manager.Channel("app"); err == nil || !strings.Contains(err.Error(), "papertrail") {
		t.Fatalf("the error is %v, want it to name the driver", err)
	}
}

func TestTheLevelFiltersTheChannel(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "stderr", Level: "warning", Writer: out}},
	}, nil)

	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Notice(context.Background(), "below")
	channel.Error(context.Background(), "above")

	written := out.String()
	if strings.Contains(written, "below") {
		t.Fatalf("a line below the channel level was written: %s", written)
	}
	if !strings.Contains(written, "above") {
		t.Fatalf("a line above the channel level was dropped: %s", written)
	}
}

// TestAnInvalidLevelIsAnError is the InvalidArgumentException
// ParsesLogConfiguration::level throws, and it must not be a silent fallback:
// a typo that quietly restores debug is a production process logging more than
// it was told to.
func TestAnInvalidLevelIsAnError(t *testing.T) {
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "stderr", Level: "verbose"}},
	}, nil)

	if _, err := manager.Channel("app"); err == nil {
		t.Fatal("an invalid level resolved without an error")
	}
}

// TestNoLevelIsDebug is `$config['level'] ?? 'debug'`.
func TestNoLevelIsDebug(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "stderr", Writer: out}},
	}, nil)

	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Debug(context.Background(), "the quietest line")

	if !strings.Contains(out.String(), "the quietest line") {
		t.Fatalf("a channel with no level dropped a debug line: %s", out.String())
	}
}

func TestTheFormatFollowsTheChannelThenTheEnvironment(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		format  string
		wantsJS bool
	}{
		{"development is text", "dev", "", false},
		{"anything else is JSON", "production", "", true},
		{"the channel may say text", "production", "text", false},
		{"the channel may say json", "dev", "json", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := &buffer{}
			manager := log.NewLogManager(log.Config{
				Env:      c.env,
				Channels: map[string]log.ChannelConfig{"app": {Driver: "stderr", Format: c.format, Writer: out}},
			}, nil)

			channel, err := manager.Channel("app")
			if err != nil {
				t.Fatalf("Channel: %v", err)
			}
			channel.Info(context.Background(), "the line")

			isJSON := strings.HasPrefix(strings.TrimSpace(out.String()), "{")
			if isJSON != c.wantsJS {
				t.Fatalf("the line reads %s", out.String())
			}
		})
	}
}

// TestTheEightLevelsRenderUnderThePSR3Names: a level that prints as "ERROR+4"
// is a level nobody trusts.
func TestTheChannelRendersTheEightLevelNames(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "stderr", Writer: out}},
	}, nil)

	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Emergency(context.Background(), "m")
	channel.Alert(context.Background(), "m")
	channel.Critical(context.Background(), "m")
	channel.Notice(context.Background(), "m")

	for _, want := range []string{`"level":"EMERGENCY"`, `"level":"ALERT"`, `"level":"CRITICAL"`, `"level":"NOTICE"`} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the output does not contain %s: %s", want, out.String())
		}
	}
}

func TestTheSingleDriverAppendsToItsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "app.log")
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "single", Path: path}},
	}, nil)

	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Info(context.Background(), "the first line")
	channel.Info(context.Background(), "the second line")

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the single driver did not create its file: %v", err)
	}
	if !strings.Contains(string(written), "the first line") || !strings.Contains(string(written), "the second line") {
		t.Fatalf("the file holds %s", written)
	}
}

func TestTheSingleAndRotatingDriversNeedAPath(t *testing.T) {
	for _, driver := range []string{"single", "daily", "monthly"} {
		t.Run(driver, func(t *testing.T) {
			manager := log.NewLogManager(log.Config{
				Env:      "testing",
				Channels: map[string]log.ChannelConfig{"app": {Driver: driver}},
			}, nil)

			if _, err := manager.Channel("app"); err == nil {
				t.Fatalf("the %s driver resolved without a path", driver)
			}
		})
	}
}

// TestTheDailyDriverNamesTheFileAfterTheDay: base-YYYY-MM-DD.ext.
func TestTheDailyDriverNamesTheFileAfterTheDay(t *testing.T) {
	dir := t.TempDir()
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "daily", Path: filepath.Join(dir, "app.log")}},
	}, nil)

	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Info(context.Background(), "the line")

	want := filepath.Join(dir, "app-"+time.Now().Format(time.DateOnly)+".log")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("the daily driver did not write %s: %v", want, err)
	}
}

// TestTheMonthlyDriverNamesTheFileAfterTheMonth: base-YYYY-MM.ext, which is the
// whole difference between it and the daily driver.
func TestTheMonthlyDriverNamesTheFileAfterTheMonth(t *testing.T) {
	dir := t.TempDir()
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "monthly", Path: filepath.Join(dir, "app.log")}},
	}, nil)

	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Info(context.Background(), "the line")

	want := filepath.Join(dir, "app-"+time.Now().Format("2006-01")+".log")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("the monthly driver did not write %s: %v", want, err)
	}
}

// TestTheRotatingDriverPrunesTheOldFiles: everything past the newest MaxFiles
// goes on the rotation.
func TestTheRotatingDriverPrunesTheOldFiles(t *testing.T) {
	dir := t.TempDir()
	// Two files from days that are over, plus today's, is three -- one more
	// than max_files, so the oldest goes.
	for _, day := range []string{"2020-01-01", "2020-01-02"} {
		if err := os.WriteFile(filepath.Join(dir, "app-"+day+".log"), []byte("old\n"), 0o644); err != nil {
			t.Fatalf("seeding %s: %v", day, err)
		}
	}

	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "daily", Path: filepath.Join(dir, "app.log"), MaxFiles: 2}},
	}, nil)

	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Info(context.Background(), "the line")

	left, err := filepath.Glob(filepath.Join(dir, "app-*.log"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(left) != 2 {
		t.Fatalf("%d files were left: %v", len(left), left)
	}
	if _, err := os.Stat(filepath.Join(dir, "app-2020-01-01.log")); err == nil {
		t.Fatal("the oldest file survived the rotation")
	}
}

func TestTheNullDriverDiscardsEverything(t *testing.T) {
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "null"}},
	}, nil)

	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Emergency(context.Background(), "into the void")
}

func TestTheCustomDriverIsBuiltByItsFactory(t *testing.T) {
	out := &buffer{}
	built := 0
	manager := log.NewLogManager(log.Config{
		Env: "testing",
		Channels: map[string]log.ChannelConfig{"app": {
			Driver: "custom",
			Via: func(config log.ChannelConfig) (*slog.Logger, error) {
				built++
				return slog.New(slog.NewJSONHandler(out, nil)), nil
			},
		}},
	}, nil)

	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Info(context.Background(), "the line")

	if built != 1 {
		t.Fatalf("the factory ran %d times", built)
	}
	if !strings.Contains(out.String(), "the line") {
		t.Fatalf("the custom logger wrote %s", out.String())
	}
}

func TestTheCustomDriverNeedsAFactory(t *testing.T) {
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "custom"}},
	}, nil)

	if _, err := manager.Channel("app"); err == nil {
		t.Fatal("the custom driver resolved without a via factory")
	}
}

func TestExtendRegistersADriverTheManagerDoesNotKnow(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "in-memory"}},
	}, nil)

	manager.Extend("in-memory", func(config log.ChannelConfig) (*slog.Logger, error) {
		return slog.New(slog.NewJSONHandler(out, nil)), nil
	})

	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Info(context.Background(), "the line")

	if !strings.Contains(out.String(), "the line") {
		t.Fatalf("the extended driver wrote %s", out.String())
	}
}

func TestBuildMakesAChannelThatIsNotInTheFile(t *testing.T) {
	first, second := &buffer{}, &buffer{}
	manager := log.NewLogManager(log.Config{Env: "testing"}, nil)

	one, err := manager.Build(log.ChannelConfig{Driver: "stderr", Writer: first})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	two, err := manager.Build(log.ChannelConfig{Driver: "stderr", Writer: second})
	if err != nil {
		t.Fatalf("Build a second time: %v", err)
	}
	if one == two {
		t.Fatal("two Build calls handed back the same logger")
	}

	two.Info(context.Background(), "the second line")
	if first.String() != "" {
		t.Fatalf("the first on-demand channel received %s", first.String())
	}
	if !strings.Contains(second.String(), "the second line") {
		t.Fatalf("the second on-demand channel wrote %s", second.String())
	}
}

func TestStackWritesIntoEveryChannel(t *testing.T) {
	one, two := &buffer{}, &buffer{}
	manager := log.NewLogManager(log.Config{
		Env: "testing",
		Channels: map[string]log.ChannelConfig{
			"one": {Driver: "stderr", Writer: one},
			"two": {Driver: "stderr", Writer: two},
		},
	}, nil)

	stack, err := manager.Stack([]string{"one", " two "}, "")
	if err != nil {
		t.Fatalf("Stack: %v", err)
	}
	stack.Info(context.Background(), "the line")

	if !strings.Contains(one.String(), "the line") {
		t.Fatalf("the first channel of the stack wrote %s", one.String())
	}
	if !strings.Contains(two.String(), "the line") {
		t.Fatalf("the second channel of the stack wrote %s", two.String())
	}
}

// TestAStackIsNotCached: Stack builds a new aggregate every time and never puts
// it in the channel cache.
func TestAStackIsNotCached(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"one": {Driver: "stderr", Writer: out}},
	}, nil)

	first, err := manager.Stack([]string{"one"}, "")
	if err != nil {
		t.Fatalf("Stack: %v", err)
	}
	second, err := manager.Stack([]string{"one"}, "")
	if err != nil {
		t.Fatalf("Stack a second time: %v", err)
	}
	if first == second {
		t.Fatal("two Stack calls handed back the same logger")
	}
}

func TestAStackNeedsAtLeastOneChannel(t *testing.T) {
	manager := log.NewLogManager(log.Config{Env: "testing"}, nil)

	if _, err := manager.Stack(nil, ""); err == nil {
		t.Fatal("an empty stack resolved without an error")
	}
}

func TestAStackOfAnUndefinedChannelIsAnError(t *testing.T) {
	manager := log.NewLogManager(log.Config{Env: "testing"}, nil)

	channel, err := manager.Stack([]string{"never-configured"}, "")
	if err == nil {
		t.Fatal("a stack over an undefined channel resolved without an error")
	}
	if channel == nil {
		t.Fatal("a failing stack returned no logger at all")
	}
}

// TestABrokenHandlerDoesNotStopTheRestOfTheStack is what IgnoreExceptions buys:
// a handler that fails does not stop the ones after it and does not surface.
//
// The failure is asserted on the handler rather than through the Logger, because
// slog's Logger discards what a handler returns -- so the difference
// IgnoreExceptions makes is only visible one level down.
func TestABrokenHandlerDoesNotStopTheRestOfTheStack(t *testing.T) {
	good := &buffer{}
	manager := log.NewLogManager(log.Config{
		Env: "testing",
		Channels: map[string]log.ChannelConfig{
			"broken":      {Driver: "stderr", Writer: failing{}},
			"good":        {Driver: "stderr", Writer: good},
			"forgiving":   {Driver: "stack", Channels: []string{"broken", "good"}, IgnoreExceptions: true},
			"unforgiving": {Driver: "stack", Channels: []string{"broken", "good"}},
		},
	}, nil)

	forgiving, err := manager.Channel("forgiving")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	if err := forgiving.GetLogger().Handler().Handle(context.Background(), record("the forgiving line")); err != nil {
		t.Fatalf("a stack with ignore_exceptions surfaced %v", err)
	}
	if !strings.Contains(good.String(), "the forgiving line") {
		t.Fatalf("the healthy channel of the stack wrote %s", good.String())
	}

	unforgiving, err := manager.Channel("unforgiving")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	if err := unforgiving.GetLogger().Handler().Handle(context.Background(), record("the unforgiving line")); err == nil {
		t.Fatal("a stack without ignore_exceptions swallowed the failure")
	}
	if !strings.Contains(good.String(), "the unforgiving line") {
		t.Fatalf("the failing handler stopped the one after it: %s", good.String())
	}
}

// record is one line, built the way slog builds one, for the tests that go
// straight at a handler.
func record(message string) slog.Record {
	return slog.NewRecord(time.Now(), log.LevelInfo, message, 0)
}

func TestShareContextReachesResolvedAndFutureChannels(t *testing.T) {
	one, two := &buffer{}, &buffer{}
	manager := log.NewLogManager(log.Config{
		Env: "testing",
		Channels: map[string]log.ChannelConfig{
			"one": {Driver: "stderr", Writer: one},
			"two": {Driver: "stderr", Writer: two},
		},
	}, nil)

	// One channel is resolved before ShareContext and one after, and both have
	// to end up carrying the field.
	before, err := manager.Channel("one")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	manager.ShareContext(map[string]any{"request_id": "r-1"})
	after, err := manager.Channel("two")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}

	before.Info(context.Background(), "first")
	after.Info(context.Background(), "second")

	if !strings.Contains(one.String(), `"request_id":"r-1"`) {
		t.Fatalf("the already resolved channel wrote %s", one.String())
	}
	if !strings.Contains(two.String(), `"request_id":"r-1"`) {
		t.Fatalf("the channel resolved afterwards wrote %s", two.String())
	}
}

func TestSharedContextIsACopy(t *testing.T) {
	manager := log.NewLogManager(log.Config{Env: "testing"}, nil)
	manager.ShareContext(map[string]any{"request_id": "r-1"})

	shared := manager.SharedContext()
	if shared["request_id"] != "r-1" {
		t.Fatalf("SharedContext returned %v", shared)
	}
	shared["request_id"] = "changed"
	if manager.SharedContext()["request_id"] != "r-1" {
		t.Fatal("writing into the map SharedContext returned changed the manager")
	}

	manager.FlushSharedContext()
	if len(manager.SharedContext()) != 0 {
		t.Fatalf("FlushSharedContext left %v", manager.SharedContext())
	}
}

// TestWithoutContextLeavesTheSharedHalfAlone: the two are separate, and
// FlushSharedContext is the one that clears the shared half.
func TestWithoutContextLeavesTheSharedHalfAlone(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "stderr", Writer: out}},
	}, nil)

	manager.ShareContext(map[string]any{"request_id": "r-1"})
	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}

	manager.WithoutContext()
	channel.Info(context.Background(), "the line")

	if strings.Contains(out.String(), "request_id") {
		t.Fatalf("WithoutContext did not clear the channel: %s", out.String())
	}
	if manager.SharedContext()["request_id"] != "r-1" {
		t.Fatal("WithoutContext cleared the shared context too")
	}
}

func TestForgetChannelMakesTheNextCallBuildItAgain(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Default:  "app",
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "stderr", Writer: out}},
	}, nil)

	first, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	manager.ForgetChannel("app")
	second, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel after ForgetChannel: %v", err)
	}
	if first == second {
		t.Fatal("ForgetChannel left the resolved channel in the cache")
	}

	// An empty name is the default channel, which is what parseDriver does.
	manager.ForgetChannel("")
	third, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	if second == third {
		t.Fatal("ForgetChannel with no name did not forget the default channel")
	}
}

func TestGetChannelsReportsWhatWasResolved(t *testing.T) {
	manager := log.NewLogManager(log.Config{
		Env: "testing",
		Channels: map[string]log.ChannelConfig{
			"one": {Driver: "null"},
			"two": {Driver: "null"},
		},
	}, nil)

	if len(manager.GetChannels()) != 0 {
		t.Fatal("a manager that resolved nothing reported channels")
	}
	resolved, err := manager.Channel("one")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}

	channels := manager.GetChannels()
	if len(channels) != 1 || channels["one"] != resolved {
		t.Fatalf("GetChannels returned %v", channels)
	}
	// The map is a copy; the loggers in it are the live ones.
	delete(channels, "one")
	if len(manager.GetChannels()) != 1 {
		t.Fatal("deleting from the map GetChannels returned changed the manager")
	}
}

func TestTheDefaultDriverIsReadableAndWritable(t *testing.T) {
	manager := log.NewLogManager(log.Config{Default: "app", Env: "testing"}, nil)

	if got := manager.GetDefaultDriver(); got != "app" {
		t.Fatalf("GetDefaultDriver returned %q", got)
	}
	manager.SetDefaultDriver("other")
	if got := manager.GetDefaultDriver(); got != "other" {
		t.Fatalf("after SetDefaultDriver it returned %q", got)
	}
}

// TestTheManagerIsItselfALogger: the eight levels and Log on the manager write
// to the default channel.
func TestTheManagerIsItselfALogger(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Default:  "app",
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "stderr", Writer: out}},
	}, nil)

	manager.Emergency(context.Background(), "emergency")
	manager.Alert(context.Background(), "alert")
	manager.Critical(context.Background(), "critical")
	manager.Error(context.Background(), "error")
	manager.Warning(context.Background(), "warning")
	manager.Notice(context.Background(), "notice")
	manager.Info(context.Background(), "info", map[string]any{"order": "abc"})
	manager.Debug(context.Background(), "debug")
	manager.Log(context.Background(), log.LevelInfo, "log")

	written := out.String()
	for _, want := range []string{"emergency", "alert", "critical", "error", "warning", "notice", "info", "debug", "log"} {
		if !strings.Contains(written, `"msg":"`+want+`"`) {
			t.Fatalf("the default channel did not receive %q: %s", want, written)
		}
	}
	if !strings.Contains(written, `"order":"abc"`) {
		t.Fatalf("the context array did not reach the default channel: %s", written)
	}
}

// TestTheManagerLogsThroughTheEmergencyLoggerWhenTheDefaultIsBroken: a channel
// that will not resolve does not silence the line.
func TestTheManagerLogsThroughTheEmergencyLoggerWhenTheDefaultIsBroken(t *testing.T) {
	manager := log.NewLogManager(log.Config{Default: "never-configured", Env: "testing"}, nil)

	// It must not panic, and the line has somewhere to go: the emergency
	// logger, which writes to the process error output when no emergency
	// channel names a path.
	manager.Error(context.Background(), "the line still has to go somewhere")
}

func TestTheRequestContextReachesEveryChannelLine(t *testing.T) {
	out := &buffer{}
	manager := log.NewLogManager(log.Config{
		Env:      "testing",
		Channels: map[string]log.ChannelConfig{"app": {Driver: "stderr", Writer: out}},
	}, nil)

	channel, err := manager.Channel("app")
	if err != nil {
		t.Fatalf("Channel: %v", err)
	}
	channel.Info(requestContext(t), "the line")

	if !strings.Contains(out.String(), `"order":"abc"`) {
		t.Fatalf("the request context did not reach the line: %s", out.String())
	}
	if strings.Contains(out.String(), "token") {
		t.Fatalf("the hidden half reached the line: %s", out.String())
	}
}

func TestTheManagerIsSafeUnderConcurrentUse(t *testing.T) {
	manager := log.NewLogManager(log.Config{
		Default: "one",
		Env:     "testing",
		Channels: map[string]log.ChannelConfig{
			"one":   {Driver: "null"},
			"two":   {Driver: "null"},
			"stack": {Driver: "stack", Channels: []string{"one", "two"}},
		},
	}, nil)

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := []string{"one", "two", "stack"}[i%3]
			if _, err := manager.Channel(name); err != nil {
				t.Errorf("Channel(%q): %v", name, err)
			}
			manager.ShareContext(map[string]any{"request_id": i})
			manager.Info(context.Background(), "the line")
			_ = manager.GetChannels()
			_ = manager.SharedContext()
		}()
	}
	wg.Wait()
}
