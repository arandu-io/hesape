package client

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

// Pool collects concurrent HTTP requests and sends them as a batch. Each
// request is keyed either by a numeric index (NewRequest) or by a
// string key (As).
type Pool struct {
	factory  *Factory
	requests []poolRequest
	mu       sync.Mutex

	// keys maps a pending request this pool built to the key it answers
	// under, so a verb can find its slot without the caller passing the key
	// twice.
	keys map[*PendingRequest]string

	// unnamed counts the requests added without a name, which is what their
	// numeric key comes from.
	unnamed int
}

type poolRequest struct {
	key     string
	req     *http.Request
	pending *PendingRequest
	ctx     context.Context
	method  string
	url     string
	query   map[string]string
	data    any
}

// NewPool creates a Pool backed by the given Factory.
func NewPool(f *Factory) *Pool {
	return &Pool{factory: f, keys: make(map[*PendingRequest]string)}
}

// NewRequest adds a request with a numeric key.
//
// The key counts only the unnamed requests: a pool of As("a"), NewRequest(),
// As("b"), NewRequest() answers under "a", "0", "b" and "1".
func (p *Pool) NewRequest() *PendingRequest {
	p.mu.Lock()
	key := fmt.Sprintf("%d", p.unnamed)
	p.unnamed++
	p.mu.Unlock()
	return p.claim(key)
}

// As adds a request with a named key.
func (p *Pool) As(key string) *PendingRequest {
	return p.claim(key)
}

// claim builds a pending request bound to this pool under a key.
//
// The binding is what makes the verb record rather than send: a request the
// pool did not build has no pool, and behaves exactly as it did before.
func (p *Pool) claim(key string) *PendingRequest {
	pr := p.factory.CreatePendingRequest()
	pr.pool = p
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys[pr] = key
	return pr
}

// record is what a verb on a pooled request calls instead of sending.
//
// A second verb on the same pending request replaces the first rather than
// adding a second entry: pool.As("a").Get(...) followed by .Post(...) is one
// request too, because the key names one slot.
//
// The context the verb was called with is recorded along with it, so that
// [Pool.Send] makes each request under the context its own caller gave it.
func (p *Pool) record(ctx context.Context, pr *PendingRequest, method, url string, query map[string]string, data any) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key, ok := p.keys[pr]
	if !ok {
		return
	}
	entry := poolRequest{key: key, pending: pr, ctx: ctx, method: method, url: url, query: query, data: data}
	for i := range p.requests {
		if p.requests[i].key == key {
			p.requests[i] = entry
			return
		}
	}
	p.requests = append(p.requests, entry)
}

// GetRequests returns all pending requests in the pool.
func (p *Pool) GetRequests() []poolRequest {
	return p.requests
}

// Send sends all requests in the pool concurrently and returns a map
// of key → Response. If concurrency is 0, all requests run concurrently.
//
// Each request goes out under the context passed to the verb that added it, so
// a caller cancels the pool by cancelling what it handed the verbs.
func (p *Pool) Send(concurrency int) (map[string]*Response, error) {
	if concurrency <= 0 {
		concurrency = len(p.requests)
	}

	results := make(map[string]*Response, len(p.requests))
	var mu sync.Mutex
	var errs []error

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, r := range p.requests {
		wg.Add(1)
		go func(r poolRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			// Unbind before sending, or Send records the call a second time
			// instead of making it.
			r.pending.pool = nil

			resp, err := r.pending.Send(r.ctx, r.method, r.url, r.query, r.data)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			results[r.key] = resp
		}(r)
	}

	wg.Wait()

	if len(errs) > 0 {
		return results, fmt.Errorf("http client: pool: %d request(s) failed", len(errs))
	}

	return results, nil
}

// Pool provides NewRequest() and As(key) as access points; callers chain
// HTTP verb methods directly from the returned *PendingRequest, such as
// As(key).Get(ctx, url, nil).
