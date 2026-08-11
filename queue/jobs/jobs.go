package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/database"
)

// DefaultQueue is where a job goes when nobody said otherwise.
const DefaultQueue = "default"

// Job is one unit of work.
//
// It is both halves of Laravel's split: the record a driver stores, and the
// handle Illuminate\Contracts\Queue\Job hands the worker. A job built by [New]
// carries no driver and can only be pushed; a job returned by a queue's Pop
// carries the queue it came off, and [Job.Release], [Job.Delete] and [Job.Fail]
// are how it settles itself.
//
// A job popped from a queue is used through a pointer, because releasing it and
// deleting it are decisions the worker and the middleware make about the same
// job and a copy would lose them.
type Job struct {
	// UUID is the deduplication key. It is stable across retries, which is what
	// makes a handler able to recognize work it already did. It answers both
	// uuid() and getJobId(): here they are the same value, because the id is
	// minted by the application rather than by the store (see database.NewID).
	UUID string
	// Queue separates work by urgency: a password reset email and a monthly
	// report should not wait behind each other.
	Queue string
	// Name routes the job to its handler: "invoice.send", "report.monthly".
	Name string
	// TenantID is who the work belongs to. It comes from the Grant at Push, and
	// the worker rebuilds a Grant from it -- a job with no tenant cannot be
	// scoped, and everything downstream of it reads across customers.
	TenantID string
	// Payload is the arguments, as JSON. Keep it to facts and ids: a payload
	// that says "look it up" is a payload that reads a row which has already
	// changed.
	Payload []byte
	// AuthorizedBy and Action record the Grant that pushed it, which is the
	// audit trail and what the worker reissues the work under.
	AuthorizedBy string
	Action       string
	// RunAt is when it becomes eligible. Zero means now.
	RunAt time.Time
	// Attempts counts the deliveries INCLUDING the current one: a job being
	// handled for the first time has Attempts == 1. LastError is why the most
	// recent one failed -- stored rather than logged, because the thing anyone
	// needs at 3am is "this failed twelve times with this message".
	Attempts  int
	LastError string

	// driver is the queue this job came off. It is nil on a job that was built
	// to be pushed and never popped, and that is what ErrDetached reports.
	driver Driver
	// connection names the queue connection, for the log line and for a handler
	// that pushes a follow-up onto the same one.
	connection string
	// released, deleted and failed are the bookkeeping of
	// Illuminate\Queue\Jobs\Job. The worker reads them to find out whether a
	// middleware already settled the job, because a job released by
	// WithoutOverlapping must not then be deleted by the success path.
	released bool
	deleted  bool
	failed   bool
}

// Driver is the three things a running job asks of the queue it came off.
//
// It is what DatabaseJob, RedisJob and SyncJob override in Laravel. A queue
// implements it and hands itself to [Popped], so the job can settle itself
// without the worker having to know which store it lives in.
type Driver interface {
	// ReleaseJob puts the job back on its queue, eligible again after delay.
	ReleaseJob(ctx context.Context, j *Job, delay time.Duration) error
	// DeleteJob removes a finished job.
	DeleteJob(ctx context.Context, j *Job) error
	// FailJob parks the job: it stops being delivered and starts being
	// something a person can list and retry.
	FailJob(ctx context.Context, j *Job, cause error) error
}

// Popped attaches a job to the queue it came off, and to that connection's
// name.
//
// It is what `new DatabaseJob($container, $this, $record, $connection, $queue)`
// is in Laravel, and every driver's Pop ends with it.
func Popped(d Driver, connection string, j Job) *Job {
	j.driver = d
	j.connection = connection
	return &j
}

// Connection is the name of the queue connection this job came off.
func (j *Job) Connection() string { return j.connection }

// Decode unmarshals the payload into v.
func (j *Job) Decode(v any) error {
	if err := json.Unmarshal(j.Payload, v); err != nil {
		return fmt.Errorf("queue: decoding %s: %w", j.Name, err)
	}
	return nil
}

// Release puts the job back on its queue, eligible again after delay.
//
// It answers release(). A middleware that decides the work should not run now
// calls it -- WithoutOverlapping when another worker holds the lock, RateLimited
// when the budget is spent -- and so does the worker after a failure that has
// attempts left.
func (j *Job) Release(ctx context.Context, delay time.Duration) error {
	if j.driver == nil {
		return ErrDetached
	}
	j.released = true
	return j.driver.ReleaseJob(ctx, j, delay)
}

// Delete removes the job. It answers delete().
func (j *Job) Delete(ctx context.Context) error {
	if j.driver == nil {
		return ErrDetached
	}
	j.deleted = true
	return j.driver.DeleteJob(ctx, j)
}

// Fail parks the job. It answers fail().
//
// Parked rather than retried: the job stops being delivered and starts being
// something a person can see, which is what a dead letter queue is for. It
// marks the job deleted too, exactly as fail() calls delete() in Laravel, so
// nothing downstream settles it a second time.
func (j *Job) Fail(ctx context.Context, cause error) error {
	if j.driver == nil {
		return ErrDetached
	}
	if cause != nil {
		j.LastError = cause.Error()
	}
	j.failed = true
	j.deleted = true
	return j.driver.FailJob(ctx, j, cause)
}

