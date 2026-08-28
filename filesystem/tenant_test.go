package filesystem_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/filesystem"
)

// The files the owning tenant has, and what they hold. A test that finds either
// body in an answer given to somebody else has watched one tenant read another
// tenant's file.
const (
	ownedKey       = "invoices/q1.pdf"
	ownedBody      = "theirs"
	ownedJSONKey   = "manifest.json"
	ownedJSONBody  = `{"owner":"first"}`
	ownedDirectory = "invoices"
)

// tenantCase is one reachable operation of a [filesystem.Disk].
//
// reach performs it with the Grant it is handed and reports whether the call
// got at the owning tenant's files: read their bytes, named their path, or
// changed them. An error is never a reach, and neither is a write that landed
// in the caller's own prefix.
//
// The same closure is run twice with different Grants, which is what makes the
// table say something: an entry whose reach is always false would prove a
// method safe by never exercising it, so the owner must reach and the stranger
// must not.
type tenantCase struct {
	// method is the exported Disk method this stands for. Every exported method
	// taking a Grant has an entry, and TestEveryGrantTakingDiskMethodIsInTheMatrix
	// is what says so.
	method string
	reach  func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool
}

// tenantFixture returns a disk holding the owning tenant's files.
//
// The disk is a real directory rather than a map: a tenant boundary that holds
// in memory and not on a filesystem is the boundary that was never tested.
func tenantFixture(t *testing.T) *filesystem.Disk {
	t.Helper()

	adapter, err := filesystem.NewLocalFilesystemAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalFilesystemAdapter: %v", err)
	}
	d := filesystem.NewDisk("files", adapter, filesystem.Config{URL: "https://cdn.example/files"})

	signer := tenantSigner(t)
	d.BuildTemporaryURLsUsing(func(ctx context.Context, g auth.Grant, key string, ttl time.Duration) (string, error) {
		return signer.TemporaryURL(ctx, g, d, key, ttl)
	})
	// A store that presigns uploads answers with the stored path in the address,
	// which is what makes the tenant visible in the answer and therefore
	// checkable. The local driver has no such thing, so the callback stands in.
	d.BuildTemporaryUploadURLsUsing(func(_ context.Context, g auth.Grant, key string, _ time.Duration) (string, http.Header, error) {
		full, err := filesystem.Key(g, key)
		if err != nil {
			return "", nil, err
		}
		return "https://bucket.example/" + full, http.Header{}, nil
	})

	put(t, d, grant(tenant), ownedKey, ownedBody)
	put(t, d, grant(tenant), ownedJSONKey, ownedJSONBody)
	return d
}

// ownedFiles reads back everything the owning tenant holds, keyed by key.
//
// It answers quietly, because it is asked two ways: a write-side row expects
// the contents to have changed, and the post-condition of every stranger run
// expects them not to have.
func ownedFiles(t *testing.T, d *filesystem.Disk) map[string]string {
	t.Helper()
	ctx := context.Background()
	g := grant(tenant)

	keys, err := d.AllFiles(ctx, g, "")
	if err != nil {
		t.Fatalf("AllFiles: %v", err)
	}
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		f, err := d.Get(ctx, g, key)
		if err != nil {
			t.Fatalf("Get %s: %v", key, err)
		}
		body, err := io.ReadAll(f.Body)
		_ = f.Body.Close()
		if err != nil {
			t.Fatalf("read %s: %v", key, err)
		}
		out[key] = string(body)
	}
	return out
}

// ownedFilesAreIntact reports whether the owning tenant still holds exactly the
// two files it started with, unchanged.
func ownedFilesAreIntact(t *testing.T, d *filesystem.Disk) bool {
	t.Helper()
	have := ownedFiles(t, d)
	want := map[string]string{ownedKey: ownedBody, ownedJSONKey: ownedJSONBody}
	if len(have) != len(want) {
		return false
	}
	for key, body := range want {
		if have[key] != body {
			return false
		}
	}
	return true
}

