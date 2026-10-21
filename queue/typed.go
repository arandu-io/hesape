package queue

import (
	"context"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/queue/jobs"
)

// Dispatch builds a job named name around payload, on the default queue, and
// pushes it onto q under g.
//
// It is the two steps every dispatch function was written as -- jobs.New, then
// Push with the same Grant -- in one call whose payload is typed, so the value
// a caller hands over is the value HandlerFor[T] decodes on the other side. The
// job is still built by jobs.New, so it carries the tenant, an id and the Grant
// that authorized it, and fails with jobs.ErrNoTenant or jobs.ErrNoName the
// same way.
//
// A job for another queue, or one with attributes, is built with jobs.New and
// pushed with Push or PushOn; this is the common case, not a second contract.
func Dispatch[T any](ctx context.Context, q Queue, g auth.Grant, name string, payload T) error {
	j, err := jobs.New(g, jobs.DefaultQueue, name, payload)
	if err != nil {
		return err
	}
	return q.Push(ctx, g, j)
}

// HandlerFor adapts a function over a typed payload to a Handler: the job's
// payload is decoded into a T and handed over beside the job itself.
//
// The job stays in the signature because a handler needs more of it than the
// payload: j.UUID is the key a handler deduplicates on, since delivery is
// at-least-once, and j is what releases or parks the work.
//
// A payload that does not decode into T is returned as the handler's error,
// wrapped with the job's name, and f is not called -- a job whose arguments do
// not fit its handler is a job that fails, not one that runs on a zero value.
func HandlerFor[T any](f func(ctx context.Context, g auth.Grant, j *jobs.Job, payload T) error) Handler {
	return HandlerFunc(func(ctx context.Context, g auth.Grant, j *jobs.Job) error {
		var payload T
		if err := j.Decode(&payload); err != nil {
			return err
		}
		return f(ctx, g, j, payload)
	})
}
