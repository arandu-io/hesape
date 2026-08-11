package filesystem_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/filesystem"
)

func put(t *testing.T, d *filesystem.Disk, g auth.Grant, key, body string) {
	t.Helper()
	if err := d.Put(context.Background(), g, key, strings.NewReader(body), ""); err != nil {
		t.Fatalf("Put %s: %v", key, err)
	}
}

func read(t *testing.T, d *filesystem.Disk, g auth.Grant, key string) string {
	t.Helper()
	f, err := d.Get(context.Background(), g, key)
	if err != nil {
		t.Fatalf("Get %s: %v", key, err)
	}
	defer f.Body.Close()
	b, err := io.ReadAll(f.Body)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return string(b)
}

// TestTheAdapterNeverSeesAGrant is the reason the interface was split. A driver
// is handed a path and nothing else, so there is no driver in which the tenant
// prefix can be forgotten -- it was never optional to apply, because it was
// applied before the call.
func TestTheAdapterNeverSeesAGrant(t *testing.T) {
	adapterType := reflect.TypeOf((*filesystem.Adapter)(nil)).Elem()
	grantType := reflect.TypeOf(auth.Grant{})
	for i := 0; i < adapterType.NumMethod(); i++ {
		m := adapterType.Method(i)
		fn := m.Type
		for j := 0; j < fn.NumIn(); j++ {
			if fn.In(j) == grantType {
				t.Errorf("Adapter.%s takes an auth.Grant; a driver that can see one is a driver that can build its own path", m.Name)
			}
		}
	}
}

// TestTheStoredPathCarriesTheTenant: what the driver receives, spelled out.
func TestTheStoredPathCarriesTheTenant(t *testing.T) {
	mem := newMem()
	d := filesystem.NewDisk("local", mem)

	put(t, d, grant(tenant), "invoices/q1.pdf", "hello")

	if _, ok := mem.files[tenant+"/invoices/q1.pdf"]; !ok {
		t.Fatalf("stored under %v, want %q", mem.seen, tenant+"/invoices/q1.pdf")
	}
}

