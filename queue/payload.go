package queue

import (
	"sync"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue/attributes"
	"github.com/arandu-io/hesape/queue/jobs"
)

// PayloadHook may add to a job on its way onto a queue.
//
// It answers the callbacks registered by [CreatePayloadUsing]. It is given the
// connection, the queue and the job as it stands; what it changes, it changes
// on the job.
//
// Laravel's hook returns an array that is merged over the payload, which is how
// an observability package adds a trace id to every job it never wrote. The Go
// form takes a pointer instead of returning a map, because merging untyped maps
// is how a hook silently overwrites maxTries.
type PayloadHook func(connection, queue string, j *jobs.Job)

var payloadHooks struct {
	sync.RWMutex
	hooks []PayloadHook
}

// CreatePayloadUsing registers a hook that runs while a job's record is built.
//
// It answers Illuminate\Queue\Queue::createPayloadUsing(), including the part
// nobody expects: passing nil clears the registered hooks rather than adding a
// nil one. That is what the PHP does, and a test that registers a hook has no
// other way to take it back.
//
// It is process-wide, exactly as the PHP static is. A hook is registered once
// at boot -- tracing, an audit field -- and never per request; anything that
// varies per job belongs on the job.
func CreatePayloadUsing(hook PayloadHook) {
	payloadHooks.Lock()
	defer payloadHooks.Unlock()
	if hook == nil {
		payloadHooks.hooks = nil
		return
	}
	payloadHooks.hooks = append(payloadHooks.hooks, hook)
}

// CreatePayload builds the record a driver stores.
//
// It answers Illuminate\Queue\Queue::createPayload(): it takes what the caller
// pushed and returns the thing that goes on the wire, with the identity, the
// tenant and the settings filled in.
//
// In Laravel the return is a JSON string, because a driver stores one blob per
// job and everything -- uuid, displayName, maxTries, the serialized command --
// has to fit inside it. Here it is a [jobs.Job], because the record has columns
// and the arguments are one of them. That is the only difference: the same
// values are decided here, at the same moment, for the same reason.
//
// delay is folded into RunAt rather than kept beside it. Laravel carries both a
// delay and an availability timestamp and has been bitten by them disagreeing;
// one field cannot.
func CreatePayload(g auth.Grant, connection, queue, name string, data any, delay time.Duration) (jobs.Job, error) {
	j, err := jobs.New(g, queue, name, data)
	if err != nil {
		return jobs.Job{}, err
	}

	j.Attributes = attributes.Of(data)
	if j.Attributes.Queue != "" && queue == "" {
		j.Queue = j.Attributes.Queue
	}
	j.DisplayName = GetDisplayName(data)
	if delay > 0 {
		j.RunAt = time.Now().UTC().Add(delay)
	}

	payloadHooks.RLock()
	hooks := payloadHooks.hooks
	payloadHooks.RUnlock()
	for _, hook := range hooks {
		hook(connection, j.Queue, &j)
	}
	return j, nil
}

// HasDisplayName is a job value that names itself for a person.
//
// It answers the `method_exists($job, 'displayName')` check in
// Queue::getDisplayName(). A value that does not implement it is named by the
// job name it was pushed under, which is what get_class($job) is in PHP.
type HasDisplayName interface {
	// DisplayName is what a console, a log line and the failed job list show.
	DisplayName() string
}

// GetDisplayName is what to show for a job's arguments, or empty when the value
// has nothing to say.
//
// It answers Queue::getDisplayName().
func GetDisplayName(data any) string {
	if named, says := data.(HasDisplayName); says {
		return named.DisplayName()
	}
	return ""
}

// GetJobTries is how many deliveries a job value asks for, or zero when it asks
// for nothing and the worker's own limit decides.
//
// It answers Queue::getJobTries(). In PHP it reads the #[Tries] attribute and
// then lets a tries() method override it; here both are the one field
// attributes.Attributes.Tries, because a value has one place to say it.
func GetJobTries(data any) int { return attributes.Of(data).Tries }

// GetJobBackoff is how long a job value asks to wait between deliveries, or nil
// when the worker's own schedule decides.
//
// It answers Queue::getJobBackoff(). The PHP returns a comma-separated string
// because the payload is JSON and the list has to survive it; here it is the
// slice it always was.
func GetJobBackoff(data any) []time.Duration { return attributes.Of(data).Backoff }

// GetJobExpiration is the deadline after which a job value stops being retried,
// or the zero time when there is none.
//
// It answers Queue::getJobExpiration(). The PHP returns a Unix timestamp; here
// it is a time.Time, which is the same instant with its unit attached.
func GetJobExpiration(data any) time.Time { return attributes.Of(data).RetryUntil }
