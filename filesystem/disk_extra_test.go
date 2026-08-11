package filesystem_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/encryption"
	"github.com/arandu-io/hesape/filesystem"
)

func namedLocalDisk(t *testing.T, name string) *filesystem.Disk {
	t.Helper()
	adapter, err := filesystem.NewLocalFilesystemAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("local adapter: %v", err)
	}
	return filesystem.NewDisk(name, adapter)
}

func TestFileExistsAndDirectoryExistsAreDifferentQuestions(t *testing.T) {
	d := namedLocalDisk(t, "local")
	ctx := context.Background()
	g := grant(tenant)

	put(t, d, g, "invoices/q1.pdf", "hello")

	ok, err := d.FileExists(ctx, g, "invoices/q1.pdf")
	if err != nil || !ok {
		t.Fatalf("file exists: %v, %v", ok, err)
	}
	missing, err := d.FileMissing(ctx, g, "invoices/q2.pdf")
	if err != nil || !missing {
		t.Fatalf("file missing: %v, %v", missing, err)
	}

	// The key is a file, not a directory: asking the wrong question must not
	// answer yes, or a caller listing "invoices/q1.pdf" walks into nothing.
	ok, err = d.DirectoryExists(ctx, g, "invoices")
	if err != nil || !ok {
		t.Fatalf("directory exists: %v, %v", ok, err)
	}
	gone, err := d.DirectoryMissing(ctx, g, "receipts")
	if err != nil || !gone {
		t.Fatalf("directory missing: %v, %v", gone, err)
	}
}

func TestDirectoryExistsCannotSeeAnotherTenants(t *testing.T) {
	const other = "22222222-2222-4222-8222-222222222222"
	d := namedLocalDisk(t, "local")
	ctx := context.Background()

	put(t, d, grant(tenant), "invoices/q1.pdf", "mine")

	ok, err := d.DirectoryExists(ctx, grant(other), "invoices")
	if err != nil {
		t.Fatalf("directory exists: %v", err)
	}
	if ok {
		t.Fatal("one tenant can see that another has a directory")
	}
}

func TestMakeDirectoryOnADriverWithoutDirectoriesIsNotAFailure(t *testing.T) {
	// An object store has no directories. Reporting an error would make every
	// module that calls it work on disk and fail on the bucket; writing a marker
	// object would put a key in every listing that Get answers ErrNotFound for.
	d := filesystem.NewDisk("bucket", newMem())

	if err := d.MakeDirectory(context.Background(), grant(tenant), "invoices"); err != nil {
		t.Fatalf("make directory: %v", err)
	}
}

func TestMakeDirectoryOnADriverWithThemMakesOne(t *testing.T) {
	d := namedLocalDisk(t, "local")
	ctx := context.Background()
	g := grant(tenant)

	if err := d.MakeDirectory(ctx, g, "reports"); err != nil {
		t.Fatalf("make directory: %v", err)
	}
	ok, err := d.DirectoryExists(ctx, g, "reports")
	if err != nil || !ok {
		t.Fatalf("the directory was made and does not exist: %v, %v", ok, err)
	}
}

func TestMakeDirectoryRefusesWithoutATenant(t *testing.T) {
	d := namedLocalDisk(t, "local")

	if err := d.MakeDirectory(context.Background(), auth.Grant{}, "reports"); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("got %v, want ErrNoTenant", err)
	}
}

