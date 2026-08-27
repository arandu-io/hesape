package filesystem_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/filesystem"
)

const otherTenant = "22222222-2222-4222-8222-222222222222"

// localDisk returns a Disk over a real directory, which is what an application
// holds. The adapter is reached through it on purpose: a test that called the
// adapter directly would be testing a path the application cannot build.
func localDisk(t *testing.T) *filesystem.Disk {
	t.Helper()
	return filesystem.NewDisk("local", localAdapter(t))
}

func localAdapter(t *testing.T) *filesystem.LocalFilesystemAdapter {
	t.Helper()
	a, err := filesystem.NewLocalFilesystemAdapter(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatalf("NewLocalFilesystemAdapter: %v", err)
	}
	return a
}

func TestLocalRoundTrip(t *testing.T) {
	d := localDisk(t)
	ctx := context.Background()

	put(t, d, grant(tenant), "invoices/2026-08.pdf", "the invoice")

	f, err := d.Get(ctx, grant(tenant), "invoices/2026-08.pdf")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = f.Body.Close() }()

	body, err := io.ReadAll(f.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "the invoice" {
		t.Fatalf("read %q", body)
	}
	if f.Key != "invoices/2026-08.pdf" {
		t.Errorf("Key = %q, want the key the caller asked for", f.Key)
	}
	if f.Size != int64(len("the invoice")) {
		t.Errorf("size = %d", f.Size)
	}
	if f.ContentType != "application/pdf" {
		t.Errorf("content type = %q", f.ContentType)
	}
}

