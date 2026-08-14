package log

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	logcontext "github.com/arandu-io/hesape/log/context"
)

// Config answers config/logging.php: the default channel and the channels
// themselves.
//
// Illuminate reads this out of the container, through $app['config']. There is
// no container here (ADR 0001), so the manager is handed the configuration it
// would otherwise resolve.
type Config struct {
	// Default answers logging.default: the channel Driver and Channel resolve
	// when asked for none.
	Default string

	// Env is the application environment. It answers two things Illuminate reads
	// off the application: getFallbackChannelName, which names a channel that did
	// not name itself, and the choice of format for a channel that did not choose
	// one -- readable text under "dev", JSON everywhere else, which is the same
	// word New reads.
	Env string

	// Channels answers logging.channels.
	Channels map[string]ChannelConfig
}

// ChannelConfig answers one entry of logging.channels.
//
// The field names are the array keys Illuminate reads, capitalised. The keys
// that only exist to configure a Monolog handler this ecosystem does not carry
// -- handler, processors, formatter, tap, action_level -- have no field: see the
// package documentation for why.
type ChannelConfig struct {
	// Driver answers `driver`: "single", "daily", "monthly", "stack", "stderr",
	// "errorlog", "null", "custom", or a name registered with Extend.
	Driver string

	// Name answers `name`, the channel name stamped on every line. Empty falls
	// back to Config.Env, which is Illuminate's getFallbackChannelName.
	Name string

	// Level answers `level`, one of the eight PSR-3 names. Empty is "debug",
	// which is the default ParsesLogConfiguration::level applies.
	Level string

	// Path answers `path`, the file the single and daily drivers write to.
	Path string

	// Days answers `days`, how many daily files to keep. Zero is Illuminate's
	// default of 7. MaxFiles wins over it when both are set.
	Days int

	// MaxFiles answers `max_files`, which Illuminate reads before `days` on the
	// daily driver and is the only thing the monthly driver reads. Zero is the
	// default the driver names: 7 files for daily, 3 for monthly.
	//
	// It is the one thing here that comes from the current Laravel rather than
	// from the clone -- see the package documentation.
	MaxFiles int

	// Channels answers `channels`, the channels a stack fans out to.
	Channels []string

	// IgnoreExceptions answers `ignore_exceptions`: a stack whose handler fails
	// keeps going through the rest, which is what WhatFailureGroupHandler does.
	IgnoreExceptions bool

	// Format is "text" or "json". Empty follows Config.Env.
	//
	// It replaces Illuminate's `formatter`, which names a Monolog formatter
	// class. slog ships exactly these two handlers, and the package
	// documentation says why there is no third.
	Format string

	// Writer is where the stderr, errorlog and null drivers write, and it is the
	// Go stand-in for the stream a Monolog StreamHandler is opened on. Empty
	// means the destination the driver names.
	Writer io.Writer

	// Via answers `via` for the custom driver: the factory that builds the
	// logger.
	Via func(config ChannelConfig) (*slog.Logger, error)
}

// LogManager answers Illuminate\Log\LogManager: it resolves channels by name,
// caches them, shares context across them, and is itself a logger that writes to
// the default channel.
//
// The name is Illuminate's rather than the shorter one Go would pick, because
// LogManager is the name a Laravel developer already knows.
//
// A LogManager is safe for concurrent use.
type LogManager struct {
	mu             sync.Mutex
	config         Config
	dispatcher     Dispatcher
	channels       map[string]*Logger
	sharedContext  map[string]any
	customCreators map[string]func(config ChannelConfig) (*slog.Logger, error)
}

// NewLogManager answers Illuminate\Log\LogManager::__construct.
//
// Illuminate takes the application and reads the configuration out of it. Here
// the configuration arrives directly, and the dispatcher -- which Illuminate
// resolves as $app['events'] -- with it. Both may be zero: a manager with no
// channels resolves nothing and falls back to the emergency logger, which is
// what Illuminate does with a missing channel.
func NewLogManager(config Config, dispatcher Dispatcher) *LogManager {
	return &LogManager{
		config:         config,
		dispatcher:     dispatcher,
		channels:       map[string]*Logger{},
		sharedContext:  map[string]any{},
		customCreators: map[string]func(config ChannelConfig) (*slog.Logger, error){},
	}
}