// TestOneTenantCannotReadAnother is the whole product thesis at the size of one
// file: the same key, two Grants, two different objects.
func TestOneTenantCannotReadAnother(t *testing.T) {
	const other = "22222222-2222-4222-8222-222222222222"
	d := filesystem.NewDisk("local", newMem())
	ctx := context.Background()

	put(t, d, grant(tenant), "secret.txt", "mine")

	if _, err := d.Get(ctx, grant(other), "secret.txt"); !errors.Is(err, filesystem.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	// And it cannot be reached by naming the other tenant either.
	if _, err := d.Get(ctx, grant(other), tenant+"/secret.txt"); !errors.Is(err, filesystem.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if got := read(t, d, grant(tenant), "secret.txt"); got != "mine" {
		t.Fatalf("body = %q, want %q", got, "mine")
	}
}

// TestGetAnswersInKeysAndNotInPaths: a caller that saw the tenant prefix come
// back would start building paths out of it.
func TestGetAnswersInKeysAndNotInPaths(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	ctx := context.Background()
	put(t, d, grant(tenant), "invoices/q1.pdf", "hello")

	f, err := d.Get(ctx, grant(tenant), "invoices/q1.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Body.Close()
	if f.Key != "invoices/q1.pdf" {
		t.Fatalf("Key = %q, want the key the caller asked for", f.Key)
	}

	info, err := d.Stat(ctx, grant(tenant), "invoices/q1.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if info.Key != "invoices/q1.pdf" {
		t.Fatalf("Stat Key = %q", info.Key)
	}
	if info.Size != 5 {
		t.Fatalf("Size = %d, want 5", info.Size)
	}
}

// TestTheStoredTypeComesFromTheKey: an upload announces whatever it likes, and
// a stored text/html is how one becomes stored XSS.
func TestTheStoredTypeComesFromTheKey(t *testing.T) {
	mem := newMem()
	d := filesystem.NewDisk("local", mem)
	put(t, d, grant(tenant), "notes.txt", "x")

	got := mem.files[tenant+"/notes.txt"].contentType
	if !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("content type = %q, want text/plain", got)
	}

	put(t, d, grant(tenant), "payload.wat", "x")
	if got := mem.files[tenant+"/payload.wat"].contentType; got != "application/octet-stream" {
		t.Fatalf("content type = %q, want application/octet-stream for an unknown extension", got)
	}
}

// TestAnExplicitTypeIsKept: inference is the default, not the only option --
// a caller that knows better than the extension says so.
func TestAnExplicitTypeIsKept(t *testing.T) {
	mem := newMem()
	d := filesystem.NewDisk("local", mem)
	if err := d.Put(context.Background(), grant(tenant), "export", strings.NewReader("a,b"), "text/csv"); err != nil {
		t.Fatal(err)
	}
	if got := mem.files[tenant+"/export"].contentType; got != "text/csv" {
		t.Fatalf("content type = %q, want text/csv", got)
	}
}

func TestExistsAndDelete(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	ctx := context.Background()
	g := grant(tenant)

	put(t, d, g, "a.txt", "a")
	if ok, err := d.Exists(ctx, g, "a.txt"); err != nil || !ok {
		t.Fatalf("Exists = %v, %v", ok, err)
	}
	if err := d.Delete(ctx, g, "a.txt"); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.Exists(ctx, g, "a.txt"); err != nil || ok {
		t.Fatalf("Exists after Delete = %v, %v", ok, err)
	}
	// Deleting what is not there is not an error.
	if err := d.Delete(ctx, g, "a.txt"); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
}

// TestListReturnsKeysSortedAndScoped: the tenant part is stripped, the order is
// promised, and another tenant's files are not in it.
func TestListReturnsKeysSortedAndScoped(t *testing.T) {
	const other = "22222222-2222-4222-8222-222222222222"
	d := filesystem.NewDisk("local", newMem())
	ctx := context.Background()

	put(t, d, grant(tenant), "invoices/b.pdf", "b")
	put(t, d, grant(tenant), "invoices/a.pdf", "a")
	put(t, d, grant(tenant), "notes.txt", "n")
	put(t, d, grant(other), "invoices/secret.pdf", "s")

	got, err := d.AllFiles(ctx, grant(tenant), "invoices/")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"invoices/a.pdf", "invoices/b.pdf"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("List = %v, want %v", got, want)
	}

	all, err := d.AllFiles(ctx, grant(tenant), "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"invoices/a.pdf", "invoices/b.pdf", "notes.txt"}; !reflect.DeepEqual(all, want) {
		t.Fatalf("List(\"\") = %v, want %v", all, want)
	}
}

// TestListCannotBeAskedForAnotherTenant: RULE 17 at its sharpest -- a listing
// is the one read that hands over every name at once.
func TestListCannotBeAskedForAnotherTenant(t *testing.T) {
	const other = "22222222-2222-4222-8222-222222222222"
	d := filesystem.NewDisk("local", newMem())
	ctx := context.Background()
	put(t, d, grant(other), "secret.pdf", "s")

	got, err := d.AllFiles(ctx, grant(tenant), other+"/")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("List = %v, want nothing", got)
	}

	if _, err := d.AllFiles(ctx, grant(tenant), "../"+other); err == nil {
		t.Fatal("a prefix that escapes was accepted")
	}
	if _, err := d.AllFiles(ctx, auth.Grant{}, ""); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}

func TestCopyAndMove(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	ctx := context.Background()
	g := grant(tenant)

	put(t, d, g, "draft.txt", "body")

	if err := d.Copy(ctx, g, "draft.txt", "archive/draft.txt"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, d, g, "archive/draft.txt"); got != "body" {
		t.Fatalf("copy body = %q", got)
	}
	if got := read(t, d, g, "draft.txt"); got != "body" {
		t.Fatal("Copy removed the source")
	}

	if err := d.Move(ctx, g, "draft.txt", "final.txt"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, d, g, "final.txt"); got != "body" {
		t.Fatalf("moved body = %q", got)
	}
	if ok, _ := d.Exists(ctx, g, "draft.txt"); ok {
		t.Fatal("Move left the source behind")
	}
}

// TestMoveOntoItselfIsNotADeletion: copy-then-delete with the same two names
// would end with nothing at all.
func TestMoveOntoItselfIsNotADeletion(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	g := grant(tenant)
	put(t, d, g, "a.txt", "body")

	if err := d.Move(context.Background(), g, "a.txt", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, d, g, "a.txt"); got != "body" {
		t.Fatalf("body = %q, want it still there", got)
	}
}