func TestVisibilityRoundTripsOnTheLocalDriver(t *testing.T) {
	d := namedLocalDisk(t, "local")
	ctx := context.Background()
	g := grant(tenant)

	put(t, d, g, "logo.png", "bytes")

	if err := d.SetVisibility(ctx, g, "logo.png", filesystem.VisibilityPrivate); err != nil {
		t.Fatalf("set visibility: %v", err)
	}
	got, err := d.GetVisibility(ctx, g, "logo.png")
	if err != nil || got != filesystem.VisibilityPrivate {
		t.Fatalf("got %q, %v", got, err)
	}

	if err := d.SetVisibility(ctx, g, "logo.png", filesystem.VisibilityPublic); err != nil {
		t.Fatalf("set visibility: %v", err)
	}
	got, err = d.GetVisibility(ctx, g, "logo.png")
	if err != nil || got != filesystem.VisibilityPublic {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestVisibilityRefusesAWordThatIsNotOne(t *testing.T) {
	d := namedLocalDisk(t, "local")
	g := grant(tenant)
	put(t, d, g, "logo.png", "bytes")

	if err := d.SetVisibility(context.Background(), g, "logo.png", "world-readable"); err == nil {
		t.Fatal("a visibility nobody defined was accepted")
	}
}

func TestVisibilitySaysSoRatherThanGuessingOnADriverWithoutIt(t *testing.T) {
	d := filesystem.NewDisk("bucket", newMem())
	g := grant(tenant)
	put(t, d, g, "logo.png", "bytes")

	// "private" would read as a guarantee this package did not make.
	if _, err := d.GetVisibility(context.Background(), g, "logo.png"); !errors.Is(err, filesystem.ErrNoVisibility) {
		t.Fatalf("got %v, want ErrNoVisibility", err)
	}
}

func TestVisibilityStillNeedsAGrant(t *testing.T) {
	d := namedLocalDisk(t, "local")

	if _, err := d.GetVisibility(context.Background(), auth.Grant{}, "logo.png"); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("got %v, want ErrNoTenant", err)
	}
}

func TestPathCarriesTheTenantAndOnlyForAGrantThatHasOne(t *testing.T) {
	d := namedLocalDisk(t, "local")
	g := grant(tenant)
	put(t, d, g, "invoices/q1.pdf", "hello")

	path, err := d.Path(g, "invoices/q1.pdf")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if !strings.Contains(path, tenant) {
		t.Fatalf("the path does not carry the tenant: %q", path)
	}
	if _, err := d.Path(auth.Grant{}, "invoices/q1.pdf"); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("got %v, want ErrNoTenant", err)
	}
}

func TestPathSaysSoOnADriverWhoseFilesAreElsewhere(t *testing.T) {
	d := filesystem.NewDisk("bucket", newMem())

	if _, err := d.Path(grant(tenant), "x.pdf"); !errors.Is(err, filesystem.ErrNoPath) {
		t.Fatalf("got %v, want ErrNoPath", err)
	}
}

func TestReadStreamAndWriteStream(t *testing.T) {
	d := namedLocalDisk(t, "local")
	ctx := context.Background()
	g := grant(tenant)

	if err := d.WriteStream(ctx, g, "notes.txt", strings.NewReader("streamed"), ""); err != nil {
		t.Fatalf("write stream: %v", err)
	}
	body, err := d.ReadStream(ctx, g, "notes.txt")
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	defer body.Close()
	read, err := io.ReadAll(body)
	if err != nil || string(read) != "streamed" {
		t.Fatalf("got %q, %v", read, err)
	}
}

func TestDiskJson(t *testing.T) {
	d := namedLocalDisk(t, "local")
	ctx := context.Background()
	g := grant(tenant)
	put(t, d, g, "manifest.json", `{"version":3}`)

	out, err := d.Json(ctx, g, "manifest.json")
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if out["version"] != float64(3) {
		t.Fatalf("got %v", out)
	}

	put(t, d, g, "broken.json", "not json")
	if _, err := d.Json(ctx, g, "broken.json"); err == nil {
		t.Fatal("a file that is not JSON was decoded")
	}
}

func TestURLIsRefusedUnlessTheDiskHasAPublicAddress(t *testing.T) {
	d := namedLocalDisk(t, "local")

	if _, err := d.URL(context.Background(), grant(tenant), "logo.png"); !errors.Is(err, filesystem.ErrNoURL) {
		t.Fatalf("got %v, want ErrNoURL", err)
	}
}

func TestURLCarriesTheTenantSoTwoTenantsNeverShareAnAddress(t *testing.T) {
	const other = "22222222-2222-4222-8222-222222222222"
	adapter, err := filesystem.NewLocalFilesystemAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("local adapter: %v", err)
	}
	d := filesystem.NewDisk("public", adapter, filesystem.Config{URL: "https://cdn.example/files/"})
	ctx := context.Background()

	mine, err := d.URL(ctx, grant(tenant), "logo.png")
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	theirs, err := d.URL(ctx, grant(other), "logo.png")
	if err != nil {
		t.Fatalf("url: %v", err)
	}
	if mine == theirs {
		t.Fatalf("two tenants got the same address for their own file: %q", mine)
	}
	if !strings.HasPrefix(mine, "https://cdn.example/files/"+tenant+"/") {
		t.Fatalf("got %q", mine)
	}
}

func TestURLStillNeedsAGrant(t *testing.T) {
	adapter, _ := filesystem.NewLocalFilesystemAdapter(t.TempDir())
	d := filesystem.NewDisk("public", adapter, filesystem.Config{URL: "https://cdn.example"})

	if _, err := d.URL(context.Background(), auth.Grant{}, "logo.png"); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("got %v, want ErrNoTenant", err)
	}
}