// Released reports whether the job was put back on its queue.
func (j *Job) Released() bool { return j.released }

// Deleted reports whether the job was removed.
func (j *Job) Deleted() bool { return j.deleted }

// HasFailed reports whether the job was parked.
func (j *Job) HasFailed() bool { return j.failed }

// DeletedOrReleased reports whether the job has already been settled.
//
// It answers isDeletedOrReleased(), and it is what the worker asks before it
// acts on the outcome of a handler: a middleware may have released the job
// before the handler ever ran, and settling it twice is how a job runs twice.
func (j *Job) DeletedOrReleased() bool { return j.deleted || j.released }

// ErrNoTenant is returned when a Grant carries no tenant.
//
// It is an error rather than a default, and that is RULE 14 with teeth: a job
// with no tenant cannot be scoped, and everything the handler touches would read
// across customers.
var ErrNoTenant = errors.New("queue: the Grant carries no tenant, and a job without one cannot be scoped")

// ErrNoName is returned when a job has no name to route by.
var ErrNoName = errors.New("queue: a job with no name cannot be routed to a handler")

// ErrForged is returned when a job claims an action or a tenant the Grant
// pushing it does not carry.
var ErrForged = errors.New("queue: the job does not match the Grant pushing it")

// ErrDetached is returned when a job that was never popped is asked to settle
// itself.
var ErrDetached = errors.New("queue: this job was built to be pushed and never came off a queue, so there is nothing to release, delete or fail")

// New builds a job from a Grant and a payload.
//
// This is the only constructor, so every job in the system carries a tenant, an
// id and the Grant that authorized it -- there is no shape of Job that skipped
// any of the three.
func New(g auth.Grant, queue, name string, payload any) (Job, error) {
	if name == "" {
		return Job{}, ErrNoName
	}
	tenant := auth.Tenant(g)
	if tenant == "" {
		return Job{}, ErrNoTenant
	}
	if queue == "" {
		queue = DefaultQueue
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Job{}, fmt.Errorf("queue: serializing %s: %w", name, err)
	}
	id, err := database.NewID()
	if err != nil {
		return Job{}, err
	}

	return Job{
		UUID:         id,
		Queue:        queue,
		Name:         name,
		TenantID:     tenant,
		Payload:      body,
		AuthorizedBy: g.Subject().ID,
		Action:       string(g.Action()),
	}, nil
}

// GrantFor rebuilds the Grant a job runs under.
//
// The action and the tenant come from the record, so the worker reissues
// exactly what the push authorized -- not more. A worker that invented its own
// Grant would be a way to reach the database with permissions nobody granted.
func GrantFor(j *Job) auth.Grant {
	return auth.SystemGrant(auth.Action(j.Action), j.TenantID)
}

// Authorized reports whether a job may be pushed under this Grant.
//
// Every driver calls it at the top of Push, and it closes an escalation the
// contract otherwise allows. [New] builds a job from the Grant, so what it
// produces always matches -- but Push takes a Job, and a Job is a struct
// anybody can fill in:
//
//	j := jobs.Job{UUID: id, Name: "invoice.send", Action: "invoice.delete", TenantID: other}
//	q.Push(ctx, viewGrant, j)
//
// The worker rebuilds the Grant from the record -- [GrantFor] gives
// auth.SystemGrant(j.Action, j.TenantID) -- so the handler would run with an
// action nobody authorized, in a tenant nobody authorized, and every Policy
// downstream would say yes because the Grant looks legitimate. The queue would
// be the one way past the authorization the whole collection exists to enforce.
// Found by audit.
//
// Checked here rather than in each driver, because a driver that forgets is a
// driver that reopens it.
func Authorized(g auth.Grant, j Job) error {
	tenant := auth.Tenant(g)
	if tenant == "" {
		return ErrNoTenant
	}
	if j.Name == "" {
		return ErrNoName
	}
	if j.TenantID != "" && j.TenantID != tenant {
		return fmt.Errorf("%w: the job says %q and the Grant says %q",
			ErrForged, j.TenantID, tenant)
	}
	if j.Action != "" && j.Action != string(g.Action()) {
		return fmt.Errorf("%w: the job says %q and the Grant authorizes %q. Build it with jobs.New, which takes both from the Grant",
			ErrForged, j.Action, g.Action())
	}
	return nil
}

// Prepare fills in the defaults a driver would otherwise each have to remember:
// the queue, the run time and the tenant.
//
// It runs after [Authorized] and returns the job a driver stores. Three drivers
// deciding independently what an empty Queue means is three chances to disagree.
func Prepare(g auth.Grant, j Job) Job {
	j.TenantID = auth.Tenant(g)
	if j.Queue == "" {
		j.Queue = DefaultQueue
	}
	// UTC, always. Every other timestamp a driver writes is UTC and every
	// comparison it makes is against time.Now().UTC(), but a RunAt supplied by
	// the caller kept whatever zone it arrived in -- so a job scheduled from a
	// UTC-3 machine was stored three hours in the past on an engine that
	// compares timestamps as text, and ran immediately. Found by audit.
	if j.RunAt.IsZero() {
		j.RunAt = time.Now().UTC()
	} else {
		j.RunAt = j.RunAt.UTC()
	}
	return j
}