// Build answers Illuminate\Log\LogManager::build: an on-demand channel from a
// configuration that is not in the file.
//
// It drops the previously built one first, under Illuminate's own name for the
// slot, "ondemand", so two Build calls never hand back the same logger.
func (m *LogManager) Build(config ChannelConfig) (*Logger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.channels, onDemandChannel)
	return m.getLocked(onDemandChannel, &config)
}

// onDemandChannel is the slot LogManager::build unsets and then resolves into.
const onDemandChannel = "ondemand"

// Stack answers Illuminate\Log\LogManager::stack: a new aggregate logger over
// the named channels.
//
// channel names the stack, and empty is PHP's null default, which falls back to
// Config.Env. The result is not cached, exactly as Illuminate does not cache it.
func (m *LogManager) Stack(channels []string, channel string) (*Logger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	logger, err := m.createStackDriverLocked(ChannelConfig{Channels: channels, Name: channel})
	if err != nil {
		return m.createEmergencyLoggerLocked(err), err
	}
	return NewLogger(logger, m.dispatcher).WithContext(m.sharedContext), nil
}

// Channel answers Illuminate\Log\LogManager::channel: the channel by name, or
// the default one when the name is empty.
//
// Illuminate swallows a failure into the emergency logger and returns it. This
// returns that same emergency logger and the failure, so a caller that wants to
// keep logging can ignore the error and a caller that wants to know can read it.
func (m *LogManager) Channel(channel string) (*Logger, error) {
	return m.Driver(channel)
}

// Driver answers Illuminate\Log\LogManager::driver, which is what Channel calls.
func (m *LogManager) Driver(driver string) (*Logger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getLocked(m.parseDriverLocked(driver), nil)
}

// getLocked answers Illuminate\Log\LogManager::get: the cache, then the resolve,
// then the shared context, and the emergency logger when any of it fails.
//
// The caller holds m.mu. It is recursive -- a stack resolves its members through
// it -- which is why the lock is taken by the exported methods and not here.
func (m *LogManager) getLocked(name string, config *ChannelConfig) (*Logger, error) {
	if cached, ok := m.channels[name]; ok {
		return cached, nil
	}

	logger, err := m.resolveLocked(name, config)
	if err != nil {
		return m.createEmergencyLoggerLocked(err), err
	}

	channel := NewLogger(logger, m.dispatcher).WithContext(m.sharedContext)
	m.channels[name] = channel
	return channel, nil
}

// resolveLocked answers Illuminate\Log\LogManager::resolve: the configuration
// for the name, then the driver that configuration asks for.
func (m *LogManager) resolveLocked(name string, config *ChannelConfig) (*slog.Logger, error) {
	if config == nil {
		found, ok := m.config.Channels[name]
		if !ok {
			return nil, fmt.Errorf("log: log [%s] is not defined", name)
		}
		config = &found
	}

	if creator, ok := m.customCreators[config.Driver]; ok {
		return creator(*config)
	}

	switch config.Driver {
	case "single":
		return m.createSingleDriverLocked(*config)
	case "daily":
		return m.createDailyDriverLocked(*config)
	case "monthly":
		return m.createMonthlyDriverLocked(*config)
	case "stack":
		return m.createStackDriverLocked(*config)
	case "stderr", "errorlog":
		return m.createErrorlogDriverLocked(*config)
	case "null":
		return m.createNullDriverLocked(*config)
	case "custom":
		return m.createCustomDriverLocked(*config)
	default:
		return nil, fmt.Errorf("log: driver [%s] is not supported", config.Driver)
	}
}

// createSingleDriverLocked answers
// Illuminate\Log\LogManager::createSingleDriver: one file, appended to.
func (m *LogManager) createSingleDriverLocked(config ChannelConfig) (*slog.Logger, error) {
	if config.Path == "" {
		return nil, errors.New("log: the single driver needs a path")
	}
	file, err := openLogFile(config.Path)
	if err != nil {
		return nil, err
	}
	return m.buildLoggerLocked(config, file)
}