// presigningMem is an Adapter that can presign both ways, which is what an
// object store is.
type presigningMem struct{ *memAdapter }

func (p presigningMem) PresignGet(_ context.Context, storedPath string, ttl time.Duration) (string, error) {
	return "https://bucket.example/" + storedPath + "?get=" + ttl.String(), nil
}

func (p presigningMem) PresignPut(_ context.Context, storedPath string, ttl time.Duration) (string, http.Header, error) {
	h := http.Header{}
	h.Set("X-Amz-Acl", "private")
	return "https://bucket.example/" + storedPath + "?put=" + ttl.String(), h, nil
}

func TestTemporaryURLsComeFromTheDriverWhenItCanPresign(t *testing.T) {
	d := filesystem.NewDisk("bucket", presigningMem{newMem()})
	ctx := context.Background()
	g := grant(tenant)

	if !d.ProvidesTemporaryURLs() || !d.ProvidesTemporaryUploadURLs() {
		t.Fatal("a presigning driver reports it cannot presign")
	}

	link, err := d.TemporaryURL(ctx, g, "invoices/q1.pdf", time.Minute)
	if err != nil {
		t.Fatalf("temporary url: %v", err)
	}
	if !strings.Contains(link, tenant) {
		t.Fatalf("the presigned link does not name the tenant's object: %q", link)
	}

	upload, headers, err := d.TemporaryUploadURL(ctx, g, "invoices/q2.pdf", time.Minute)
	if err != nil {
		t.Fatalf("temporary upload url: %v", err)
	}
	if !strings.Contains(upload, tenant) || headers.Get("X-Amz-Acl") == "" {
		t.Fatalf("got %q and %v", upload, headers)
	}
}

func TestATemporaryURLWithoutALifetimeIsRefused(t *testing.T) {
	d := filesystem.NewDisk("bucket", presigningMem{newMem()})

	if _, err := d.TemporaryURL(context.Background(), grant(tenant), "x.pdf", 0); err == nil {
		t.Fatal("a link with no expiry was issued")
	}
	if _, _, err := d.TemporaryUploadURL(context.Background(), grant(tenant), "x.pdf", 0); err == nil {
		t.Fatal("an upload link with no expiry was issued")
	}
}

func TestALocalDiskGetsTemporaryURLsFromTheCallback(t *testing.T) {
	d := namedLocalDisk(t, "local")
	ctx := context.Background()

	if d.ProvidesTemporaryURLs() {
		t.Fatal("a directory on disk reports it can presign")
	}
	signer := filesystem.NewURLSigner(encryption.NewSigner([]byte("0123456789abcdef0123456789abcdef")), "/files")
	d.BuildTemporaryURLsUsing(func(ctx context.Context, g auth.Grant, key string, ttl time.Duration) (string, error) {
		return signer.TemporaryURL(ctx, g, d, key, ttl)
	})

	if !d.ProvidesTemporaryURLs() {
		t.Fatal("the callback was installed and the disk still says it cannot")
	}
	link, err := d.TemporaryURL(ctx, grant(tenant), "invoices/q1.pdf", time.Minute)
	if err != nil {
		t.Fatalf("temporary url: %v", err)
	}
	if !strings.HasPrefix(link, "/files/") {
		t.Fatalf("got %q", link)
	}
	// The tenant must not travel in the clear: it is inside the signed payload.
	if strings.Contains(link, tenant) {
		t.Fatalf("the link exposes the tenant: %q", link)
	}
}

func TestTemporaryUploadURLIsRefusedWhenNothingCanIssueOne(t *testing.T) {
	d := namedLocalDisk(t, "local")

	if d.ProvidesTemporaryUploadURLs() {
		t.Fatal("a directory on disk reports it can presign an upload")
	}
	if _, _, err := d.TemporaryUploadURL(context.Background(), grant(tenant), "x.pdf", time.Minute); !errors.Is(err, filesystem.ErrNoURL) {
		t.Fatalf("got %v, want ErrNoURL", err)
	}
}

