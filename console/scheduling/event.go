package scheduling

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/console"
)

// defaultExpiresAt is how many minutes an overlap mutex lives when the event
// does not say: one day.
const defaultExpiresAt = 1440

// Filter is a callback that decides whether an event may run.
type Filter func(ctx context.Context) bool

// Hook is a callback run before or after an event.
type Hook func(ctx context.Context) error

// Event is one scheduled command.
//
// Its fields are unexported, with setters and getters standing in for them,
// because Go cannot have a field and a method that share a name -- such as
// WithoutOverlapping.
//
// Two fields carry the event's authorization: Action, which the event's
// Grant is issued for, and PerTenant, which says the event is expanded to
// one run per tenant with a Grant each. Without them a scheduled task would
// be the one path into the database with no authorization on it.
type Event struct {
	// Command is the command line the event runs. CommandBuilder reads it.
	Command string

	// Output is where the command's output is sent. It is a path, and the
	// default is the null device of the system.
	Output string

	// ShouldAppendOutput says whether the output is appended rather than
	// replacing what is there.
	ShouldAppendOutput bool

	// Mutex is what stops the event from overlapping itself.
	Mutex EventMutex

	// MutexNameResolver overrides how the mutex is named. Nil means the default
	// name, which is a digest of the expression and the command.
	MutexNameResolver func(*Event) string

	// ExitCode is what the command ended with, once it has.
	ExitCode int

	expression    string
	repeatSeconds int
	timezone      *time.Location
	user          string
	environments  []string

	evenInMaintenanceMode bool
	withoutOverlapping    bool
	onOneServer           bool
	expiresAt             int
	runInBackground       bool
	description           string

	action    auth.Action
	perTenant bool
	tenant    string

	filters []Filter
	rejects []Filter

	beforeCallbacks []Hook
	afterCallbacks  []Hook

	// lastChecked is when the event was last tested for eligibility, and it is
	// what a sub-minute repeat counts from.
	lastChecked time.Time

	// now is the clock lastChecked and ShouldRepeatNow read. Nil means time.Now.
	//
	// The runner sets it to its own, so the loop that repeats an event within
	// the minute and the event deciding whether it is time agree on what time it
	// is. Nothing else sets it: an event asked in isolation reads the wall
	// clock.
	now func() time.Time

	// callback is set when the event is a closure rather than a command line,
	// and execute calls it instead of starting a process.
	//
	// It lives on Event and not only on CallbackEvent because Go has no virtual
	// dispatch: a Schedule holds *Event, and a run that went through the base
	// type would otherwise shell out to an empty command line.
	callback Callback

	// exception is what the callback failed with, kept so the after callbacks
	// run before it is returned -- which is the order Event.Run has.
	exception error
}

// NewEvent returns an event for a command line.
//
// It sets the mutex, the command and the timezone, and defaults the output
// for the system.
func NewEvent(mutex EventMutex, command string, timezone *time.Location) *Event {
	e := &Event{
		Mutex:      mutex,
		Command:    command,
		timezone:   timezone,
		expression: "* * * * *",
		expiresAt:  defaultExpiresAt,
	}
	e.Output = e.GetDefaultOutput()
	return e
}

// GetDefaultOutput is where output goes when nobody said.
func (e *Event) GetDefaultOutput() string {
	if os.PathSeparator == '\\' {
		return "NUL"
	}
	return "/dev/null"
}

// Run runs the event.
//
// An overlapping event returns without doing anything, and an event that was
// not sent to the background is finished -- callbacks and mutex release --
// as soon as it returns.
func (e *Event) Run(ctx context.Context) error {
	skip, err := e.ShouldSkipDueToOverlapping(ctx)
	if err != nil {
		return err
	}
	if skip {
		return nil
	}

	exitCode, err := e.start(ctx)
	if err != nil {
		return err
	}
	if e.runInBackground {
		return nil
	}

	finished := e.Finish(ctx, exitCode)
	// The callback's own error wins: it is what the work failed with, and
	// Finish already ran the after callbacks before this returns it.
	if e.exception != nil {
		return e.exception
	}
	return finished
}