// createDailyDriverLocked answers
// Illuminate\Log\LogManager::createDailyDriver: one file per day, keeping only
// the newest of them, which is Monolog's RotatingFileHandler.
//
// The count is `max_files`, then `days`, then 7 -- the clone reads only `days`,
// and `max_files` is the current Laravel's, which the package documentation
// names as the second source.
func (m *LogManager) createDailyDriverLocked(config ChannelConfig) (*slog.Logger, error) {
	keep := config.MaxFiles
	if keep <= 0 {
		keep = config.Days
	}
	if keep <= 0 {
		keep = defaultDailyFiles
	}
	return m.createRotatingDriverLocked(config, filePerDay, keep, "daily")
}

// createMonthlyDriverLocked answers
// Illuminate\Log\LogManager::createMonthlyDriver: one file per month, keeping
// the last `max_files` of them, three by default.
//
// It has no counterpart in the clone; it is the current Laravel's, and it is
// here because a `monthly` channel in config/logging.php is something a Laravel
// developer writes today and would otherwise fail as an unsupported driver.
func (m *LogManager) createMonthlyDriverLocked(config ChannelConfig) (*slog.Logger, error) {
	keep := config.MaxFiles
	if keep <= 0 {
		keep = defaultMonthlyFiles
	}
	return m.createRotatingDriverLocked(config, filePerMonth, keep, "monthly")
}

// createRotatingDriverLocked answers
// Illuminate\Log\LogManager::createRotatingDriver, which the daily and monthly
// drivers both go through.
func (m *LogManager) createRotatingDriverLocked(config ChannelConfig, format string, keep int, driver string) (*slog.Logger, error) {
	if config.Path == "" {
		return nil, fmt.Errorf("log: the %s driver needs a path", driver)
	}
	return m.buildLoggerLocked(config, &rotatingWriter{path: config.Path, format: format, keep: keep})
}

// The two date formats Monolog's RotatingFileHandler names FILE_PER_DAY and
// FILE_PER_MONTH, written as Go layouts. They are what goes in the file name
// between the base and the extension.
const (
	filePerDay   = time.DateOnly
	filePerMonth = "2006-01"
)

// The counts the two rotating drivers fall back to: Illuminate's
// `$config['max_files'] ?? $config['days'] ?? 7` and `$config['max_files'] ?? 3`.
const (
	defaultDailyFiles   = 7
	defaultMonthlyFiles = 3
)

// createStackDriverLocked answers
// Illuminate\Log\LogManager::createStackDriver: one logger over the handlers of
// every named channel.
//
// Illuminate stamps the record with the stack's own channel name and lets each
// handler format it. Here every handler keeps the name of the channel that
// configured it, because a stack in Go is those handlers themselves and stamping
// again would put two `channel` keys on one line.
func (m *LogManager) createStackDriverLocked(config ChannelConfig) (*slog.Logger, error) {
	if len(config.Channels) == 0 {
		return nil, errors.New("log: a stack needs at least one channel")
	}

	handlers := make([]slog.Handler, 0, len(config.Channels))
	for _, name := range config.Channels {
		channel, err := m.getLocked(strings.TrimSpace(name), nil)
		if err != nil {
			return nil, err
		}
		handlers = append(handlers, channel.GetLogger().Handler())
	}
	return slog.New(&multiHandler{handlers: handlers, ignoreExceptions: config.IgnoreExceptions}), nil
}

// createErrorlogDriverLocked answers
// Illuminate\Log\LogManager::createErrorlogDriver, and serves the `stderr`
// channel too: PHP's error_log writes to the process error output, and so does
// this.
func (m *LogManager) createErrorlogDriverLocked(config ChannelConfig) (*slog.Logger, error) {
	destination := config.Writer
	if destination == nil {
		destination = os.Stderr
	}
	return m.buildLoggerLocked(config, destination)
}

// createNullDriverLocked answers the `null` channel of config/logging.php, the
// one Illuminate resolves to when running unit tests: it discards everything.
func (m *LogManager) createNullDriverLocked(config ChannelConfig) (*slog.Logger, error) {
	return m.buildLoggerLocked(config, io.Discard)
}

