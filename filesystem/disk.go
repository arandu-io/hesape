package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/arandu-io/hesape/auth"
)

// ErrNotFound is returned when a key does not exist for this tenant.
//
// It is the same error whether the file is absent or belongs to somebody else,
// and that is deliberate: distinguishing them would tell a caller which keys
// exist in other tenants.
var ErrNotFound = errors.New("filesystem: not found")

// ErrNoDisk is returned by [Disks.Disk] for a name nobody registered.
var ErrNoDisk = errors.New("filesystem: no such disk")

// Info is what a file is, without its content.
//
// Stat answers with it, Get carries it inside a [File], and [Send] writes it
// into the response headers. One type, because a size that means one thing in a
// listing and another in a download is how a Content-Length ends up wrong.
type Info struct {
	// Key is the key the caller asked for, never the stored path. A caller that
	// saw the tenant prefix here would start building paths out of it.
	Key         string
	Size        int64
	ContentType string
	ModifiedAt  time.Time
}

// File is what a Get returns: the [Info] plus the bytes.
type File struct {
	Info
	// Body is the content. The caller closes it.
	Body io.ReadCloser
}

// Adapter is what a driver implements.
//
// It takes stored paths, already resolved by [Key], and knows nothing about
// tenants or Grants. That is the point: the isolation is enforced once, in this
// package, instead of in each driver -- and a driver that forgets it cannot,
// because it is never told which tenant it is serving.
//
// Six methods, because they are the six an object store and a directory both
// have. Copy, Move and DeletePrefix are composed out of them by [Disk], so a
// new driver is six methods and not nine.
type Adapter interface {
	// Put writes body at path, creating whatever it needs to.
	Put(ctx context.Context, path string, body io.Reader, contentType string) error
	// Get reads it back. It returns ErrNotFound when the path is not there.
	Get(ctx context.Context, path string) (File, error)
	// Stat answers the same metadata Get carries, without the body.
	Stat(ctx context.Context, path string) (Info, error)
	// Exists reports whether the path is there.
	Exists(ctx context.Context, path string) (bool, error)
	// Delete removes it. Removing what is not there is not an error.
	Delete(ctx context.Context, path string) error
	// List returns the stored paths under a prefix, in no promised order.
	List(ctx context.Context, prefix string) ([]string, error)
}

// Disk is what an application calls.
//
// Every method takes an auth.Grant. That is not ceremony: it is the only thing
// standing between "the application stores files" and "any handler can read any
// customer's files by building a string". The Grant decides the tenant, the
// tenant decides the prefix, and the prefix is not reachable from the key.
//
// Reads are not exempt. List, Get, Stat and Exists take a Grant for the same
// reason Put does -- RULE 17 -- and a listing is the one call where forgetting
// it hands over the names of every file in the system.
type Disk struct {
	name    string
	adapter Adapter
}

// NewDisk names an adapter.
//
// The name is what appears in an error and in `aru` output; it is not part of
// any path, so renaming a disk does not move a file.
func NewDisk(name string, a Adapter) *Disk {
	return &Disk{name: name, adapter: a}
}

// Name returns the name this disk was registered under.
func (d *Disk) Name() string { return d.name }

// Adapter returns the driver underneath.
//
// It exists so [URLSigner] can ask whether the driver is a [Presigner], and so
// a test can assert against the fake it installed. It is not a way around the
// Grant: an Adapter takes stored paths, and the only thing that produces one is
// [Key], which needs a Grant.
func (d *Disk) Adapter() Adapter { return d.adapter }

// Put writes a file under the tenant of the Grant.
//
// An empty contentType is inferred from the key. Inferring from the key and not
// from what an upload announced is deliberate: the client-supplied type is a
// string an attacker chose, and storing it is how an upload becomes stored XSS
// three months later when something serves it back.
func (d *Disk) Put(ctx context.Context, g auth.Grant, key string, body io.Reader, contentType string) error {
	full, err := Key(g, key)
	if err != nil {
		return err
	}
	if contentType == "" {
		contentType = TypeOf(key)
	}
	if err := d.adapter.Put(ctx, full, body, contentType); err != nil {
		return d.wrap("put", key, err)
	}
	return nil
}

// Get reads one back. The caller closes File.Body.
func (d *Disk) Get(ctx context.Context, g auth.Grant, key string) (File, error) {
	full, err := Key(g, key)
	if err != nil {
		return File{}, err
	}
	f, err := d.adapter.Get(ctx, full)
	if err != nil {
		return File{}, d.wrap("get", key, err)
	}
	// The driver answers in stored paths; the caller asked in keys, and gets
	// its own word back.
	f.Key = key
	return f, nil
}

// Stat answers what Get would carry, without moving the bytes.
func (d *Disk) Stat(ctx context.Context, g auth.Grant, key string) (Info, error) {
	full, err := Key(g, key)
	if err != nil {
		return Info{}, err
	}
	i, err := d.adapter.Stat(ctx, full)
	if err != nil {
		return Info{}, d.wrap("stat", key, err)
	}
	i.Key = key
	return i, nil
}

// Exists reports whether the key is there for this tenant.
func (d *Disk) Exists(ctx context.Context, g auth.Grant, key string) (bool, error) {
	full, err := Key(g, key)
	if err != nil {
		return false, err
	}
	ok, err := d.adapter.Exists(ctx, full)
	if err != nil {
		return false, d.wrap("exists", key, err)
	}
	return ok, nil
}