func TestResponseAndDownloadDifferByOneHeader(t *testing.T) {
	d := namedLocalDisk(t, "local")
	g := grant(tenant)
	put(t, d, g, "report.pdf", "%PDF-1.4")

	inline := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/report", nil)
	if err := d.Response(inline, r, g, "report.pdf", "", http.Header{"X-Trace": {"abc"}}); err != nil {
		t.Fatalf("response: %v", err)
	}
	if got := inline.Header().Get("Content-Disposition"); !strings.HasPrefix(got, "inline") {
		t.Fatalf("got %q", got)
	}
	if inline.Header().Get("X-Trace") != "abc" {
		t.Fatal("the extra header was dropped")
	}
	if inline.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("nosniff is not negotiable and it is missing")
	}

	attached := httptest.NewRecorder()
	if err := d.Download(attached, r, g, "report.pdf", "q1.pdf", nil); err != nil {
		t.Fatalf("download: %v", err)
	}
	got := attached.Header().Get("Content-Disposition")
	if !strings.HasPrefix(got, "attachment") || !strings.Contains(got, "q1.pdf") {
		t.Fatalf("got %q", got)
	}
}

func TestServeUsingTakesOverTheResponse(t *testing.T) {
	d := namedLocalDisk(t, "local")
	g := grant(tenant)
	put(t, d, g, "report.pdf", "%PDF-1.4")

	d.ServeUsing(func(w http.ResponseWriter, _ *http.Request, _ auth.Grant, key string, opt filesystem.ServeOptions) error {
		w.Header().Set("Location", "https://cdn.example/"+key)
		w.WriteHeader(http.StatusFound)
		return nil
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/report", nil)
	if err := d.Response(w, r, g, "report.pdf", "", nil); err != nil {
		t.Fatalf("response: %v", err)
	}
	if w.Code != http.StatusFound {
		t.Fatalf("got %d, want a redirect from the installed callback", w.Code)
	}
}

func TestServeFileRedeemsALinkAndRefusesEverythingElse(t *testing.T) {
	adapter, err := filesystem.NewLocalFilesystemAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("local adapter: %v", err)
	}
	adapter.DiskName("local").ShouldServeSignedUrls(true)
	if !adapter.ServesSignedURLs() {
		t.Fatal("the setting did not stick")
	}

	disks := filesystem.NewDisks("local")
	d := disks.Add("local", adapter)
	g := grant(tenant)
	put(t, d, g, "invoices/q1.pdf", "hello")

	signer := filesystem.NewURLSigner(encryption.NewSigner([]byte("0123456789abcdef0123456789abcdef")), "/files")
	link, err := signer.TemporaryURL(context.Background(), g, d, "invoices/q1.pdf", time.Minute)
	if err != nil {
		t.Fatalf("temporary url: %v", err)
	}

	route := filesystem.NewServeFile(disks, signer, true)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, strings.TrimPrefix(link, "/files"), nil)
	route.ServeHTTP(w, r)
	if w.Code != http.StatusOK || w.Body.String() != "hello" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}

	forged := httptest.NewRecorder()
	route.ServeHTTP(forged, httptest.NewRequest(http.MethodGet, "/not-a-token", nil))
	if forged.Code != http.StatusForbidden {
		t.Fatalf("a forged token got %d", forged.Code)
	}

	off := filesystem.NewServeFile(disks, signer, false)
	refused := httptest.NewRecorder()
	off.ServeHTTP(refused, httptest.NewRequest(http.MethodGet, strings.TrimPrefix(link, "/files"), nil))
	if refused.Code != http.StatusForbidden {
		t.Fatalf("a disk that is not served by this route got %d", refused.Code)
	}
}

func TestBuildMakesALocalDiskAndScopedNarrowsIt(t *testing.T) {
	root := t.TempDir()
	disks := filesystem.NewDisks("local", "cloud")
	ctx := context.Background()
	g := grant(tenant)

	local, err := disks.Build("local", filesystem.Config{Driver: "local", Root: root})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	disks.Set("local", local)

	scoped, err := disks.Build("uploads", filesystem.Config{Driver: "scoped", Disk: "local", Prefix: "uploads"})
	if err != nil {
		t.Fatalf("build scoped: %v", err)
	}
	disks.Set("uploads", scoped)

	put(t, scoped, g, "q1.pdf", "scoped")

	// The scope wraps the disk and the tenant is inside it, so the object lands
	// at <prefix>/<tenant>/<key> -- the prefix narrows which part of the disk
	// this one reaches, and the tenant still separates two customers inside it.
	if _, err := os.Stat(filepath.Join(root, "uploads", tenant, "q1.pdf")); err != nil {
		t.Fatalf("the scoped disk stored somewhere else: %v", err)
	}
	// The same key on the disk being scoped is a different object, and here it
	// is no object at all.
	loose, err := local.Exists(ctx, g, "q1.pdf")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if loose {
		t.Fatal("the scoped disk wrote outside its prefix")
	}
	keys, err := scoped.AllFiles(ctx, g, "")
	if err != nil {
		t.Fatalf("all files: %v", err)
	}
	if len(keys) != 1 || keys[0] != "q1.pdf" {
		t.Fatalf("the scoped listing shows %v, and the caller cannot address those", keys)
	}
}