// createCustomDriverLocked answers
// Illuminate\Log\LogManager::createCustomDriver: the factory named by `via`
// builds the logger.
func (m *LogManager) createCustomDriverLocked(config ChannelConfig) (*slog.Logger, error) {
	if config.Via == nil {
		return nil, errors.New("log: the custom driver needs a via factory")
	}
	return config.Via(config)
}

// buildLoggerLocked is the part every driver shares: the level, the format, the
// channel name, and the context processor Illuminate pushes onto every resolved
// channel.
func (m *LogManager) buildLoggerLocked(config ChannelConfig, destination io.Writer) (*slog.Logger, error) {
	level, err := m.levelLocked(config)
	if err != nil {
		return nil, err
	}
	handler := logcontext.NewContextLogProcessor(newHandler(destination, m.formatLocked(config), level))
	return slog.New(handler).With(slog.String("channel", m.parseChannelLocked(config))), nil
}

// levelLocked answers ParsesLogConfiguration::level: the configured level, or
// debug, and an error rather than the InvalidArgumentException PHP throws.
func (m *LogManager) levelLocked(config ChannelConfig) (slog.Level, error) {
	if config.Level == "" {
		return LevelDebug, nil
	}
	level, err := ParseLevel(config.Level)
	if err != nil {
		return LevelDebug, fmt.Errorf("log: invalid log level: %w", err)
	}
	return level, nil
}

// parseChannelLocked answers ParsesLogConfiguration::parseChannel together with
// LogManager::getFallbackChannelName: the configured name, then the environment,
// then "production" -- which is what Illuminate falls back to when the
// application has no bound env.
func (m *LogManager) parseChannelLocked(config ChannelConfig) string {
	if config.Name != "" {
		return config.Name
	}
	if m.config.Env != "" {
		return m.config.Env
	}
	return "production"
}

// formatLocked resolves the channel format: what it asked for, then what the
// environment implies.
func (m *LogManager) formatLocked(config ChannelConfig) string {
	if config.Format != "" {
		return strings.ToLower(config.Format)
	}
	if m.config.Env == "dev" {
		return "text"
	}
	return "json"
}

// parseDriverLocked answers Illuminate\Log\LogManager::parseDriver: the name,
// trimmed, or the default when none was given.
func (m *LogManager) parseDriverLocked(driver string) string {
	driver = strings.TrimSpace(driver)
	if driver == "" {
		driver = strings.TrimSpace(m.config.Default)
	}
	return driver
}

// createEmergencyLoggerLocked answers
// Illuminate\Log\LogManager::createEmergencyLogger: the logger that exists so a
// broken logging configuration does not take the request down with it.
//
// It writes to the path of an "emergency" channel when one is configured, and to
// the process error output otherwise, because there is no application storage
// path to guess at here. It logs the failure on the way out, exactly as
// Illuminate does inside its catch.
func (m *LogManager) createEmergencyLoggerLocked(cause error) *Logger {
	var destination io.Writer = os.Stderr
	if config, ok := m.config.Channels["emergency"]; ok && config.Path != "" {
		if file, err := openLogFile(config.Path); err == nil {
			destination = file
		}
	}

	logger := NewLogger(slog.New(newHandler(destination, m.formatLocked(ChannelConfig{}), LevelDebug)), m.dispatcher)
	logger.Emergency(context.Background(), "Unable to create configured logger. Using emergency logger.", map[string]any{
		"exception": cause.Error(),
	})
	return logger
}

// ShareContext answers Illuminate\Log\LogManager::shareContext: context that
// every channel gets, the ones already resolved included.
func (m *LogManager) ShareContext(fields map[string]any) *LogManager {
	m.mu.Lock()
	channels := slices.Collect(maps.Values(m.channels))
	maps.Copy(m.sharedContext, fields)
	m.mu.Unlock()

	for _, channel := range channels {
		channel.WithContext(fields)
	}
	return m
}

// SharedContext answers Illuminate\Log\LogManager::sharedContext: the context
// shared across channels and stacks.
//
// It is a copy, because the caller of a PHP array getter gets a copy too.
func (m *LogManager) SharedContext() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return maps.Clone(m.sharedContext)
}

