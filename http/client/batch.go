package client

import (
	"sync"
	"time"

	"github.com/arandu-io/hesape/support/deferpkg"
)

// Batch collects requests to send concurrently, with callbacks for
// progress, error handling, and completion. It uses goroutines to send
// them.
type Batch struct {
	factory     *Factory
	requests    []batchRequest
	concurrency int
	sent        bool

	before   func()
	progress func(*Response, int)
	onError  func(error, int)
	then     func(map[string]*Response)
	finally  func()

	totalRequests   int
	pendingRequests int
	failedRequests  int
	createdAt       time.Time
	finishedAt      time.Time
}

type batchRequest struct {
	key     string
	pending *PendingRequest
	method  string
	url     string
	query   map[string]string
	data    any
}

// NewBatch creates a Batch backed by the given Factory.
func NewBatch(f *Factory) *Batch {
	return &Batch{
		factory:     f,
		createdAt:   time.Now(),
		concurrency: 5,
	}
}

// As adds a named request to the batch.
func (b *Batch) As(key string) *PendingRequest {
	pr := b.factory.CreatePendingRequest()
	b.requests = append(b.requests, batchRequest{
		key:     key,
		pending: pr,
	})
	return pr
}

// NewRequest adds an unnamed request to the batch.
func (b *Batch) NewRequest() *PendingRequest {
	return b.As("")
}

// Before registers a callback that runs before any request is sent.
func (b *Batch) Before(callback func()) *Batch {
	b.before = callback
	return b
}

// Progress registers a callback that runs after each successful request.
// It receives the Response and the index of the completed request.
func (b *Batch) Progress(callback func(*Response, int)) *Batch {
	b.progress = callback
	return b
}

// Catch registers a callback that runs when a request fails.
func (b *Batch) Catch(callback func(error, int)) *Batch {
	b.onError = callback
	return b
}

// Then registers a callback that runs when all requests succeed.
func (b *Batch) Then(callback func(map[string]*Response)) *Batch {
	b.then = callback
	return b
}

// Finally registers a callback that runs after all requests complete,
// regardless of success or failure.
func (b *Batch) Finally(callback func()) *Batch {
	b.finally = callback
	return b
}

// Concurrency sets the maximum number of concurrent requests.
func (b *Batch) Concurrency(limit int) *Batch {
	b.concurrency = limit
	return b
}

// Defer sends the batch after the response has gone back to the browser,
// rather than while it is waiting.
//
// The callback it hands back is the one the deferred-callback collection
// invokes; nothing here runs it, which is what makes it deferred.
func (b *Batch) Defer() *deferpkg.DeferredCallback {
	return deferpkg.NewDeferredCallback(func() {
		// The batch reports failures through the Catch callback the caller
		// registered; there is nobody left to hand an error to out here.
		_, _ = b.Send()
	}, "", false)
}

// Send executes all requests in the batch.
func (b *Batch) Send() (map[string]*Response, error) {
	if b.sent {
		return nil, NewBatchInProgressError()
	}
	b.sent = true
	b.totalRequests = len(b.requests)
	b.pendingRequests = b.totalRequests

	if b.before != nil {
		b.before()
	}

	results := make(map[string]*Response, len(b.requests))
	var mu sync.Mutex
	var anyErr error

	sem := make(chan struct{}, b.concurrency)
	var wg sync.WaitGroup

	for i, r := range b.requests {
		wg.Add(1)
		go func(idx int, r batchRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			resp, err := r.pending.Send(r.method, r.url, r.query, r.data)

			mu.Lock()
			defer mu.Unlock()

			b.pendingRequests--

			if err != nil {
				b.failedRequests++
				if b.onError != nil {
					b.onError(err, idx)
				}
				anyErr = err
				return
			}

			results[r.key] = resp

			if b.progress != nil {
				b.progress(resp, idx)
			}
		}(i, r)
	}

	wg.Wait()

	b.finishedAt = time.Now()

	if b.then != nil && anyErr == nil && b.failedRequests == 0 {
		b.then(results)
	}

	if b.finally != nil {
		b.finally()
	}

	return results, anyErr
}

// ProcessedRequests returns the number of requests that have been processed.
func (b *Batch) ProcessedRequests() int {
	return b.totalRequests - b.pendingRequests
}

// Finished reports whether the batch has finished executing.
func (b *Batch) Finished() bool {
	return !b.finishedAt.IsZero()
}

// HasFailures reports whether any request in the batch failed.
func (b *Batch) HasFailures() bool {
	return b.failedRequests > 0
}

// GetRequests returns all requests in the batch.
func (b *Batch) GetRequests() []batchRequest {
	return b.requests
}