func TestScopedDriverRefusesAConfigurationThatScopesNothing(t *testing.T) {
	disks := filesystem.NewDisks("local")

	if _, err := disks.Build("uploads", filesystem.Config{Driver: "scoped", Prefix: "uploads"}); err == nil {
		t.Fatal("a scoped disk with no disk to scope was built")
	}
	if _, err := disks.Build("uploads", filesystem.Config{Driver: "scoped", Disk: "local"}); err == nil {
		t.Fatal("a scoped disk with no prefix was built")
	}
}

func TestExtendRegistersADriverAndBuildUsesIt(t *testing.T) {
	disks := filesystem.NewDisks("local")
	disks.Extend("memory", func(name string, cfg filesystem.Config) (filesystem.Adapter, error) {
		return newMem(), nil
	})

	d, err := disks.Build("scratch", filesystem.Config{Driver: "memory"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	put(t, d, grant(tenant), "x.txt", "x")

	if _, err := disks.Build("nope", filesystem.Config{Driver: "ftp"}); err == nil {
		t.Fatal("a driver nobody registered was built")
	}
}

func TestDriveCloudForgetAndPurge(t *testing.T) {
	disks := filesystem.NewDisks("local", "cloud")
	disks.Add("local", newMem())
	disks.Add("cloud", newMem())

	if disks.GetDefaultDriver() != "local" || disks.GetDefaultCloudDriver() != "cloud" {
		t.Fatal("the defaults are not what the set was built with")
	}
	if _, err := disks.Drive(""); err != nil {
		t.Fatalf("drive: %v", err)
	}
	if _, err := disks.Cloud(); err != nil {
		t.Fatalf("cloud: %v", err)
	}

	disks.ForgetDisk("cloud")
	if _, err := disks.Cloud(); !errors.Is(err, filesystem.ErrNoDisk) {
		t.Fatalf("got %v, want ErrNoDisk", err)
	}
	disks.Purge("")
	if _, err := disks.Disk(""); !errors.Is(err, filesystem.ErrNoDisk) {
		t.Fatalf("got %v, want ErrNoDisk", err)
	}

	noCloud := filesystem.NewDisks("local")
	if _, err := noCloud.Cloud(); !errors.Is(err, filesystem.ErrNoDisk) {
		t.Fatalf("got %v, want ErrNoDisk", err)
	}
}

func TestGetAdapterAndGetDriverAnswerWithTheSameObject(t *testing.T) {
	mem := newMem()
	d := filesystem.NewDisk("local", mem, filesystem.Config{Driver: "local", Root: "/tmp"})

	if d.GetAdapter() != filesystem.Adapter(mem) || d.GetDriver() != filesystem.Adapter(mem) {
		t.Fatal("the two names answer with different objects, and there is only one")
	}
	if d.GetConfig().Root != "/tmp" {
		t.Fatalf("got %v", d.GetConfig())
	}
}

// recorder is the smallest thing that is a TB, so the assertions can be tested
// for failing as well as for passing.
type recorder struct{ failures []string }

func (r *recorder) Helper()                           {}
func (r *recorder) Errorf(format string, args ...any) { r.failures = append(r.failures, format) }
func (r *recorder) failed() bool                      { return len(r.failures) > 0 }

func TestTheAssertionsPassAndFailOnTheRightThings(t *testing.T) {
	d := namedLocalDisk(t, "local")
	ctx := context.Background()
	g := grant(tenant)
	put(t, d, g, "invoices/q1.pdf", "hello")

	passing := &recorder{}
	d.AssertExists(ctx, passing, g, "invoices/q1.pdf", []byte("hello"))
	d.AssertMissing(ctx, passing, g, "invoices/q2.pdf")
	d.AssertCount(ctx, passing, g, "invoices", 1, false)
	d.AssertDirectoryEmpty(ctx, passing, g, "receipts")
	if passing.failed() {
		t.Fatalf("assertions that should hold reported: %v", passing.failures)
	}

	failing := &recorder{}
	d.AssertExists(ctx, failing, g, "invoices/q1.pdf", []byte("goodbye"))
	d.AssertMissing(ctx, failing, g, "invoices/q1.pdf")
	d.AssertCount(ctx, failing, g, "invoices", 9, true)
	if len(failing.failures) != 3 {
		t.Fatalf("expected three failures, got %d", len(failing.failures))
	}
}