// ShouldSkipDueToOverlapping reports whether another copy of the event holds
// the mutex.
func (e *Event) ShouldSkipDueToOverlapping(ctx context.Context) (bool, error) {
	if !e.withoutOverlapping || e.Mutex == nil {
		return false, nil
	}
	created, err := e.Mutex.Create(ctx, e)
	if err != nil {
		return false, err
	}
	return !created, nil
}

// IsRepeatable reports whether the event was asked to repeat within the minute.
func (e *Event) IsRepeatable() bool { return e.repeatSeconds > 0 }

// ShouldRepeatNow reports whether enough of the minute has passed for the
// next repeat.
//
// Runner.repeatEvents is what calls it: without a caller, EveryFifteenSeconds
// would be a schedule that ran once a minute instead of four times.
func (e *Event) ShouldRepeatNow() bool {
	if !e.IsRepeatable() || e.lastChecked.IsZero() {
		return false
	}
	return int(e.clock().Sub(e.lastChecked).Seconds()) >= e.repeatSeconds
}

// clock is the time the event reads, defaulted.
func (e *Event) clock() time.Time {
	if e.now != nil {
		return e.now()
	}
	return time.Now()
}

// useClock points the event at the runner's clock. A nil clock leaves the event
// on the wall clock.
func (e *Event) useClock(now func() time.Time) {
	if now != nil {
		e.now = now
	}
}

// RepeatSeconds reports how often the event repeats within a minute, or
// zero.
func (e *Event) RepeatSeconds() int { return e.repeatSeconds }

// start runs the before callbacks and then the command.
//
// The mutex is removed on failure: an event that threw on the way in must
// not leave its overlap lock behind, or it never runs again until the lock
// expires.
func (e *Event) start(ctx context.Context) (int, error) {
	if err := e.CallBeforeCallbacks(ctx); err != nil {
		e.removeMutex(ctx)
		return 1, err
	}
	exitCode, err := e.execute(ctx)
	if err != nil {
		e.removeMutex(ctx)
		return exitCode, err
	}
	return exitCode, nil
}

// execute runs the work: the closure when there is one, the process
// otherwise.
//
// It runs the built command line through the system shell: the command
// string carries redirections, and a redirection is the shell's.
//
// The returned error is a failure to run at all. A command that ran and
// exited non-zero is its status and no error, which is what the onFailure
// callbacks are for.
func (e *Event) execute(ctx context.Context) (int, error) {
	if e.callback != nil {
		e.exception = e.callback(ctx, e.Grant())
		if e.exception != nil {
			return 1, nil
		}
		return 0, nil
	}

	command := e.BuildCommand()
	if strings.TrimSpace(command) == "" {
		return 0, nil
	}

	shell, flag := "/bin/sh", "-c"
	if os.PathSeparator == '\\' {
		shell, flag = "cmd", "/c"
	}

	err := exec.CommandContext(ctx, shell, flag, command).Run()
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		// A non-zero status is the command's answer, not a failure to run it:
		// the onFailure callbacks are what report it.
		return exit.ExitCode(), nil
	}
	return 1, err
}

// Finish records the exit code, runs the after callbacks and releases the
// mutex.
//
// The mutex is released whatever the callbacks did.
func (e *Event) Finish(ctx context.Context, exitCode int) error {
	e.ExitCode = exitCode
	err := e.CallAfterCallbacks(ctx)
	e.removeMutex(ctx)
	return err
}

// CallBeforeCallbacks runs every callback registered with Before.
func (e *Event) CallBeforeCallbacks(ctx context.Context) error {
	for _, callback := range e.beforeCallbacks {
		if err := callback(ctx); err != nil {
			return err
		}
	}
	return nil
}

