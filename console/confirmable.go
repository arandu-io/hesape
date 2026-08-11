package console

import "sync"

// ConfirmToProceed asks before a destructive command runs.
//
// It answers ConfirmableTrait::confirmToProceed, order included: --force is
// checked first and skips the question, then the warning is rendered as an
// alert, then the question is put with a default of no. An answer that is not
// yes prints "Command cancelled." and returns false, and the command returns
// without doing anything.
//
// shouldConfirm is the callback PHP defaults to "the environment is production".
// It is passed rather than read, because a package that reaches for the
// environment is a package no test can put in either one.
//
// It is the interactive guard. GuardEnv in exit.go is the other one: it refuses
// outright, for the pipeline where there is nobody at the keyboard to answer.
func (o *IO) ConfirmToProceed(warning string, shouldConfirm bool) (bool, error) {
	if !shouldConfirm {
		return true, nil
	}
	if o.HasOption("force") && o.Option("force").Bool() {
		return true, nil
	}

	if warning == "" {
		warning = "Application In Production"
	}
	o.OutputComponents().Alert(warning)

	confirmed, err := o.Confirm("Are you sure you want to run this command?", false)
	if err != nil {
		return false, err
	}
	if !confirmed {
		o.OutputComponents().Warn("Command cancelled")
		return false, nil
	}
	return true, nil
}

// prohibited is which commands have been prohibited from running.
//
// PHP keeps a static bool per command class; a map keyed by name is the same
// thing for a Command that is a value rather than a class. It is guarded because
// a test suite may prohibit from one goroutine while another dispatches.
var prohibited struct {
	sync.RWMutex
	names map[string]bool
}

// Prohibit stops a command from running, or lets it run again.
//
// It answers Prohibitable::prohibit, default argument included: calling it with
// no second value prohibits. It is what a test suite calls so a destructive
// command cannot fire from inside a test run.
func Prohibit(name string, prohibit ...bool) {
	value := true
	if len(prohibit) > 0 {
		value = prohibit[0]
	}

	prohibited.Lock()
	defer prohibited.Unlock()
	if prohibited.names == nil {
		prohibited.names = map[string]bool{}
	}
	prohibited.names[name] = value
}

// IsProhibited reports whether the command was prohibited, and says so on the
// terminal unless it was asked to be quiet.
//
// It answers Prohibitable::isProhibited, the quiet argument included.
func IsProhibited(name string, o *IO, quiet ...bool) bool {
	prohibited.RLock()
	stopped := prohibited.names[name]
	prohibited.RUnlock()

	if !stopped {
		return false
	}
	silent := len(quiet) > 0 && quiet[0]
	if !silent && o != nil {
		o.OutputComponents().Warn("This command is prohibited from running in this environment")
	}
	return true
}