// WithoutContext answers Illuminate\Log\LogManager::withoutContext: drop the
// given keys from every resolved channel, or clear them all.
//
// It leaves the shared context alone, exactly as Illuminate does -- the two are
// separate, and FlushSharedContext is the one that clears the shared half.
func (m *LogManager) WithoutContext(keys ...string) *LogManager {
	m.mu.Lock()
	channels := slices.Collect(maps.Values(m.channels))
	m.mu.Unlock()

	for _, channel := range channels {
		channel.WithoutContext(keys...)
	}
	return m
}

// FlushSharedContext answers Illuminate\Log\LogManager::flushSharedContext.
func (m *LogManager) FlushSharedContext() *LogManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sharedContext = map[string]any{}
	return m
}

// GetDefaultDriver answers Illuminate\Log\LogManager::getDefaultDriver:
// logging.default.
func (m *LogManager) GetDefaultDriver() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config.Default
}

// SetDefaultDriver answers Illuminate\Log\LogManager::setDefaultDriver.
func (m *LogManager) SetDefaultDriver(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Default = name
}

// Extend answers Illuminate\Log\LogManager::extend: register a factory for a
// driver name the manager does not know.
//
// PHP binds the closure to the manager so that it can reach the protected
// helpers. Nothing here is reachable that way, so the factory receives only the
// channel configuration, which is what the closure reads in practice.
func (m *LogManager) Extend(driver string, callback func(config ChannelConfig) (*slog.Logger, error)) *LogManager {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.customCreators[driver] = callback
	return m
}

// ForgetChannel answers Illuminate\Log\LogManager::forgetChannel: drop the
// resolved channel so the next call builds it again.
func (m *LogManager) ForgetChannel(driver string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.channels, m.parseDriverLocked(driver))
}

// GetChannels answers Illuminate\Log\LogManager::getChannels: every channel
// resolved so far, by name. It is a copy of the map, and the loggers in it are
// the live ones.
func (m *LogManager) GetChannels() map[string]*Logger {
	m.mu.Lock()
	defer m.mu.Unlock()
	return maps.Clone(m.channels)
}

// Emergency answers Illuminate\Log\LogManager::emergency: it writes to the
// default channel. The eight levels and Log all do this.
func (m *LogManager) Emergency(ctx context.Context, message any, fields ...map[string]any) {
	m.Log(ctx, LevelEmergency, message, fields...)
}

// Alert answers Illuminate\Log\LogManager::alert.
func (m *LogManager) Alert(ctx context.Context, message any, fields ...map[string]any) {
	m.Log(ctx, LevelAlert, message, fields...)
}

// Critical answers Illuminate\Log\LogManager::critical.
func (m *LogManager) Critical(ctx context.Context, message any, fields ...map[string]any) {
	m.Log(ctx, LevelCritical, message, fields...)
}

// Error answers Illuminate\Log\LogManager::error.
func (m *LogManager) Error(ctx context.Context, message any, fields ...map[string]any) {
	m.Log(ctx, LevelError, message, fields...)
}

// Warning answers Illuminate\Log\LogManager::warning.
func (m *LogManager) Warning(ctx context.Context, message any, fields ...map[string]any) {
	m.Log(ctx, LevelWarning, message, fields...)
}

// Notice answers Illuminate\Log\LogManager::notice.
func (m *LogManager) Notice(ctx context.Context, message any, fields ...map[string]any) {
	m.Log(ctx, LevelNotice, message, fields...)
}

// Info answers Illuminate\Log\LogManager::info.
func (m *LogManager) Info(ctx context.Context, message any, fields ...map[string]any) {
	m.Log(ctx, LevelInfo, message, fields...)
}

// Debug answers Illuminate\Log\LogManager::debug.
func (m *LogManager) Debug(ctx context.Context, message any, fields ...map[string]any) {
	m.Log(ctx, LevelDebug, message, fields...)
}

// Log answers Illuminate\Log\LogManager::log: log at an arbitrary level, on the
// default channel.
//
// A channel that will not resolve does not silence the line: Driver hands back
// the emergency logger, and the line goes there, which is the whole reason
// Illuminate builds one.
func (m *LogManager) Log(ctx context.Context, level slog.Level, message any, fields ...map[string]any) {
	channel, _ := m.Driver("")
	channel.Log(ctx, level, message, fields...)
}