// Delete removes a file. Removing what is not there is not an error.
func (d *Disk) Delete(ctx context.Context, g auth.Grant, key string) error {
	full, err := Key(g, key)
	if err != nil {
		return err
	}
	if err := d.adapter.Delete(ctx, full); err != nil {
		return d.wrap("delete", key, err)
	}
	return nil
}

// List returns the keys under a prefix, without the tenant part, sorted.
//
// The empty prefix means everything this tenant has. Sorted because a listing
// that changes order between two identical calls turns a paginated screen into
// a bug report nobody can reproduce.
func (d *Disk) List(ctx context.Context, g auth.Grant, prefix string) ([]string, error) {
	full, err := prefixPath(g, prefix)
	if err != nil {
		return nil, err
	}
	paths, err := d.adapter.List(ctx, full)
	if err != nil {
		return nil, d.wrap("list", prefix, err)
	}
	tenantPrefix := auth.Tenant(g) + "/"
	keys := make([]string, 0, len(paths))
	for _, p := range paths {
		// A driver that answers with something outside the prefix it was given
		// is a driver with a bug, and passing it on would be this package
		// handing a caller another tenant's key. Dropped, not returned.
		if !strings.HasPrefix(p, tenantPrefix) {
			continue
		}
		keys = append(keys, strings.TrimPrefix(p, tenantPrefix))
	}
	sort.Strings(keys)
	return keys, nil
}

// Copy duplicates a file inside one tenant.
//
// Both keys are resolved against the same Grant, so a copy cannot cross into
// another tenant in either direction -- which is the mistake worth preventing,
// because "copy this into the shared folder" is how it gets asked for.
func (d *Disk) Copy(ctx context.Context, g auth.Grant, src, dst string) error {
	f, err := d.Get(ctx, g, src)
	if err != nil {
		return err
	}
	defer f.Body.Close()
	return d.Put(ctx, g, dst, f.Body, f.ContentType)
}

// Move copies and then deletes the source.
//
// In that order. The other order loses the file when the write fails, and the
// write is the half that fails.
func (d *Disk) Move(ctx context.Context, g auth.Grant, src, dst string) error {
	if src == dst {
		return nil
	}
	if err := d.Copy(ctx, g, src, dst); err != nil {
		return err
	}
	return d.Delete(ctx, g, src)
}

// DeletePrefix removes everything under a prefix, for this tenant only.
//
// It is the one operation whose blast radius is worth stating out loud: an
// empty prefix deletes everything the tenant has. It still cannot reach past
// the tenant, because the prefix is resolved the same way a key is.
func (d *Disk) DeletePrefix(ctx context.Context, g auth.Grant, prefix string) error {
	keys, err := d.List(ctx, g, prefix)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := d.Delete(ctx, g, key); err != nil {
			return err
		}
	}
	return nil
}

// wrap names the disk and the key in an error, and lets ErrNotFound through
// unchanged in the sense that errors.Is still sees it.
func (d *Disk) wrap(op, key string, err error) error {
	return fmt.Errorf("filesystem: %s %s on disk %q: %w", op, key, d.name, err)
}

// TypeOf infers a content type from a key's extension, falling back to a type
// that browsers download rather than render.
//
// The fallback matters: serving an unknown file as text/html is how an upload
// becomes stored XSS.
func TypeOf(key string) string {
	if t := mime.TypeByExtension(path.Ext(key)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// Disks is the set of configured disks, and the one place a name becomes a
// disk.
//
// It is Illuminate's FilesystemManager without the container: disks are
// registered at boot from configuration, and a handler asks for one by name.
// The zero value is not usable; call [NewDisks].
type Disks struct {
	mu          sync.RWMutex
	byName      map[string]*Disk
	defaultDisk string
}

// NewDisks returns an empty set whose default disk will be defaultName.
//
// The default may be registered after this call -- configuration names it
// before it is wired -- and asking for it before it exists is an error, not a
// panic at boot.
func NewDisks(defaultName string) *Disks {
	return &Disks{byName: map[string]*Disk{}, defaultDisk: defaultName}
}

// Add registers an adapter under a name and returns the disk it became.
//
// Registering the same name twice replaces it, because the alternative -- an
// error at boot from a config file that listed a disk twice -- is a crash for a
// mistake with an obvious reading.
func (ds *Disks) Add(name string, a Adapter) *Disk {
	d := NewDisk(name, a)
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.byName[name] = d
	return d
}

// Disk returns a disk by name. The empty name is the default disk.
//
// There is no second accessor for the default. A caller that has no opinion
// passes "", and a caller that does passes the name; one function, so nothing
// has to decide which of two to reach for.
func (ds *Disks) Disk(name string) (*Disk, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	if name == "" {
		name = ds.defaultDisk
	}
	if name == "" {
		return nil, fmt.Errorf("%w: no disk was asked for and none is the default", ErrNoDisk)
	}
	d, ok := ds.byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q. Registered: %s", ErrNoDisk, name, strings.Join(ds.names(), ", "))
	}
	return d, nil
}

// Names returns the registered names, sorted.
func (ds *Disks) Names() []string {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.names()
}

func (ds *Disks) names() []string {
	out := make([]string, 0, len(ds.byName))
	for name := range ds.byName {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