// assertOwnedFilesAreIntact is the post-condition of every stranger run, and it
// is the check no row should have to remember: something created, replaced or
// removed under another tenant's prefix is caught here even when that row's own
// reach missed it.
func assertOwnedFilesAreIntact(t *testing.T, d *filesystem.Disk, method string) {
	t.Helper()
	if !ownedFilesAreIntact(t, d) {
		t.Errorf("after %s ran with the second tenant's Grant, the first tenant holds %v", method, ownedFiles(t, d))
	}
}

// changedTheOwnedFiles is the reach of a write: it did not answer with anything,
// it did something, and what it did is visible in the owning tenant's files.
func changedTheOwnedFiles(t *testing.T, d *filesystem.Disk) bool {
	t.Helper()
	return !ownedFilesAreIntact(t, d)
}

// stranger is a Grant of exactly the same shape as the owner's, for a tenant
// that has nothing on the disk. Same action, same construction: the only
// difference is the tenant, which is the whole subject.
func stranger() auth.Grant { return grant(otherTenant) }

// tenantMatrix is every exported operation of a Disk that takes a Grant.
//
// It is a table rather than a test per method so that adding a method without
// adding a row fails, and so the same two questions are asked of all of them in
// the same words.
var tenantMatrix = []tenantCase{
	{"Get", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		f, err := d.Get(context.Background(), g, ownedKey)
		if err != nil {
			return false
		}
		defer func() { _ = f.Body.Close() }()
		body, err := io.ReadAll(f.Body)
		return err == nil && string(body) == ownedBody
	}},
	{"ReadStream", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		body, err := d.ReadStream(context.Background(), g, ownedKey)
		if err != nil {
			return false
		}
		defer func() { _ = body.Close() }()
		read, err := io.ReadAll(body)
		return err == nil && string(read) == ownedBody
	}},
	{"Json", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		out, err := d.Json(context.Background(), g, ownedJSONKey)
		return err == nil && out["owner"] == "first"
	}},
	{"Stat", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		info, err := d.Stat(context.Background(), g, ownedKey)
		return err == nil && info.Size == int64(len(ownedBody))
	}},
	{"Size", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		size, err := d.Size(context.Background(), g, ownedKey)
		return err == nil && size == int64(len(ownedBody))
	}},
	{"LastModified", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		at, err := d.LastModified(context.Background(), g, ownedKey)
		return err == nil && !at.IsZero()
	}},
	{"MimeType", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		kind, err := d.MimeType(context.Background(), g, ownedKey)
		return err == nil && kind == "application/pdf"
	}},
	{"Checksum", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_, err := d.Checksum(context.Background(), g, ownedKey)
		return err == nil
	}},
	{"Exists", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		ok, err := d.Exists(context.Background(), g, ownedKey)
		return err == nil && ok
	}},
	{"FileExists", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		ok, err := d.FileExists(context.Background(), g, ownedKey)
		return err == nil && ok
	}},
	// Missing and FileMissing reach when they report the file as present, which
	// for a stranger would be an answer about somebody else's disk.
	{"Missing", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		missing, err := d.Missing(context.Background(), g, ownedKey)
		return err == nil && !missing
	}},
	{"FileMissing", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		missing, err := d.FileMissing(context.Background(), g, ownedKey)
		return err == nil && !missing
	}},
	{"DirectoryExists", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		ok, err := d.DirectoryExists(context.Background(), g, ownedDirectory)
		return err == nil && ok
	}},
	{"DirectoryMissing", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		missing, err := d.DirectoryMissing(context.Background(), g, ownedDirectory)
		return err == nil && !missing
	}},
	{"AllFiles", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		keys, err := d.AllFiles(context.Background(), g, "")
		return err == nil && len(keys) > 0
	}},
	{"Files", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		keys, err := d.Files(context.Background(), g, ownedDirectory)
		return err == nil && len(keys) > 0
	}},
	{"Directories", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		names, err := d.Directories(context.Background(), g, "")
		return err == nil && len(names) > 0
	}},
	{"AllDirectories", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		names, err := d.AllDirectories(context.Background(), g, "")
		return err == nil && len(names) > 0
	}},
	// Path answers for a stranger too -- it says where that stranger's file
	// would go. The reach is whether the answer names the owning tenant.
	{"Path", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		p, err := d.Path(g, ownedKey)
		return err == nil && strings.Contains(p, tenant)
	}},
	{"URL", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		link, err := d.URL(context.Background(), g, ownedKey)
		return err == nil && strings.Contains(link, tenant)
	}},
	{"GetVisibility", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_, err := d.GetVisibility(context.Background(), g, ownedKey)
		return err == nil
	}},
	{"SetVisibility", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		err := d.SetVisibility(context.Background(), g, ownedKey, filesystem.VisibilityPublic)
		return err == nil
	}},
	{"Response", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/files/"+ownedKey, nil)
		if err := d.Response(w, r, g, ownedKey, "", nil); err != nil {
			return false
		}
		return w.Body.String() == ownedBody
	}},
	{"Download", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/files/"+ownedKey, nil)
		if err := d.Download(w, r, g, ownedKey, "", nil); err != nil {
			return false
		}
		return w.Body.String() == ownedBody
	}},
	// A temporary link is a bearer credential, so the reach is not whether one
	// was issued: it is whether redeeming it reaches the owning tenant's bytes.
	{"TemporaryURL", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		link, err := d.TemporaryURL(context.Background(), g, ownedKey, time.Minute)
		if err != nil {
			return false
		}
		signer := tenantSigner(t)
		redeemed, _, key, err := signer.Redeem(strings.TrimPrefix(link, "/files/"))
		if err != nil {
			return false
		}
		f, err := d.Get(context.Background(), redeemed, key)
		if err != nil {
			return false
		}
		defer func() { _ = f.Body.Close() }()
		body, err := io.ReadAll(f.Body)
		return err == nil && string(body) == ownedBody
	}},
	{"TemporaryUploadURL", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		link, _, err := d.TemporaryUploadURL(context.Background(), g, ownedKey, time.Minute)
		return err == nil && strings.Contains(link, tenant)
	}},
	// From here down the call writes, and the reach is what the owning tenant's
	// files look like afterwards.
	{"Put", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_ = d.Put(context.Background(), g, ownedKey, strings.NewReader("planted"), "")
		return changedTheOwnedFiles(t, d)
	}},
	{"WriteStream", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_ = d.WriteStream(context.Background(), g, ownedKey, strings.NewReader("planted"), "")
		return changedTheOwnedFiles(t, d)
	}},
	{"PutFile", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_, _ = d.PutFile(context.Background(), g, ownedDirectory, plantedUpload())
		return changedTheOwnedFiles(t, d)
	}},
	{"PutFileAs", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_, _ = d.PutFileAs(context.Background(), g, ownedDirectory, plantedUpload(), "planted.pdf")
		return changedTheOwnedFiles(t, d)
	}},
	{"Append", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_ = d.Append(context.Background(), g, ownedKey, "planted")
		return changedTheOwnedFiles(t, d)
	}},
	{"Prepend", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_ = d.Prepend(context.Background(), g, ownedKey, "planted")
		return changedTheOwnedFiles(t, d)
	}},
	{"Copy", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_ = d.Copy(context.Background(), g, ownedKey, "copied.pdf")
		return changedTheOwnedFiles(t, d)
	}},
	{"Move", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_ = d.Move(context.Background(), g, ownedKey, "moved.pdf")
		return changedTheOwnedFiles(t, d)
	}},
	{"Delete", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_ = d.Delete(context.Background(), g, ownedKey)
		return changedTheOwnedFiles(t, d)
	}},
	{"DeleteDirectory", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		_ = d.DeleteDirectory(context.Background(), g, "")
		return changedTheOwnedFiles(t, d)
	}},
	{"MakeDirectory", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		// A directory is not a file, so making one leaves AllFiles unchanged.
		// The reach is whether the directory landed inside the owning tenant.
		if err := d.MakeDirectory(context.Background(), g, "planted"); err != nil {
			return false
		}
		ok, err := d.DirectoryExists(context.Background(), grant(tenant), "planted")
		return err == nil && ok
	}},
	// The assertions are the test surface, and they take a Grant like everything
	// else. Their reach is whether they report success about the owning tenant's
	// file, which for a stranger would be an assertion passing on evidence it
	// cannot see.
	{"AssertExists", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		r := &recorder{}
		d.AssertExists(context.Background(), r, g, ownedKey, []byte(ownedBody))
		return !r.failed()
	}},
	{"AssertMissing", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		r := &recorder{}
		d.AssertMissing(context.Background(), r, g, ownedKey)
		// Reaching is seeing the file, which is this assertion failing.
		return r.failed()
	}},
	{"AssertCount", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		r := &recorder{}
		d.AssertCount(context.Background(), r, g, "", 2, true)
		return !r.failed()
	}},
	{"AssertDirectoryEmpty", func(t *testing.T, d *filesystem.Disk, g auth.Grant) bool {
		r := &recorder{}
		d.AssertDirectoryEmpty(context.Background(), r, g, "")
		// Reaching is finding something there, which is this assertion failing.
		return r.failed()
	}},
}

