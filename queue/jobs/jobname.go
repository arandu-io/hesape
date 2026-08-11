package jobs

import "strings"

// JobName reads the several names a job has.
//
// It answers Illuminate\Queue\Jobs\JobName, which in PHP is a class with three
// static methods and no state. It is a struct with no fields here for the same
// reason it is a class there: the three belong together and none of them needs
// a receiver, so `jobs.JobName{}.Resolve(...)` reads the way `JobName::resolve`
// does and keeps the class name a Laravel developer already knows.
//
// A job has three names because a wrapper has two:
//
//	Name          what routes it to a handler
//	DisplayName   what a person reads
//	class name    what is really behind a wrapper
//
// For an ordinary job all three are the same string, and these three methods
// are what say so in one place instead of at every call site.
type JobName struct{}

// Parse splits a job name into the name and the method behind it.
//
// It answers JobName::parse(), which is Str::parseCallback($job, 'fire'): in
// Laravel a payload's job key is "App\Jobs\SendInvoice@handle" and this is what
// separates the class from the method. Here a name is "invoice.send" and there
// is no method to call, so a name with no "@" comes back with the default --
// which is exactly what parseCallback does with a class that has none.
//
// It is kept because a payload written by an older release, or by a bridge from
// a PHP application pushing onto the same store, still carries the "@" form,
// and silently routing "SendInvoice@handle" to a handler registered as
// "SendInvoice" is better than failing to route it at all.
func (JobName) Parse(name string) (string, string) {
	if class, method, found := strings.Cut(name, "@"); found {
		return class, method
	}
	return name, "fire"
}

// Resolve is the name to show for a job.
//
// It answers JobName::resolve(), which prefers the payload's displayName over
// the class. A nil job, or one with no DisplayName, answers with name.
func (JobName) Resolve(name string, j *Job) string {
	if j != nil && j.DisplayName != "" {
		return j.DisplayName
	}
	return name
}

// ResolveClassName is the name of the work behind a wrapper.
//
// It answers JobName::resolveClassName(), which digs data.commandName out of
// the payload because in Laravel a queued closure and a queued listener both
// arrive as CallQueuedHandler and their own class is inside. Here the name that
// routes is the name that was registered, so this is that name -- the two only
// diverge in Laravel because the wrapper is a class and the routing key is the
// class.
func (JobName) ResolveClassName(name string, j *Job) string {
	if j != nil && j.Name != "" {
		return j.Name
	}
	class, _ := JobName{}.Parse(name)
	return class
}
