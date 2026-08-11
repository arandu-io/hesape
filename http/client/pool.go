package client

import (
	"fmt"
	"net/http"
	"sync"
)

// Pool mirrors Illuminate\Http\Client\Pool.
//
// It collects concurrent HTTP requests and sends them as a batch. Each
// request is keyed either by a numeric index (NewRequest) or by a
// string key (As).
type Pool struct {
	factory  *Factory
	requests []poolRequest
	mu       sync.Mutex
}

type poolRequest struct {
	key     string
	req     *http.Request
	pending *PendingRequest
	method  string
	url     string
	query   map[string]string
	data    any
}

// NewPool creates a Pool backed by the given Factory.
func NewPool(f *Factory) *Pool {
	return &Pool{factory: f}
}

// NewRequest adds a request with a numeric key.
func (p *Pool) NewRequest() *PendingRequest {
	pr := p.factory.CreatePendingRequest()
	p.requests = append(p.requests, poolRequest{
		key:     fmt.Sprintf("%d", len(p.requests)),
		pending: pr,
	})
	return pr
}

// As adds a request with a named key.
func (p *Pool) As(key string) *PendingRequest {
	pr := p.factory.CreatePendingRequest()
	p.requests = append(p.requests, poolRequest{
		key:     key,
		pending: pr,
	})
	return pr
}

// GetRequests returns all pending requests in the pool.
func (p *Pool) GetRequests() []poolRequest {
	return p.requests
}

// Send sends all requests in the pool concurrently and returns a map
// of key → Response. If concurrency is 0, all requests run concurrently.
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

			resp, err := r.pending.Send(r.method, r.url, r.query, r.data)
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

// __call-like proxy: delegates HTTP verb methods to a new request.
// In Go, callers use As(key).Get(url, nil) directly.
// Pool provides NewRequest() and As(key) as access points; callers chain
// from the returned *PendingRequest.