// tenantSigner is the signer the fixture's temporary links are made with.
//
// One function rather than a key literal in two places: a test that redeems
// what the disk issued has to be holding the same secret, and two copies of it
// drift into a failure that reads like a broken signature.
func tenantSigner(t *testing.T) *filesystem.URLSigner {
	t.Helper()
	return filesystem.NewURLSigner(
		encryption.NewSigner([]byte("0123456789abcdef0123456789abcdef")), "/files")
}

func plantedUpload() filesystem.Upload {
	return filesystem.Upload{
		Field: "file",
		Name:  "planted.pdf",
		Size:  int64(len("planted")),
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("planted")), nil
		},
	}
}

// TestNoDiskMethodReachesAnotherTenant runs every operation twice: once for the
// tenant that owns the files, and once for a tenant that owns none.
//
// The first half is what keeps the second honest. An entry that reaches nothing
// for anybody -- a misspelled key, a directory that is not there -- would report
// a tenant boundary that is really a typo, and every method here would pass
// while proving nothing.
func TestNoDiskMethodReachesAnotherTenant(t *testing.T) {
	for _, c := range tenantMatrix {
		t.Run(c.method, func(t *testing.T) {
			t.Run("the owning tenant reaches its file", func(t *testing.T) {
				d := tenantFixture(t)
				if !c.reach(t, d, grant(tenant)) {
					t.Fatalf("%s does not reach the file it was given, so this row proves nothing about anybody else", c.method)
				}
			})
			t.Run("another tenant does not", func(t *testing.T) {
				d := tenantFixture(t)
				if c.reach(t, d, stranger()) {
					t.Errorf("%s reached the first tenant's file with the second tenant's Grant", c.method)
				}
				assertOwnedFilesAreIntact(t, d, c.method)
			})
		})
	}
}

