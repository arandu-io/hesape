package process

import "strings"

// FakeProcessResult is a ProcessResult that no program produced.
//
// It answers to Illuminate\Process\FakeProcessResult, and it is what
// Factory.Result hands back. A fake handler may return one, and a test may
// build one directly.
//
// It is a separate type from the real result, and not a convenience over it,
// because PHP normalises what a test writes into what a program would have
// printed -- and that normalisation is the whole of this type. See
// normalizeOutput.
type FakeProcessResult struct {
	command     string
	exitCode    int
	output      string
	errorOutput string
}

var _ ProcessResult = (*FakeProcessResult)(nil)

// NewFakeProcessResult builds a fake result. It is PHP's constructor, whose
// four arguments all have defaults; here they are all given, and the zero of
// each is the PHP default.
//
// output and errorOutput are the PHP array|string: pass a string for one run of
// text, or a []string for a list of lines. Anything else is treated as empty.
func NewFakeProcessResult(command string, exitCode int, output, errorOutput any) *FakeProcessResult {
	return &FakeProcessResult{
		command:     command,
		exitCode:    exitCode,
		output:      normalizeOutput(output),
		errorOutput: normalizeOutput(errorOutput),
	}
}

// normalizeOutput turns what a test wrote into what a program would have
// printed. It is PHP's protected normalizeOutput, and it is copied down to its
// two oddities, because a fake that ends differently from the real thing is a
// test that passes against a program it will not pass against.
//
// A string gets exactly one trailing newline, however many it had. A list of
// lines gets one newline between each and, unlike the string, none at the end
// -- PHP's rtrim runs after the implode there and before the concatenation
// here. And a string PHP calls empty answers empty, which includes "0", since
// that is what empty() says about it.
func normalizeOutput(output any) string {
	switch value := output.(type) {
	case nil:
		return ""
	case string:
		if value == "" || value == "0" {
			return ""
		}
		return strings.TrimRight(value, "\n") + "\n"
	case []string:
		if len(value) == 0 {
			return ""
		}
		var lines strings.Builder
		for _, line := range value {
			lines.WriteString(strings.TrimRight(line, "\n"))
			lines.WriteString("\n")
		}
		return strings.TrimRight(lines.String(), "\n")
	default:
		return ""
	}
}

// Command is the command line this result claims to come from. PHP's command.
func (r *FakeProcessResult) Command() string { return r.command }

// WithCommand is a copy of this result attached to the given command line.
//
// PHP's withCommand, and it returns a new value there too: a fake result
// written once in a test is handed to every command that matches the pattern,
// so stamping the command on the original would make the second match report
// the first one's command.
func (r *FakeProcessResult) WithCommand(command string) *FakeProcessResult {
	return &FakeProcessResult{
		command:     command,
		exitCode:    r.exitCode,
		output:      r.output,
		errorOutput: r.errorOutput,
	}
}

// Successful reports an exit code of zero. PHP's successful.
func (r *FakeProcessResult) Successful() bool { return r.exitCode == 0 }

// Failed is the other half of Successful. PHP's failed.
func (r *FakeProcessResult) Failed() bool { return !r.Successful() }

// ExitCode is the status this result claims. PHP's exitCode.
func (r *FakeProcessResult) ExitCode() int { return r.exitCode }

// Output is the standard output this result claims. PHP's output.
func (r *FakeProcessResult) Output() string { return r.output }

// SeeInOutput reports whether Output contains the given string. PHP's
// seeInOutput.
func (r *FakeProcessResult) SeeInOutput(output string) bool {
	return strings.Contains(r.Output(), output)
}

// ErrorOutput is the standard error this result claims. PHP's errorOutput.
func (r *FakeProcessResult) ErrorOutput() string { return r.errorOutput }

// SeeInErrorOutput reports whether ErrorOutput contains the given string. PHP's
// seeInErrorOutput.
func (r *FakeProcessResult) SeeInErrorOutput(output string) bool {
	return strings.Contains(r.ErrorOutput(), output)
}

// Throw reports a failed result as an error. PHP's throw, which throws where
// this returns (T, error).
func (r *FakeProcessResult) Throw(callback func(ProcessResult, *ProcessFailedException)) (ProcessResult, error) {
	return throw(r, callback)
}

// ThrowIf is Throw when the condition holds. PHP's throwIf.
func (r *FakeProcessResult) ThrowIf(condition bool, callback func(ProcessResult, *ProcessFailedException)) (ProcessResult, error) {
	return throwIf(r, condition, callback)
}