// TestCopyCannotCrossTenants: both keys are resolved against the same Grant, so
// "copy this into the shared folder" is not expressible.
func TestCopyCannotCrossTenants(t *testing.T) {
	const other = "22222222-2222-4222-8222-222222222222"
	mem := newMem()
	d := filesystem.NewDisk("local", mem)
	g := grant(tenant)
	put(t, d, g, "secret.txt", "mine")

	if err := d.Copy(context.Background(), g, "secret.txt", "../"+other+"/stolen.txt"); err == nil {
		t.Fatal("a copy out of the tenant was accepted")
	}
	for path := range mem.files {
		if strings.HasPrefix(path, other+"/") {
			t.Fatalf("a file landed in another tenant: %q", path)
		}
	}
}

func TestDeleteDirectory(t *testing.T) {
	d := filesystem.NewDisk("local", newMem())
	ctx := context.Background()
	g := grant(tenant)

	put(t, d, g, "tmp/a.txt", "a")
	put(t, d, g, "tmp/b.txt", "b")
	put(t, d, g, "keep.txt", "k")

	if err := d.DeleteDirectory(ctx, g, "tmp/"); err != nil {
		t.Fatal(err)
	}
	left, err := d.AllFiles(ctx, g, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"keep.txt"}; !reflect.DeepEqual(left, want) {
		t.Fatalf("left = %v, want %v", left, want)
	}
}

// TestDeleteDirectoryStopsAtTheTenant: the blast radius is the tenant, even for an
// empty prefix, and never wider.
func TestDeleteDirectoryStopsAtTheTenant(t *testing.T) {
	const other = "22222222-2222-4222-8222-222222222222"
	d := filesystem.NewDisk("local", newMem())
	ctx := context.Background()

	put(t, d, grant(tenant), "a.txt", "a")
	put(t, d, grant(other), "b.txt", "b")

	if err := d.DeleteDirectory(ctx, grant(tenant), ""); err != nil {
		t.Fatal(err)
	}
	if ok, err := d.Exists(ctx, grant(other), "b.txt"); err != nil || !ok {
		t.Fatalf("another tenant's file was deleted: %v, %v", ok, err)
	}
}

// TestTheErrorNamesTheDiskAndTheKey, and still unwraps to ErrNotFound so a
// handler can turn it into a 404 without reading the sentence.
func TestTheErrorNamesTheDiskAndTheKey(t *testing.T) {
	d := filesystem.NewDisk("invoices", newMem())
	_, err := d.Get(context.Background(), grant(tenant), "missing.pdf")
	if !errors.Is(err, filesystem.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "invoices") || !strings.Contains(err.Error(), "missing.pdf") {
		t.Fatalf("err = %q, want the disk and the key in it", err)
	}
}

func TestDisksResolvesByNameAndDefault(t *testing.T) {
	ds := filesystem.NewDisks("local")
	ds.Add("local", newMem())
	ds.Add("s3", newMem())

	d, err := ds.Disk("")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name() != "local" {
		t.Fatalf("default = %q, want local", d.Name())
	}
	if d, err := ds.Disk("s3"); err != nil || d.Name() != "s3" {
		t.Fatalf("Disk(s3) = %v, %v", d, err)
	}
	if _, err := ds.Disk("nope"); !errors.Is(err, filesystem.ErrNoDisk) {
		t.Fatalf("err = %v, want ErrNoDisk", err)
	}
	if want := []string{"local", "s3"}; !reflect.DeepEqual(ds.Names(), want) {
		t.Fatalf("Names = %v, want %v", ds.Names(), want)
	}
}

// TestAnUnknownDiskSaysWhichOnesExist: the error is read by somebody who typed
// a name into a config file.
func TestAnUnknownDiskSaysWhichOnesExist(t *testing.T) {
	ds := filesystem.NewDisks("local")
	ds.Add("local", newMem())
	_, err := ds.Disk("s3")
	if err == nil || !strings.Contains(err.Error(), "local") {
		t.Fatalf("err = %v, want the registered names in it", err)
	}
}

// TestADefaultThatWasNeverRegisteredIsAnErrorAndNotAPanic: configuration names
// a disk before it is wired, and a boot that crashes on it is a crash for a
// typo.
func TestADefaultThatWasNeverRegisteredIsAnErrorAndNotAPanic(t *testing.T) {
	ds := filesystem.NewDisks("")
	if _, err := ds.Disk(""); !errors.Is(err, filesystem.ErrNoDisk) {
		t.Fatalf("err = %v, want ErrNoDisk", err)
	}
}
