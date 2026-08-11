package scheduling

import (
	"os"
	"strings"

	"github.com/arandu-io/hesape/console"
)

// CommandBuilder renders the shell line one event runs as.
//
// It answers Illuminate\Console\Scheduling\CommandBuilder. It is a type with one
// method and no state, exactly as the PHP is, because the two shapes it builds
// -- foreground and background -- differ enough to be worth naming separately.
type CommandBuilder struct{}

// BuildCommand renders the line.
//
// It answers CommandBuilder::buildCommand.
func (b CommandBuilder) BuildCommand(event *Event) string {
	if event.runInBackground {
		return b.buildBackgroundCommand(event)
	}
	return b.buildForegroundCommand(event)
}

// buildForegroundCommand is CommandBuilder::buildForegroundCommand: the command,
// its output redirected, and stderr folded into stdout.
func (b CommandBuilder) buildForegroundCommand(event *Event) string {
	redirect := " > "
	if event.ShouldAppendOutput {
		redirect = " >> "
	}
	return b.ensureCorrectUser(event, event.Command+redirect+escapeArgument(event.Output)+" 2>&1")
}

// buildBackgroundCommand is CommandBuilder::buildBackgroundCommand.
//
// The shape is the PHP's: the command and schedule:finish in one subshell, the
// whole thing backgrounded, and the exit status passed to schedule:finish so the
// after callbacks and the mutex release happen in the process that reports back
// rather than the one that walked away.
func (b CommandBuilder) buildBackgroundCommand(event *Event) string {
	redirect := " > "
	if event.ShouldAppendOutput {
		redirect = " >> "
	}

	output := escapeArgument(event.Output)
	finished := console.FormatCommandString("schedule:finish") + ` "` + event.MutexName() + `"`

	if os.PathSeparator == '\\' {
		return `start /b cmd /v:on /c "(` + event.Command + " & " + finished + ` ^!ERRORLEVEL^!)` + redirect + output + ` 2>&1"`
	}

	return b.ensureCorrectUser(event,
		"("+event.Command+redirect+output+" 2>&1 ; "+finished+` "$?") > `+
			escapeArgument(event.GetDefaultOutput())+" 2>&1 &")
}

// ensureCorrectUser is CommandBuilder::ensureCorrectUser: the sudo, and only
// where there is one.
func (b CommandBuilder) ensureCorrectUser(event *Event, command string) string {
	if event.user == "" || os.PathSeparator == '\\' {
		return command
	}
	return "sudo -u " + event.user + " -- sh -c '" + command + "'"
}

// escapeArgument quotes a value so the shell takes it as one word.
//
// It answers Illuminate\Support\ProcessUtils::escapeArgument. A path with a
// space in it is the ordinary case, and a path with a quote in it is the one
// this exists for.
func escapeArgument(value string) string {
	if value == "" {
		return `""`
	}
	if os.PathSeparator == '\\' {
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