// TestLocalOneTenantCannotReadAnother is the reason every Disk method takes a
// Grant, checked against a real directory rather than a map.
func TestLocalOneTenantCannotReadAnother(t *testing.T) {
	d := localDisk(t)
	ctx := context.Background()

	put(t, d, grant(tenant), "secret.pdf", "theirs")

	if _, err := d.Get(ctx, grant(otherTenant), "secret.pdf"); !errors.Is(err, filesystem.ErrNotFound) {
		t.Fatalf("another tenant read the file: %v", err)
	}
	if exists, err := d.Exists(ctx, grant(otherTenant), "secret.pdf"); err != nil || exists {
		t.Fatalf("another tenant sees the file: %v (%v)", exists, err)
	}
	// And its own delete does not touch the other's file.
	if err := d.Delete(ctx, grant(otherTenant), "secret.pdf"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if exists, err := d.Exists(ctx, grant(tenant), "secret.pdf"); err != nil || !exists {
		t.Fatal("one tenant's delete removed another tenant's file")
	}
}

// TestLocalAPathCannotEscapeTheRoot is the second of the two checks, and the
// only one this file is responsible for: CleanKey rejects "../" in a key, and
// this rejects a resolved path outside the root.
//
// It goes at the adapter directly, because that is where a hand-built path would
// arrive. An Adapter is a public interface, so "the Disk would never send this"
// is not the same as "this cannot happen".
func TestLocalAPathCannotEscapeTheRoot(t *testing.T) {
	a := localAdapter(t)
	ctx := context.Background()

	for _, p := range []string{
		"../escaped.txt",
		"../../etc/passwd",
		tenant + "/../../escaped.txt",
		"..",
		"",
		"/",
	} {
		if err := a.Put(ctx, p, strings.NewReader("x"), ""); !errors.Is(err, filesystem.ErrBadKey) {
			t.Errorf("Put(%q) = %v, want ErrBadKey", p, err)
		}
		if _, err := a.Get(ctx, p); !errors.Is(err, filesystem.ErrBadKey) {
			t.Errorf("Get(%q) = %v, want ErrBadKey", p, err)
		}
		if _, err := a.Stat(ctx, p); !errors.Is(err, filesystem.ErrBadKey) {
			t.Errorf("Stat(%q) = %v, want ErrBadKey", p, err)
		}
		if _, err := a.Exists(ctx, p); !errors.Is(err, filesystem.ErrBadKey) {
			t.Errorf("Exists(%q) = %v, want ErrBadKey", p, err)
		}
		if err := a.Delete(ctx, p); !errors.Is(err, filesystem.ErrBadKey) {
			t.Errorf("Delete(%q) = %v, want ErrBadKey", p, err)
		}
	}

	// A NUL byte truncates a path in every syscall that takes one, so a path
	// carrying it can name a different file than it appears to.
	if err := a.Put(ctx, "a\x00.txt", strings.NewReader("x"), ""); !errors.Is(err, filesystem.ErrBadKey) {
		t.Errorf("a NUL byte was accepted: %v", err)
	}

	// Nothing landed next to the root either.
	if entries, err := os.ReadDir(filepath.Dir(a.Root())); err == nil {
		for _, e := range entries {
			if e.Name() != filepath.Base(a.Root()) {
				t.Errorf("something was written outside the root: %s", e.Name())
			}
		}
	}
}

// TestLocalAListingCannotEscapeTheRoot: List walks, and a walk that starts
// outside the root reads a directory that is not this application's.
func TestLocalAListingCannotEscapeTheRoot(t *testing.T) {
	a := localAdapter(t)

	for _, prefix := range []string{"../", "../../", tenant + "/../../"} {
		if _, err := a.List(context.Background(), prefix); !errors.Is(err, filesystem.ErrBadKey) {
			t.Errorf("List(%q) = %v, want ErrBadKey", prefix, err)
		}
	}
}

func TestLocalAGrantWithoutATenantIsRefused(t *testing.T) {
	d := localDisk(t)

	err := d.Put(context.Background(), auth.Grant{}, "x.pdf", strings.NewReader("x"), "")
	if !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}

// TestLocalListStaysWithinTheTenant: the tenant is stripped on the way out for
// the same reason it is added on the way in -- a caller that saw it could start
// passing it.
func TestLocalListStaysWithinTheTenant(t *testing.T) {
	d := localDisk(t)
	ctx := context.Background()

	put(t, d, grant(tenant), "invoices/a.pdf", "a")
	put(t, d, grant(tenant), "invoices/b.pdf", "b")
	put(t, d, grant(tenant), "avatars/c.png", "c")
	put(t, d, grant(otherTenant), "invoices/theirs.pdf", "theirs")

	all, err := d.AllFiles(ctx, grant(tenant), "")
	if err != nil {
		t.Fatalf("AllFiles: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("listed %v, want three of its own", all)
	}
	for _, key := range all {
		if strings.Contains(key, tenant) || strings.Contains(key, otherTenant) {
			t.Errorf("the key leaks the tenant: %q", key)
		}
		if strings.Contains(key, "theirs") {
			t.Errorf("another tenant's file was listed: %q", key)
		}
	}

	invoices, err := d.AllFiles(ctx, grant(tenant), "invoices/")
	if err != nil {
		t.Fatalf("AllFiles: %v", err)
	}
	if len(invoices) != 2 {
		t.Fatalf("the directory returned %v", invoices)
	}
}

// TestLocalListingATenantWithNothingIsEmpty: a tenant that never stored anything
// has no directory, and that is an empty list rather than an error.
func TestLocalListingATenantWithNothingIsEmpty(t *testing.T) {
	d := localDisk(t)

	keys, err := d.AllFiles(context.Background(), grant(tenant), "")
	if err != nil {
		t.Fatalf("AllFiles: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("listed %v", keys)
	}
}

// TestLocalAPartialWriteIsNotVisible: written to a temporary name and renamed,
// so a reader never sees half a file and a crash mid-upload leaves nothing
// behind.
func TestLocalAPartialWriteIsNotVisible(t *testing.T) {
	d := localDisk(t)
	ctx := context.Background()
	g := grant(tenant)

	if err := d.Put(ctx, g, "broken.pdf", failingReader{}, ""); err == nil {
		t.Fatal("a failing upload reported success")
	}
	if exists, _ := d.Exists(ctx, g, "broken.pdf"); exists {
		t.Fatal("a failed upload left the file in place")
	}
	keys, _ := d.AllFiles(ctx, g, "")
	if len(keys) != 0 {
		t.Errorf("a partial file was left behind: %v", keys)
	}
}

// TestLocalAnUnknownTypeIsNotHTML: serving an unknown file as text/html is how
// an upload becomes stored XSS.
func TestLocalAnUnknownTypeIsNotHTML(t *testing.T) {
	d := localDisk(t)
	ctx := context.Background()

	put(t, d, grant(tenant), "payload.weird", "<script>alert(1)</script>")

	f, err := d.Get(ctx, grant(tenant), "payload.weird")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Body.Close() }()

	if strings.Contains(f.ContentType, "html") {
		t.Fatalf("content type = %q, and the browser would render it", f.ContentType)
	}
}

func TestLocalDeletingWhatIsNotThereIsFine(t *testing.T) {
	d := localDisk(t)

	if err := d.Delete(context.Background(), grant(tenant), "never-existed.pdf"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

// TestLocalADirectoryIsNotAFile: a Stat that answered for one would let a
// caller download the name of a folder.
func TestLocalADirectoryIsNotAFile(t *testing.T) {
	d := localDisk(t)
	ctx := context.Background()
	g := grant(tenant)

	put(t, d, g, "invoices/a.pdf", "a")

	if _, err := d.Stat(ctx, g, "invoices"); !errors.Is(err, filesystem.ErrNotFound) {
		t.Errorf("Stat of a directory = %v, want ErrNotFound", err)
	}
	if _, err := d.Get(ctx, g, "invoices"); !errors.Is(err, filesystem.ErrNotFound) {
		t.Errorf("Get of a directory = %v, want ErrNotFound", err)
	}
	if ok, err := d.Exists(ctx, g, "invoices"); err != nil || ok {
		t.Errorf("Exists of a directory = %v, %v", ok, err)
	}
}

// TestLocalTheRootIsCreated: the alternative is an application that boots fine
// and fails on the first upload.
func TestLocalTheRootIsCreated(t *testing.T) {
	root := filepath.Join(t.TempDir(), "a", "b", "files")

	if _, err := filesystem.NewLocalFilesystemAdapter(root); err != nil {
		t.Fatalf("NewLocalFilesystemAdapter: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("the root was not created: %v", err)
	}
	if _, err := filesystem.NewLocalFilesystemAdapter(""); err == nil {
		t.Error("an adapter with no root was accepted")
	}
}

// failingReader fails halfway, like a connection dropping mid-upload.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("the connection dropped") }

// TestLocalConcurrentUploadsOfTheSameKeyDoNotMix is a bug an audit found.
//
// Every upload wrote to the key plus ".partial", which is the same file for
// every concurrent upload of the same key. Two of them opened it with O_TRUNC,
// interleaved their bytes, and both renamed the result into place -- so the
// stored object was neither upload. Two people replacing the same attachment is
// not a rare race; it is what a retry looks like.
//
// The bodies here are distinguishable by content, so a mixed file fails the
// check rather than passing by luck.
func TestLocalConcurrentUploadsOfTheSameKeyDoNotMix(t *testing.T) {
	d := localDisk(t)
	g := grant(tenant)
	ctx := context.Background()

	const writers = 8
	const size = 64 * 1024 // big enough that one io.Copy is several writes

	bodies := make([][]byte, writers)
	for i := range bodies {
		bodies[i] = bytes.Repeat([]byte{byte('a' + i)}, size)
	}

	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := d.Put(ctx, g, "invoice.pdf", bytes.NewReader(bodies[i]), "application/pdf"); err != nil {
				t.Errorf("Put %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	f, err := d.Get(ctx, g, "invoice.pdf")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = f.Body.Close() }()

	got, err := io.ReadAll(f.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != size {
		t.Fatalf("the stored file is %d bytes, want %d -- two uploads were written into one", len(got), size)
	}
	for _, want := range bodies {
		if bytes.Equal(got, want) {
			return
		}
	}
	t.Fatalf("the stored file matches no upload: it starts with %q and ends with %q",
		got[:8], got[len(got)-8:])
}

// TestLocalAnUploadInFlightIsNotListed: a partial file is not a stored object,
// and a caller listing keys during an upload must not see one.
func TestLocalAnUploadInFlightIsNotListed(t *testing.T) {
	d := localDisk(t)
	g := grant(tenant)
	ctx := context.Background()

	put(t, d, g, "reports/q1.pdf", "done")

	keys, err := d.AllFiles(ctx, g, "")
	if err != nil {
		t.Fatalf("AllFiles: %v", err)
	}
	if len(keys) != 1 || keys[0] != "reports/q1.pdf" {
		t.Fatalf("AllFiles = %v, want exactly the stored key", keys)
	}
}

// TestLocalTheWholeDiskSurfaceAnswers is the merge check: every method the
// package grew is composed out of the six an Adapter has, so a driver that
// implements the six answers all of them. Against a real directory, because a
// map cannot tell whether a listing came back sorted by accident.
func TestLocalTheWholeDiskSurfaceAnswers(t *testing.T) {
	d := localDisk(t)
	ctx := context.Background()
	g := grant(tenant)

	put(t, d, g, "invoices/2026/a.pdf", "a")
	put(t, d, g, "invoices/b.pdf", "bb")
	put(t, d, g, "notes.txt", "note")

	if missing, err := d.Missing(ctx, g, "notes.txt"); err != nil || missing {
		t.Errorf("Missing = %v, %v", missing, err)
	}
	if missing, err := d.Missing(ctx, g, "nope.txt"); err != nil || !missing {
		t.Errorf("Missing of an absent key = %v, %v", missing, err)
	}
	if size, err := d.Size(ctx, g, "invoices/b.pdf"); err != nil || size != 2 {
		t.Errorf("Size = %d, %v", size, err)
	}
	if at, err := d.LastModified(ctx, g, "notes.txt"); err != nil || at.IsZero() {
		t.Errorf("LastModified = %v, %v", at, err)
	}
	if mt, err := d.MimeType(ctx, g, "invoices/b.pdf"); err != nil || mt != "application/pdf" {
		t.Errorf("MimeType = %q, %v", mt, err)
	}
	// The SHA-256 of "note", so a change of algorithm is a failing test and not
	// a silent change of meaning.
	const noteSum = "edb465624291e4053c6c5ea4b7eb320dec773e10a57d26b95dcf0564f8e310f8"
	if sum, err := d.Checksum(ctx, g, "notes.txt"); err != nil || sum != noteSum {
		t.Errorf("Checksum = %q, %v", sum, err)
	}

	if files, err := d.Files(ctx, g, ""); err != nil || !equal(files, []string{"notes.txt"}) {
		t.Errorf("Files(root) = %v, %v", files, err)
	}
	if files, err := d.Files(ctx, g, "invoices"); err != nil || !equal(files, []string{"invoices/b.pdf"}) {
		t.Errorf("Files(invoices) = %v, %v", files, err)
	}
	if all, err := d.AllFiles(ctx, g, "invoices"); err != nil ||
		!equal(all, []string{"invoices/2026/a.pdf", "invoices/b.pdf"}) {
		t.Errorf("AllFiles(invoices) = %v, %v", all, err)
	}
	if dirs, err := d.Directories(ctx, g, ""); err != nil || !equal(dirs, []string{"invoices/"}) {
		t.Errorf("Directories(root) = %v, %v", dirs, err)
	}
	if dirs, err := d.AllDirectories(ctx, g, ""); err != nil ||
		!equal(dirs, []string{"invoices/", "invoices/2026/"}) {
		t.Errorf("AllDirectories(root) = %v, %v", dirs, err)
	}

	if err := d.Append(ctx, g, "notes.txt", "second"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got := read(t, d, g, "notes.txt"); got != "note\nsecond" {
		t.Errorf("after Append = %q", got)
	}
	if err := d.Prepend(ctx, g, "notes.txt", "first"); err != nil {
		t.Fatalf("Prepend: %v", err)
	}
	if got := read(t, d, g, "notes.txt"); got != "first\nnote\nsecond" {
		t.Errorf("after Prepend = %q", got)
	}
	// Appending to what is not there writes it, which is what makes the call
	// usable without a check nobody would remember.
	if err := d.Append(ctx, g, "new.log", "line"); err != nil {
		t.Fatalf("Append to an absent key: %v", err)
	}
	if got := read(t, d, g, "new.log"); got != "line" {
		t.Errorf("Append to an absent key wrote %q", got)
	}

	if err := d.DeleteDirectory(ctx, g, "invoices"); err != nil {
		t.Fatalf("DeleteDirectory: %v", err)
	}
	if left, err := d.AllFiles(ctx, g, "invoices"); err != nil || len(left) != 0 {
		t.Errorf("DeleteDirectory left %v (%v)", left, err)
	}
	if ok, err := d.Exists(ctx, g, "notes.txt"); err != nil || !ok {
		t.Errorf("DeleteDirectory reached outside the directory: %v, %v", ok, err)
	}
}

// TestLocalAppendAndPrependStayInTheTenant: the two calls that read before they
// write are the two where a forgotten Grant would be hardest to see.
func TestLocalAppendAndPrependStayInTheTenant(t *testing.T) {
	d := localDisk(t)
	ctx := context.Background()

	put(t, d, grant(tenant), "log.txt", "mine")

	if err := d.Append(ctx, grant(otherTenant), "log.txt", "theirs"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got := read(t, d, grant(tenant), "log.txt"); got != "mine" {
		t.Fatalf("another tenant's Append changed the file: %q", got)
	}
	if got := read(t, d, grant(otherTenant), "log.txt"); got != "theirs" {
		t.Fatalf("the other tenant's own file = %q", got)
	}
}

// TestLocalPutFileNamesTheFileItself: the announced filename is a string the
// client chose, and storing under it means two people uploading "scan.pdf"
// overwrite each other.
func TestLocalPutFileNamesTheFileItself(t *testing.T) {
	d := localDisk(t)
	ctx := context.Background()
	g := grant(tenant)

	u := filesystem.Upload{
		Field:       "avatar",
		Name:        "scan.PDF",
		Size:        3,
		ContentType: "text/html",
		Open:        func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("abc")), nil },
	}

	first, err := d.PutFile(ctx, g, "avatars", u)
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	second, err := d.PutFile(ctx, g, "avatars", u)
	if err != nil {
		t.Fatalf("PutFile: %v", err)
	}
	if first == second {
		t.Fatal("two uploads landed on the same key")
	}
	for _, key := range []string{first, second} {
		if !strings.HasPrefix(key, "avatars/") || !strings.HasSuffix(key, ".pdf") {
			t.Errorf("key = %q, want it under the directory and keeping the extension", key)
		}
		if strings.Contains(key, "scan") {
			t.Errorf("key = %q, and the client named it", key)
		}
	}
	// The announced type is recorded and never trusted: the stored one comes
	// from the key.
	if mt, err := d.MimeType(ctx, g, first); err != nil || mt != "application/pdf" {
		t.Errorf("MimeType = %q, %v", mt, err)
	}

	named, err := d.PutFileAs(ctx, g, "avatars", u, "me.pdf")
	if err != nil {
		t.Fatalf("PutFileAs: %v", err)
	}
	if named != "avatars/me.pdf" {
		t.Errorf("PutFileAs = %q", named)
	}
	if got := read(t, d, g, named); got != "abc" {
		t.Errorf("body = %q", got)
	}

	// A name is a name. One with a separator is refused rather than cleaned.
	for _, bad := range []string{"", ".", "..", "../escape.pdf", "sub/dir.pdf", `back\slash.pdf`} {
		if _, err := d.PutFileAs(ctx, g, "avatars", u, bad); !errors.Is(err, filesystem.ErrBadKey) {
			t.Errorf("PutFileAs(%q) = %v, want ErrBadKey", bad, err)
		}
	}
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The bodies of the files beside the root. A test that finds either of them in
// an answer has watched the adapter read a file that is not in its store.
const (
	outsideSecret = "outside secret"
	outsideNested = "outside nested"
)

// symlinkFixture builds a root with a directory beside it, and returns both
// plus an adapter rooted at the first.
//
// They are siblings rather than parent and child so that no key can name the
// second one: reaching it takes a link, which is the whole subject.
func symlinkFixture(t *testing.T) (root, outside string, a *filesystem.LocalFilesystemAdapter) {
	t.Helper()

	base := t.TempDir()
	root = filepath.Join(base, "files")
	outside = filepath.Join(base, "outside")

	if err := os.MkdirAll(filepath.Join(outside, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"secret.txt":     outsideSecret,
		"sub/nested.txt": outsideNested,
	} {
		if err := os.WriteFile(filepath.Join(outside, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	a, err := filesystem.NewLocalFilesystemAdapter(root)
	if err != nil {
		t.Fatalf("NewLocalFilesystemAdapter: %v", err)
	}
	return root, outside, a
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// escapeRoutes are the shapes a link inside the root takes to point out of it.
//
// They are separate cases because they are followed at different moments: a
// link as the last component is followed by a read, a link as a directory
// component is followed by every operation including the write, and a chain and
// a relative target are what a check that reads one link at a time misses. None
// of the keys contains "..", which is what a check on the key alone reads.
var escapeRoutes = []struct {
	name string
	// plant creates the links under root. Nothing under outside may be
	// reachable afterwards.
	plant func(t *testing.T, root, outside string)
	// key is the stored path that walks them, and body what the file it lands
	// on holds -- so a test can say which file came back rather than that one
	// did.
	key  string
	body string
}{
	{
		name: "a link as a directory component",
		plant: func(t *testing.T, root, outside string) {
			symlink(t, outside, filepath.Join(root, "out"))
		},
		key:  "out/secret.txt",
		body: outsideSecret,
	},
	{
		name: "a link as the last component",
		plant: func(t *testing.T, root, outside string) {
			symlink(t, filepath.Join(outside, "secret.txt"), filepath.Join(root, "secret.txt"))
		},
		key:  "secret.txt",
		body: outsideSecret,
	},
	{
		name: "a chain of links",
		plant: func(t *testing.T, root, outside string) {
			symlink(t, "second", filepath.Join(root, "first"))
			symlink(t, outside, filepath.Join(root, "second"))
		},
		key:  "first/secret.txt",
		body: outsideSecret,
	},
	{
		name: "a relative link that climbs out",
		plant: func(t *testing.T, root, outside string) {
			target, err := filepath.Rel(filepath.Join(root, tenant), outside)
			if err != nil {
				t.Fatal(err)
			}
			symlink(t, target, filepath.Join(root, tenant, "up"))
		},
		key:  tenant + "/up/secret.txt",
		body: outsideSecret,
	},
	{
		name: "a link under a deeper prefix",
		plant: func(t *testing.T, root, outside string) {
			symlink(t, outside, filepath.Join(root, "out"))
		},
		key:  "out/sub/nested.txt",
		body: outsideNested,
	},
}

// TestLocalASymlinkCannotBeReadThrough: the key names nothing that escapes, and
// the path it resolves to does. A check that compares strings answers about the
// key; the kernel answers about the link.
func TestLocalASymlinkCannotBeReadThrough(t *testing.T) {
	for _, route := range escapeRoutes {
		t.Run(route.name, func(t *testing.T) {
			root, outside, a := symlinkFixture(t)
			route.plant(t, root, outside)
			ctx := context.Background()

			if f, err := a.Get(ctx, route.key); err == nil {
				body, _ := io.ReadAll(f.Body)
				_ = f.Body.Close()
				t.Errorf("Get(%q) read %q from outside the root", route.key, body)
			}
			if info, err := a.Stat(ctx, route.key); err == nil {
				t.Errorf("Stat(%q) answered %d bytes about a file outside the root", route.key, info.Size)
			}
			if ok, err := a.Exists(ctx, route.key); err == nil && ok {
				t.Errorf("Exists(%q) = true for a file outside the root", route.key)
			}
		})
	}
}

// TestLocalASymlinkCannotBeWrittenThrough is the half that is not a leak but a
// write: the application's own credentials putting attacker-chosen bytes at an
// attacker-chosen path.
func TestLocalASymlinkCannotBeWrittenThrough(t *testing.T) {
	for _, route := range escapeRoutes {
		t.Run(route.name, func(t *testing.T) {
			root, outside, a := symlinkFixture(t)
			route.plant(t, root, outside)
			ctx := context.Background()

			// The route's own key replaces a file outside the root; the name
			// beside it creates one.
			for _, key := range []string{route.key, path.Join(path.Dir(route.key), "planted.txt")} {
				_ = a.Put(ctx, key, strings.NewReader("planted"), "")
			}
			assertOutsideIsUntouched(t, outside)
		})
	}
}

// TestLocalASymlinkCannotBeDeletedThrough: Delete removes the link when the
// link is the last component, and the file the link points at when it is not.
func TestLocalASymlinkCannotBeDeletedThrough(t *testing.T) {
	for _, route := range escapeRoutes {
		t.Run(route.name, func(t *testing.T) {
			root, outside, a := symlinkFixture(t)
			route.plant(t, root, outside)

			_ = a.Delete(context.Background(), route.key)
			assertOutsideIsUntouched(t, outside)
		})
	}
}

// TestLocalASymlinkCannotBeListedThrough: a listing is the one answer that
// names files the caller had not asked for by name, so a walk that follows a
// link hands back the contents of a directory nobody has a key for.
func TestLocalASymlinkCannotBeListedThrough(t *testing.T) {
	for _, route := range escapeRoutes {
		t.Run(route.name, func(t *testing.T) {
			root, outside, a := symlinkFixture(t)
			route.plant(t, root, outside)

			realRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, prefix := range []string{"", path.Dir(route.key) + "/", route.key} {
				keys, err := a.List(context.Background(), prefix)
				if err != nil {
					continue
				}
				for _, key := range keys {
					// Where the key lands, not what it says. A key naming a
					// path that resolves outside the root is a key the caller
					// can hand straight back to Get.
					landed, err := filepath.EvalSymlinks(filepath.Join(root, key))
					if err != nil {
						continue
					}
					if !strings.HasPrefix(landed, realRoot+string(filepath.Separator)) {
						t.Errorf("List(%q) answered %q, which is %s -- outside the root", prefix, key, landed)
					}
				}
			}
		})
	}
}

// TestLocalAPathThatPassedTheCheckIsNotACapability is the race written down:
// the path is checked while it names a directory inside the root, the directory
// becomes a link out of it, and the operation runs against the name that was
// approved.
//
// It is deterministic rather than concurrent because the window is not the
// point. The point is that the check and the syscall are two separate
// resolutions of one string, and a check that resolves it itself is answering
// about a filesystem that no longer exists by the time the syscall runs.
func TestLocalAPathThatPassedTheCheckIsNotACapability(t *testing.T) {
	root, outside, a := symlinkFixture(t)
	ctx := context.Background()

	// A real directory holding a real file, entirely inside the root.
	if err := a.Put(ctx, "reports/q1.txt", strings.NewReader("inside"), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// The check. It passes, and it passes for the right reason.
	if _, err := a.Path("reports/q1.txt"); err != nil {
		t.Fatalf("Path: %v", err)
	}

	// The swap.
	if err := os.RemoveAll(filepath.Join(root, "reports")); err != nil {
		t.Fatal(err)
	}
	symlink(t, outside, filepath.Join(root, "reports"))

	// The use.
	if f, err := a.Get(ctx, "reports/secret.txt"); err == nil {
		body, _ := io.ReadAll(f.Body)
		_ = f.Body.Close()
		t.Errorf("Get read %q after the directory became a link out of the root", body)
	}
	if p, err := a.Path("reports/secret.txt"); err == nil {
		landed, err := filepath.EvalSymlinks(p)
		realRoot, rootErr := filepath.EvalSymlinks(root)
		if err == nil && rootErr == nil && !strings.HasPrefix(landed, realRoot+string(filepath.Separator)) {
			t.Errorf("Path answered %q, which is %s -- outside the root", p, landed)
		}
	}
	_ = a.Put(ctx, "reports/planted.txt", strings.NewReader("planted"), "")
	_ = a.Delete(ctx, "reports/secret.txt")
	assertOutsideIsUntouched(t, outside)
}

// TestLocalASymlinkIsRefusedThroughTheDiskToo puts the same question at the
// layer an application actually calls: the key goes through the Grant, the
// Grant puts the tenant in front of it, and the adapter sees a stored path it
// did not build.
//
// It is also where the tenant prefix is proved to be untouched by the
// containment fix. The link is planted inside the tenant's own directory, so
// reaching it at all takes a key that carried the prefix -- and what comes back
// is the tenant's own file, alone.
func TestLocalASymlinkIsRefusedThroughTheDiskToo(t *testing.T) {
	root, outside, a := symlinkFixture(t)
	d := filesystem.NewDisk("local", a)
	g := grant(tenant)
	ctx := context.Background()

	// A file of the tenant's own, so the listing has something right to say.
	put(t, d, g, "invoices/q1.pdf", "mine")
	// And a link inside the tenant's directory, pointing out of the root.
	symlink(t, outside, filepath.Join(root, tenant, "out"))

	if f, err := d.Get(ctx, g, "out/secret.txt"); err == nil {
		body, _ := io.ReadAll(f.Body)
		_ = f.Body.Close()
		t.Errorf("Get read %q from outside the root", body)
	}
	if err := d.Put(ctx, g, "out/planted.txt", strings.NewReader("planted"), ""); err == nil {
		t.Error("Put wrote through a link out of the root")
	}
	if err := d.Delete(ctx, g, "out/secret.txt"); err == nil {
		t.Error("Delete removed a file outside the root")
	}

	keys, err := d.AllFiles(ctx, g, "")
	if err != nil {
		t.Fatalf("AllFiles: %v", err)
	}
	if !equal(keys, []string{"invoices/q1.pdf"}) {
		t.Errorf("AllFiles = %v, want the tenant's own file and nothing else", keys)
	}
	// The other tenant holds the same shape of Grant and reaches none of it.
	if keys, err := d.AllFiles(ctx, grant(otherTenant), ""); err != nil || len(keys) != 0 {
		t.Errorf("another tenant sees %v (%v)", keys, err)
	}
	assertOutsideIsUntouched(t, outside)
}

// assertOutsideIsUntouched is the whole assertion for a write or a delete:
// anything extra beside the root was put there through the root, and anything
// missing was removed through it.
func assertOutsideIsUntouched(t *testing.T, outside string) {
	t.Helper()

	want := map[string]string{
		"secret.txt":     outsideSecret,
		"sub/nested.txt": outsideNested,
	}
	got := map[string]string{}
	err := filepath.WalkDir(outside, func(p string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(outside, p)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(relative)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, body := range want {
		switch have, ok := got[name]; {
		case !ok:
			t.Errorf("%s was deleted from outside the root", name)
		case have != body:
			t.Errorf("%s outside the root now holds %q", name, have)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s was created outside the root", name)
		}
	}
}