// CallAfterCallbacks runs every callback registered with Then.
//
// Every one of them runs even when an earlier one failed, and the first
// error is what comes back: a ping that could not be sent must not stop the
// mail that reports the output.
func (e *Event) CallAfterCallbacks(ctx context.Context) error {
	var first error
	for _, callback := range e.afterCallbacks {
		if err := callback(ctx); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// BuildCommand renders the command line the event runs.
func (e *Event) BuildCommand() string { return (CommandBuilder{}).BuildCommand(e) }

// IsDue reports whether the event should run now.
//
// It checks maintenance mode first, then the expression, then the
// environment.
func (e *Event) IsDue(now time.Time, environment string, downForMaintenance bool) bool {
	if !e.RunsInMaintenanceMode() && downForMaintenance {
		return false
	}
	return e.expressionPasses(now) && e.RunsInEnvironment(environment)
}

// RunsInMaintenanceMode reports whether the event runs while the application
// is down.
func (e *Event) RunsInMaintenanceMode() bool { return e.evenInMaintenanceMode }

// expressionPasses evaluates the cron expression in the event's timezone.
func (e *Event) expressionPasses(now time.Time) bool {
	expression, err := ParseCronExpression(e.expression)
	if err != nil {
		// An expression that does not parse fires never rather than always.
		// Schedule.Exec refuses one at registration, so reaching this means
		// somebody set the expression by hand afterwards.
		return false
	}
	if e.timezone != nil {
		now = now.In(e.timezone)
	}
	return expression.IsDue(now)
}

// RunsInEnvironment reports whether the event runs in the given environment.
//
// An event that named none runs in all.
func (e *Event) RunsInEnvironment(environment string) bool {
	return len(e.environments) == 0 || slices.Contains(e.environments, environment)
}

// FiltersPass reports whether every when passed and no skip matched.
//
// It records the time: a sub-minute repeat counts from the last check.
func (e *Event) FiltersPass(ctx context.Context) bool {
	e.lastChecked = e.clock()

	for _, filter := range e.filters {
		if !filter(ctx) {
			return false
		}
	}
	for _, reject := range e.rejects {
		if reject(ctx) {
			return false
		}
	}
	return true
}

// StoreOutput makes sure the output is written to a file rather than
// discarded.
func (e *Event) StoreOutput() *Event {
	e.ensureOutputIsBeingCaptured()
	return e
}

// SendOutputTo sends the command's output to a path.
func (e *Event) SendOutputTo(location string, append ...bool) *Event {
	e.Output = location
	e.ShouldAppendOutput = len(append) > 0 && append[0]
	return e
}

// AppendOutputTo adds the command's output to what is already at a path.
func (e *Event) AppendOutputTo(location string) *Event { return e.SendOutputTo(location, true) }

// ensureOutputIsBeingCaptured points an event whose output goes to the null
// device at a log file first, because there is nothing to mail or read
// otherwise.
func (e *Event) ensureOutputIsBeingCaptured() {
	if e.Output == "" || e.Output == e.GetDefaultOutput() {
		e.SendOutputTo(filepathJoin("storage", "logs", "schedule-"+sha1Hex(e.MutexName())+".log"))
	}
}

// Mailer is what EmailOutputTo sends through.
//
// It is declared here rather than imported: the mail package is not a dependency
// of the scheduler, and what an event needs of a mailer is one method.
type Mailer interface {
	// Raw sends a plain text message.
	Raw(ctx context.Context, addresses []string, subject, body string) error
}

// Pinger is what the ping callbacks call.
//
// It is declared here for the same reason Mailer is: a GET to a URL is the whole
// of what an event asks of an HTTP client.
type Pinger interface {
	// Ping makes a GET request to the URL.
	Ping(ctx context.Context, url string) error
}

// EmailOutputTo mails the output of the event.
//
// onlyIfOutputExists defaults to true: a task that printed nothing sends no
// mail.
func (e *Event) EmailOutputTo(mailer Mailer, addresses []string, onlyIfOutputExists ...bool) *Event {
	e.ensureOutputIsBeingCaptured()
	only := true
	if len(onlyIfOutputExists) > 0 {
		only = onlyIfOutputExists[0]
	}
	return e.Then(func(ctx context.Context) error {
		return e.emailOutput(ctx, mailer, addresses, only)
	})
}

// EmailWrittenOutputTo mails the output only when there was some.
func (e *Event) EmailWrittenOutputTo(mailer Mailer, addresses []string) *Event {
	return e.EmailOutputTo(mailer, addresses, true)
}

// EmailOutputOnFailure mails the output when the command failed.
//
// It mails even when there was no output: a task that failed silently is the
// one worth reading.
func (e *Event) EmailOutputOnFailure(mailer Mailer, addresses []string) *Event {
	e.ensureOutputIsBeingCaptured()
	return e.OnFailure(func(ctx context.Context) error {
		return e.emailOutput(ctx, mailer, addresses, false)
	})
}

// emailOutput reads the event's output and mails it through mailer, unless
// onlyIfOutputExists is true and there was none.
func (e *Event) emailOutput(ctx context.Context, mailer Mailer, addresses []string, onlyIfOutputExists bool) error {
	if mailer == nil {
		return errors.New("scheduling: the event was asked to mail its output and no mailer was given")
	}
	text := ""
	if contents, err := os.ReadFile(e.Output); err == nil {
		text = string(contents)
	}
	if onlyIfOutputExists && strings.TrimSpace(text) == "" {
		return nil
	}
	return mailer.Raw(ctx, addresses, e.getEmailSubject(), text)
}

// getEmailSubject is the event's description, or a default naming the
// command.
func (e *Event) getEmailSubject() string {
	if e.description != "" {
		return e.description
	}
	return fmt.Sprintf("Scheduled Job Output For [%s]", e.Command)
}

// PingBefore makes a GET to the URL before the event runs.
func (e *Event) PingBefore(pinger Pinger, url string) *Event {
	return e.Before(pingCallback(pinger, url))
}

// PingBeforeIf makes the ping before, when the condition holds.
func (e *Event) PingBeforeIf(condition bool, pinger Pinger, url string) *Event {
	if condition {
		return e.PingBefore(pinger, url)
	}
	return e
}

// ThenPing makes a GET to the URL after the event runs.
func (e *Event) ThenPing(pinger Pinger, url string) *Event {
	return e.Then(pingCallback(pinger, url))
}

// ThenPingIf makes the ping after, when the condition holds.
func (e *Event) ThenPingIf(condition bool, pinger Pinger, url string) *Event {
	if condition {
		return e.ThenPing(pinger, url)
	}
	return e
}

// PingOnSuccess makes a GET to the URL when the event succeeded.
func (e *Event) PingOnSuccess(pinger Pinger, url string) *Event {
	return e.OnSuccess(pingCallback(pinger, url))
}

// PingOnSuccessIf makes the ping on success, when the condition holds.
func (e *Event) PingOnSuccessIf(condition bool, pinger Pinger, url string) *Event {
	if condition {
		return e.PingOnSuccess(pinger, url)
	}
	return e
}

// PingOnFailure makes a GET to the URL when the event failed.
func (e *Event) PingOnFailure(pinger Pinger, url string) *Event {
	return e.OnFailure(pingCallback(pinger, url))
}

// PingOnFailureIf makes the ping on failure, when the condition holds.
func (e *Event) PingOnFailureIf(condition bool, pinger Pinger, url string) *Event {
	if condition {
		return e.PingOnFailure(pinger, url)
	}
	return e
}

// pingCallback returns a hook that makes a GET request to url through pinger.
func pingCallback(pinger Pinger, url string) Hook {
	return func(ctx context.Context) error {
		if pinger == nil {
			return errors.New("scheduling: the event was asked to ping and no pinger was given")
		}
		return pinger.Ping(ctx, url)
	}
}

// Before registers a callback run before the event.
func (e *Event) Before(callback Hook) *Event {
	if callback != nil {
		e.beforeCallbacks = append(e.beforeCallbacks, callback)
	}
	return e
}

// After registers a callback run after the event.
//
// It is one call to Then.
func (e *Event) After(callback Hook) *Event { return e.Then(callback) }

// Then registers a callback run after the event.
func (e *Event) Then(callback Hook) *Event {
	if callback != nil {
		e.afterCallbacks = append(e.afterCallbacks, callback)
	}
	return e
}

// ThenWithOutput registers a callback that is handed what the event printed.
func (e *Event) ThenWithOutput(callback func(ctx context.Context, output string) error, onlyIfOutputExists ...bool) *Event {
	e.ensureOutputIsBeingCaptured()
	return e.Then(e.withOutputCallback(callback, onlyIfOutputExists...))
}

// OnSuccess registers a callback run when the event ended with status zero.
func (e *Event) OnSuccess(callback Hook) *Event {
	return e.Then(func(ctx context.Context) error {
		if e.ExitCode == 0 {
			return callback(ctx)
		}
		return nil
	})
}

// OnSuccessWithOutput is OnSuccess with the output handed to the callback.
func (e *Event) OnSuccessWithOutput(callback func(ctx context.Context, output string) error, onlyIfOutputExists ...bool) *Event {
	e.ensureOutputIsBeingCaptured()
	return e.OnSuccess(e.withOutputCallback(callback, onlyIfOutputExists...))
}

// OnFailure registers a callback run when the event ended badly.
func (e *Event) OnFailure(callback Hook) *Event {
	return e.Then(func(ctx context.Context) error {
		if e.ExitCode != 0 {
			return callback(ctx)
		}
		return nil
	})
}

// OnFailureWithOutput is OnFailure with the output handed to the callback.
func (e *Event) OnFailureWithOutput(callback func(ctx context.Context, output string) error, onlyIfOutputExists ...bool) *Event {
	e.ensureOutputIsBeingCaptured()
	return e.OnFailure(e.withOutputCallback(callback, onlyIfOutputExists...))
}

// withOutputCallback returns a hook that reads the event's output and hands
// it to callback, skipping the call when onlyIfOutputExists is true and
// there was none.
func (e *Event) withOutputCallback(callback func(ctx context.Context, output string) error, onlyIfOutputExists ...bool) Hook {
	only := len(onlyIfOutputExists) > 0 && onlyIfOutputExists[0]
	return func(ctx context.Context) error {
		output := ""
		if contents, err := os.ReadFile(e.Output); err == nil {
			output = string(contents)
		}
		if only && strings.TrimSpace(output) == "" {
			return nil
		}
		return callback(ctx, output)
	}
}

// GetSummaryForDisplay is the event as a listing prints it.
//
// It is the description when it has one, the command line when it is a
// command, and "Callback" when it is a closure that was never named.
func (e *Event) GetSummaryForDisplay() string {
	if e.description != "" {
		return e.description
	}
	if e.callback != nil {
		return "Callback"
	}
	return e.BuildCommand()
}

// NextRunDate is when the event runs next, after the given time.
//
// To find the nth occurrence, call it again with each result: there is no
// argument for walking further down the list in one call.
func (e *Event) NextRunDate(after time.Time) time.Time {
	expression, err := ParseCronExpression(e.expression)
	if err != nil {
		return time.Time{}
	}
	if e.timezone != nil {
		after = after.In(e.timezone)
	}
	return expression.GetNextRunDate(after)
}

// GetExpression is the cron expression the event fires on.
func (e *Event) GetExpression() string { return e.expression }

// PreventOverlapsUsing points the event at another mutex.
func (e *Event) PreventOverlapsUsing(mutex EventMutex) *Event {
	e.Mutex = mutex
	return e
}

// MutexName is the key the event's overlap lock is stored under.
//
// The name is derived from the expression and the command, so the same
// event on two replicas resolves to the same key and one of them loses the
// race.
func (e *Event) MutexName() string {
	if e.MutexNameResolver != nil {
		return e.MutexNameResolver(e)
	}
	return "framework/schedule-" + sha1Hex(e.expression+NormalizeCommand(e.Command))
}

// CreateMutexNameUsing overrides how the mutex is named.
func (e *Event) CreateMutexNameUsing(resolver func(*Event) string) *Event {
	e.MutexNameResolver = resolver
	return e
}

// removeMutex releases the mutex, and only when the event asked for the lock
// in the first place.
func (e *Event) removeMutex(ctx context.Context) {
	if e.withoutOverlapping && e.Mutex != nil {
		_ = e.Mutex.Forget(ctx, e)
	}
}

// NormalizeCommand rewrites the running binary's path out of a command line.
//
// It exists so the mutex name of an event does not change when the binary
// moves: two replicas installed under different paths must resolve to the
// same lock, or neither ever loses the race.
func NormalizeCommand(command string) string {
	binary := console.PhpBinary()
	if binary == "" {
		return command
	}
	return strings.ReplaceAll(command, binary, "app")
}

// sha1Hex is the digest the mutex names use. It names a lock, it does not
// protect anything, so the digest only needs to be deterministic and short.
func sha1Hex(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// filepathJoin builds a path with the separator of the system.
func filepathJoin(parts ...string) string {
	return strings.Join(parts, string(os.PathSeparator))
}