// newHandler builds the handler for a format: the two slog ships, under the
// eight PSR-3 level names New also renders.
func newHandler(destination io.Writer, format string, level slog.Level) slog.Handler {
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.LevelKey {
				if l, ok := a.Value.Any().(slog.Level); ok {
					a.Value = slog.StringValue(levelName(l))
				}
			}
			return a
		},
	}
	if format == "text" {
		return slog.NewTextHandler(destination, opts)
	}
	return slog.NewJSONHandler(destination, opts)
}

// multiHandler is the stack: one record into every handler.
//
// ignoreExceptions answers `ignore_exceptions`, which in Illuminate wraps the
// handlers in a WhatFailureGroupHandler -- a handler that fails does not stop
// the ones after it and does not surface. Without it, the failures come back
// joined, because slog has somewhere to put them and Monolog does not.
type multiHandler struct {
	handlers         []slog.Handler
	ignoreExceptions bool
}

// Enabled reports whether any handler in the stack wants the level. Monolog asks
// the same question of the group.
func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, sub := range h.handlers {
		if sub.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

// Handle writes the record into every handler that wants it.
func (h *multiHandler) Handle(ctx context.Context, record slog.Record) error {
	var failures []error
	for _, sub := range h.handlers {
		if !sub.Enabled(ctx, record.Level) {
			continue
		}
		if err := sub.Handle(ctx, record.Clone()); err != nil {
			failures = append(failures, err)
		}
	}
	if h.ignoreExceptions {
		return nil
	}
	return errors.Join(failures...)
}

// WithAttrs pushes the attributes down into every handler.
func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		out[i] = sub.WithAttrs(slices.Clone(attrs))
	}
	return &multiHandler{handlers: out, ignoreExceptions: h.ignoreExceptions}
}

// WithGroup pushes the group down into every handler.
func (h *multiHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	out := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		out[i] = sub.WithGroup(name)
	}
	return &multiHandler{handlers: out, ignoreExceptions: h.ignoreExceptions}
}

// openLogFile opens a log file for appending, creating the directory it lives in
// the way Monolog's StreamHandler does.
func openLogFile(path string) (*os.File, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}

// rotatingWriter is Monolog's RotatingFileHandler: one file per period, named
// after the configured path with the date before the extension, and only the
// last `keep` of them kept.
//
// format is the period -- filePerDay or filePerMonth -- and it is the whole
// difference between the daily and the monthly driver, exactly as it is the
// whole difference in Monolog.
type rotatingWriter struct {
	mu      sync.Mutex
	path    string
	format  string
	keep    int
	current string
	file    *os.File
}

// Write appends to the current period's file, opening it -- and pruning the old
// ones -- the first time that period is written to.
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	format := w.format
	if format == "" {
		format = filePerDay
	}

	period := time.Now().Format(format)
	if w.file == nil || period != w.current {
		if w.file != nil {
			_ = w.file.Close()
			w.file = nil
		}
		file, err := openLogFile(rotatingPath(w.path, period))
		if err != nil {
			return 0, err
		}
		w.file, w.current = file, period
		w.prune()
	}
	return w.file.Write(p)
}

// rotatingPath is Monolog's default rotating filename: base-YYYY-MM-DD.ext for
// a daily channel and base-YYYY-MM.ext for a monthly one.
func rotatingPath(path, period string) string {
	ext := filepath.Ext(path)
	return strings.TrimSuffix(path, ext) + "-" + period + ext
}

// prune deletes everything past the newest `keep` files, which is what
// RotatingFileHandler does when it rotates.
func (w *rotatingWriter) prune() {
	if w.keep <= 0 {
		return
	}
	ext := filepath.Ext(w.path)
	matches, err := filepath.Glob(strings.TrimSuffix(w.path, ext) + "-*" + ext)
	if err != nil || len(matches) <= w.keep {
		return
	}
	// The names sort the way the dates do, so the oldest are at the front.
	slices.Sort(matches)
	for _, old := range matches[:len(matches)-w.keep] {
		_ = os.Remove(old)
	}
}
