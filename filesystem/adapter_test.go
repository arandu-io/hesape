package filesystem_test

import (
	"bytes"
	"context"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/filesystem"
)

// memAdapter is the smallest thing that is honestly an Adapter: a map from
// stored path to bytes.
//
// It is here and not in a package of its own because the assertions below are
// about this package's behaviour, not the driver's -- the driver suite lives
// with the drivers. What it does prove is that an Adapter never learns which
// tenant it is serving: the only thing it is ever handed is a path.
type memAdapter struct {
	mu    sync.Mutex
	files map[string]memFile
	// seen records every path the adapter was asked about, so a test can assert
	// on what crossed the boundary.
	seen []string
}

type memFile struct {
	body        []byte
	contentType string
	modified    time.Time
}

func newMem() *memAdapter { return &memAdapter{files: map[string]memFile{}} }

// seekBody is what a real driver hands back: something that can seek, which is
// what http.ServeContent needs for a Range request.
type seekBody struct{ *bytes.Reader }

func (seekBody) Close() error { return nil }

func (m *memAdapter) record(path string) {
	m.seen = append(m.seen, path)
}

func (m *memAdapter) Put(_ context.Context, path string, body io.Reader, contentType string) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(path)
	m.files[path] = memFile{body: b, contentType: contentType, modified: time.Unix(1754800000, 0).UTC()}
	return nil
}

func (m *memAdapter) Get(_ context.Context, path string) (filesystem.File, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(path)
	f, ok := m.files[path]
	if !ok {
		return filesystem.File{}, filesystem.ErrNotFound
	}
	return filesystem.File{
		Info: filesystem.Info{
			Key:         path,
			Size:        int64(len(f.body)),
			ContentType: f.contentType,
			ModifiedAt:  f.modified,
		},
		Body: seekBody{bytes.NewReader(f.body)},
	}, nil
}

func (m *memAdapter) Stat(_ context.Context, path string) (filesystem.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(path)
	f, ok := m.files[path]
	if !ok {
		return filesystem.Info{}, filesystem.ErrNotFound
	}
	return filesystem.Info{
		Key:         path,
		Size:        int64(len(f.body)),
		ContentType: f.contentType,
		ModifiedAt:  f.modified,
	}, nil
}

func (m *memAdapter) Exists(_ context.Context, path string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(path)
	_, ok := m.files[path]
	return ok, nil
}

func (m *memAdapter) Delete(_ context.Context, path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(path)
	delete(m.files, path)
	return nil
}

func (m *memAdapter) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.record(prefix)
	var out []string
	for path := range m.files {
		if strings.HasPrefix(path, prefix) {
			out = append(out, path)
		}
	}
	// Reverse-sorted on purpose: Disk.List promises an order, and a driver that
	// happens to answer in the right one would hide it if this did too.
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}

// streamAdapter is a memAdapter whose bodies cannot seek, which is the other
// half of what Send has to handle.
type streamAdapter struct{ *memAdapter }

func (s streamAdapter) Get(ctx context.Context, path string) (filesystem.File, error) {
	f, err := s.memAdapter.Get(ctx, path)
	if err != nil {
		return filesystem.File{}, err
	}
	body, err := io.ReadAll(f.Body)
	if err != nil {
		return filesystem.File{}, err
	}
	f.Body = io.NopCloser(bytes.NewReader(body))
	return f, nil
}

// presignAdapter is a memAdapter that can hand out a URL of its own.
type presignAdapter struct{ *memAdapter }

func (p presignAdapter) PresignGet(_ context.Context, path string, ttl time.Duration) (string, error) {
	return "https://store.example/" + path + "?expires=" + ttl.String(), nil
}