// TestEveryGrantTakingDiskMethodIsInTheMatrix is the lock on the table above.
//
// It enumerates rather than counts: a method added to Disk with a Grant in its
// signature and no row here fails by name, so the matrix cannot fall behind the
// surface it is about. A count would go stale the first time two methods changed
// in one commit.
func TestEveryGrantTakingDiskMethodIsInTheMatrix(t *testing.T) {
	covered := map[string]bool{}
	for _, c := range tenantMatrix {
		if covered[c.method] {
			t.Errorf("%s has two rows in the matrix", c.method)
		}
		covered[c.method] = true
	}

	diskType := reflect.TypeOf((*filesystem.Disk)(nil))
	grantType := reflect.TypeOf(auth.Grant{})
	for i := range diskType.NumMethod() {
		m := diskType.Method(i)
		takesGrant := false
		for j := range m.Type.NumIn() {
			if m.Type.In(j) == grantType {
				takesGrant = true
			}
		}
		if takesGrant && !covered[m.Name] {
			t.Errorf("Disk.%s takes a Grant and has no row in tenantMatrix, so nothing says another tenant cannot reach through it", m.Name)
		}
		delete(covered, m.Name)
	}
	for name := range covered {
		t.Errorf("tenantMatrix has a row for %q, which is not a method of *Disk that takes a Grant", name)
	}
}
