package s3_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/arandu-io/hesape/auth"
	"github.com/arandu-io/hesape/filesystem"
	"github.com/arandu-io/hesape/filesystem/s3"
)

// The tests drive a server that answers like a bucket. What is being checked is
// the protocol: the path a stored path lands on, the signature the request
// carries, the pagination of a listing, and the errors that have to be
// indistinguishable.
//
// Against a real bucket they would need credentials in CI, and the value would
// be proving that Cloudflare implements S3 -- which is not this package's claim.

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	tenantB = "22222222-2222-4222-8222-222222222222"
)

func grant(tenant string) auth.Grant { return auth.SystemGrant("file.write", tenant) }

// bucket returns an adapter pointed at a handler, plus what the handler saw.
func bucket(t *testing.T, handler http.HandlerFunc) (*s3.AwsS3V3Adapter, *[]*http.Request) {
	t.Helper()

	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Clone(context.Background()))
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	adapter, err := s3.New(s3.Config{
		Endpoint:  srv.URL,
		Bucket:    "uploads",
		Region:    "auto",
		AccessKey: "key",
		SecretKey: "secret",
		PathStyle: true,
		Client:    srv.Client(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return adapter, &seen
}

// disk is what an application holds: the adapter behind a Disk, which is the
// only thing that knows about tenants.
func disk(t *testing.T, handler http.HandlerFunc) (*filesystem.Disk, *[]*http.Request) {
	t.Helper()
	adapter, seen := bucket(t, handler)
	return filesystem.NewDisk("s3", adapter), seen
}

// TestTheKeyLandsUnderTheTenant is the property the whole contract is built on,
// checked where it would actually break: in the URL.
func TestTheKeyLandsUnderTheTenant(t *testing.T) {
	d, seen := disk(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	err := d.Put(context.Background(), grant(tenantA), "invoices/2026-08.pdf",
		strings.NewReader("the invoice"), "application/pdf")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	if len(*seen) != 1 {
		t.Fatalf("%d requests", len(*seen))
	}
	path := (*seen)[0].URL.Path
	if want := "/uploads/" + tenantA + "/invoices/2026-08.pdf"; path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

// TestTheAdapterIsNeverToldTheTenant: it is handed a path and nothing else,
// which is why no method here can forget the prefix.
func TestTheAdapterIsNeverToldTheTenant(t *testing.T) {
	adapter, seen := bucket(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// A path with no tenant in it is uploaded exactly as given: this package has
	// no opinion about what a path means, which is the point.
	if err := adapter.Put(context.Background(), "a.pdf", strings.NewReader("x"), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := (*seen)[0].URL.Path; got != "/uploads/a.pdf" {
		t.Fatalf("path = %q", got)
	}
}

// TestTheRequestIsSigned: without the signature every call is a 403, and a
// signature that is present but wrong is the same 403 -- which is why the shape
// is worth checking here rather than discovering against a bucket.
func TestTheRequestIsSigned(t *testing.T) {
	d, seen := disk(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if err := d.Put(context.Background(), grant(tenantA), "a.pdf", strings.NewReader("x"), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	req := (*seen)[0]
	header := req.Header.Get("Authorization")
	for _, want := range []string{"AWS4-HMAC-SHA256", "Credential=key/", "SignedHeaders=", "Signature="} {
		if !strings.Contains(header, want) {
			t.Errorf("the Authorization header has no %q: %s", want, header)
		}
	}
	if req.Header.Get("X-Amz-Date") == "" {
		t.Error("no X-Amz-Date, and the signature is scoped to it")
	}
	// The payload hash is signed, which is what makes a modified body fail.
	if req.Header.Get("X-Amz-Content-Sha256") == "" {
		t.Error("no X-Amz-Content-Sha256")
	}
	if !strings.Contains(header, "/auto/s3/aws4_request") {
		t.Errorf("the scope is not the configured region: %s", header)
	}
}

func TestGetReturnsTheBody(t *testing.T) {
	d, _ := disk(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Last-Modified", "Mon, 10 Aug 2026 12:00:00 GMT")
		_, _ = w.Write([]byte("the invoice"))
	})

	f, err := d.Get(context.Background(), grant(tenantA), "a.pdf")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = f.Body.Close() }()

	body, _ := io.ReadAll(f.Body)
	if string(body) != "the invoice" {
		t.Fatalf("read %q", body)
	}
	if f.ContentType != "application/pdf" {
		t.Errorf("content type = %q", f.ContentType)
	}
	// The caller asked in keys and gets its own word back, never the path.
	if f.Key != "a.pdf" {
		t.Errorf("Key = %q", f.Key)
	}
	if f.ModifiedAt.IsZero() {
		t.Error("no modification time")
	}
}

// TestStatAsksWithoutMovingTheBytes: the metadata half of Get, which is what
// Size, LastModified and MimeType are built on.
func TestStatAsksWithoutMovingTheBytes(t *testing.T) {
	d, seen := disk(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", "11")
		w.Header().Set("Last-Modified", "Mon, 10 Aug 2026 12:00:00 GMT")
		w.WriteHeader(http.StatusOK)
	})

	info, err := d.Stat(context.Background(), grant(tenantA), "a.pdf")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if (*seen)[0].Method != http.MethodHead {
		t.Errorf("method = %s, want HEAD", (*seen)[0].Method)
	}
	if info.Size != 11 {
		t.Errorf("Size = %d", info.Size)
	}
	if info.ContentType != "application/pdf" {
		t.Errorf("ContentType = %q", info.ContentType)
	}
	if info.Key != "a.pdf" {
		t.Errorf("Key = %q", info.Key)
	}
}

// TestForbiddenAndMissingAreTheSameError: a bucket policy that hides an object
// answers 403, and telling the two apart would leak which keys exist.
func TestForbiddenAndMissingAreTheSameError(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		d, _ := disk(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
		})
		ctx := context.Background()

		if _, err := d.Get(ctx, grant(tenantA), "a.pdf"); !errors.Is(err, filesystem.ErrNotFound) {
			t.Errorf("status %d gave %v, want ErrNotFound", status, err)
		}
		if _, err := d.Stat(ctx, grant(tenantA), "a.pdf"); !errors.Is(err, filesystem.ErrNotFound) {
			t.Errorf("status %d: Stat = %v, want ErrNotFound", status, err)
		}
		exists, err := d.Exists(ctx, grant(tenantA), "a.pdf")
		if err != nil || exists {
			t.Errorf("status %d: exists = %v (%v)", status, exists, err)
		}
	}
}

// TestAnErrorSaysWhatTheBucketSaid: a bare "403 Forbidden" sends people to check
// credentials when the actual problem is the bucket name.
func TestAnErrorSaysWhatTheBucketSaid(t *testing.T) {
	d, _ := disk(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`<Error><Code>NoSuchBucket</Code><Message>The specified bucket does not exist</Message></Error>`))
	})

	err := d.Put(context.Background(), grant(tenantA), "a.pdf", strings.NewReader("x"), "")
	if err == nil {
		t.Fatal("a 400 was accepted")
	}
	if !strings.Contains(err.Error(), "NoSuchBucket") {
		t.Errorf("the error does not carry what the bucket said: %v", err)
	}
}

// TestListPaginates: a bucket with more than a thousand objects answers in
// pages, and stopping at the first silently loses the rest.
func TestListPaginates(t *testing.T) {
	page := 0
	d, _ := disk(t, func(w http.ResponseWriter, _ *http.Request) {
		page++
		if page == 1 {
			_, _ = w.Write([]byte(`<ListBucketResult>
				<Contents><Key>` + tenantA + `/a.pdf</Key></Contents>
				<Contents><Key>` + tenantA + `/folder/</Key></Contents>
				<IsTruncated>true</IsTruncated>
				<NextContinuationToken>more</NextContinuationToken>
			</ListBucketResult>`))
			return
		}
		_, _ = w.Write([]byte(`<ListBucketResult>
			<Contents><Key>` + tenantA + `/b.pdf</Key></Contents>
			<IsTruncated>false</IsTruncated>
		</ListBucketResult>`))
	})

	keys, err := d.AllFiles(context.Background(), grant(tenantA), "")
	if err != nil {
		t.Fatalf("AllFiles: %v", err)
	}
	// The directory marker is not one of them: Get would answer ErrNotFound for
	// a key ending in a slash.
	if len(keys) != 2 {
		t.Fatalf("listed %v, want both pages and no marker", keys)
	}
	// The tenant is stripped on the way out, or a caller could start passing it.
	for _, key := range keys {
		if strings.Contains(key, tenantA) {
			t.Errorf("the key leaks the tenant: %q", key)
		}
	}
}

// TestTheListingIsScopedToTheTenant: without the prefix, one tenant lists every
// file in the bucket. It is asked of the bucket rather than filtered afterwards,
// so the pages walked are not somebody else's object names.
func TestTheListingIsScopedToTheTenant(t *testing.T) {
	d, seen := disk(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`))
	})

	if _, err := d.AllFiles(context.Background(), grant(tenantB), "invoices/"); err != nil {
		t.Fatalf("AllFiles: %v", err)
	}

	prefix := (*seen)[0].URL.Query().Get("prefix")
	if !strings.HasPrefix(prefix, tenantB+"/") {
		t.Fatalf("prefix = %q, want it scoped to the tenant", prefix)
	}
	if !strings.HasSuffix(prefix, "invoices/") {
		t.Errorf("prefix = %q, want the caller's directory inside it", prefix)
	}
}

func TestAGrantWithoutATenantIsRefused(t *testing.T) {
	d, _ := disk(t, func(http.ResponseWriter, *http.Request) {})

	err := d.Put(context.Background(), auth.Grant{}, "a.pdf", strings.NewReader("x"), "")
	if !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}

func TestAKeyCannotEscapeTheTenant(t *testing.T) {
	d, _ := disk(t, func(http.ResponseWriter, *http.Request) {})

	err := d.Put(context.Background(), grant(tenantA), "../"+tenantB+"/secret.pdf",
		strings.NewReader("x"), "")
	if !errors.Is(err, filesystem.ErrBadKey) {
		t.Fatalf("err = %v, want ErrBadKey", err)
	}
}

// TestR2DerivesItsEndpoint: getting the endpoint or the region wrong produces a
// signature error that names neither, so neither is asked for.
func TestR2DerivesItsEndpoint(t *testing.T) {
	if _, err := s3.R2(s3.R2Config{Bucket: "uploads", AccessKey: "k", SecretKey: "s"}); err == nil {
		t.Error("R2 without an account id was accepted")
	}

	adapter, err := s3.R2(s3.R2Config{
		AccountID: "abc123", Bucket: "uploads", AccessKey: "k", SecretKey: "s",
	})
	if err != nil {
		t.Fatalf("R2: %v", err)
	}
	if adapter == nil {
		t.Fatal("R2 returned nothing")
	}
	link, err := adapter.PresignGet(context.Background(), tenantA+"/a.pdf", time.Hour)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	if !strings.HasPrefix(link, "https://abc123.r2.cloudflarestorage.com/uploads/") {
		t.Errorf("link = %q, want the derived endpoint", link)
	}
}

func TestNewRefusesAnIncompleteConfig(t *testing.T) {
	for _, c := range []struct {
		name string
		cfg  s3.Config
	}{
		{"no endpoint", s3.Config{Bucket: "b", AccessKey: "k", SecretKey: "s"}},
		{"no bucket", s3.Config{Endpoint: "https://x", AccessKey: "k", SecretKey: "s"}},
		{"no credentials", s3.Config{Endpoint: "https://x", Bucket: "b"}},
		{"not a URL", s3.Config{Endpoint: "not a url", Bucket: "b", AccessKey: "k", SecretKey: "s"}},
	} {
		if _, err := s3.New(c.cfg); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}
}

// TestAKeyIsEscapedPerSegment: escaping the whole path would encode the
// separators and turn a nested key into one long name, and leaving a plus sign
// alone would sign one path and request another.
func TestAKeyIsEscapedPerSegment(t *testing.T) {
	d, seen := disk(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if err := d.Put(context.Background(), grant(tenantA), "my folder/a+b.pdf",
		strings.NewReader("x"), ""); err != nil {
		t.Fatalf("Put: %v", err)
	}

	raw := (*seen)[0].URL.EscapedPath()
	if strings.Contains(raw, "%2F") {
		t.Errorf("the separators were escaped: %s", raw)
	}
	if !strings.Contains(raw, "%20") {
		t.Errorf("the spaces were not escaped: %s", raw)
	}
	if !strings.Contains(raw, "%2B") {
		t.Errorf("the plus was not escaped, and the bucket would read it as a space: %s", raw)
	}
}

// TestAQueryIsEncodedTheWayTheSignatureIs is a mismatch that only shows up on a
// real bucket: url.Values.Encode writes a space as "+", the bucket
// re-canonicalizes it as "%20", and the signature stops matching. A folder named
// "Q1 reports" is all it takes.
func TestAQueryIsEncodedTheWayTheSignatureIs(t *testing.T) {
	d, seen := disk(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`))
	})

	if _, err := d.AllFiles(context.Background(), grant(tenantA), "Q1 reports/"); err != nil {
		t.Fatalf("AllFiles: %v", err)
	}
	raw := (*seen)[0].URL.RawQuery
	if strings.Contains(raw, "+") {
		t.Errorf("the query encodes a space as a plus: %s", raw)
	}
	if !strings.Contains(raw, "%20") {
		t.Errorf("the space is not percent-encoded: %s", raw)
	}
}

// TestAPresignedLinkCarriesEverythingTheBucketChecks.
//
// This is the method the audit named: the link that stands in for a session when
// a route cannot, in an <img> tag or an e-mail. The bytes never pass through the
// application, so everything that decides whether it works is in the URL.
func TestAPresignedLinkCarriesEverythingTheBucketChecks(t *testing.T) {
	adapter, _ := bucket(t, func(http.ResponseWriter, *http.Request) {})

	link, err := adapter.PresignGet(context.Background(), tenantA+"/invoices/q1 report.pdf", 15*time.Minute)
	if err != nil {
		t.Fatalf("PresignGet: %v", err)
	}
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("the link is not a URL: %v", err)
	}
	if want := "/uploads/" + tenantA + "/invoices/q1 report.pdf"; parsed.Path != want {
		t.Errorf("path = %q, want %q", parsed.Path, want)
	}
	if strings.Contains(parsed.RawQuery, "+") {
		t.Errorf("the query encodes a space as a plus: %s", parsed.RawQuery)
	}

	q := parsed.Query()
	for _, want := range []string{"X-Amz-Algorithm", "X-Amz-Credential", "X-Amz-Date", "X-Amz-Expires", "X-Amz-SignedHeaders", "X-Amz-Signature"} {
		if q.Get(want) == "" {
			t.Errorf("the link carries no %s: %s", want, parsed.RawQuery)
		}
	}
	if got := q.Get("X-Amz-Expires"); got != "900" {
		t.Errorf("X-Amz-Expires = %q, want 900", got)
	}
	if got := q.Get("X-Amz-SignedHeaders"); got != "host" {
		t.Errorf("X-Amz-SignedHeaders = %q, want host -- a browser sends nothing else that has to match", got)
	}
	// The signature is last, because it signs the ones before it.
	if !strings.HasSuffix(parsed.RawQuery, "&X-Amz-Signature="+q.Get("X-Amz-Signature")) {
		t.Errorf("the signature is not the last parameter: %s", parsed.RawQuery)
	}
}

// TestTwoPresignedLinksDifferByPath: the signature covers the object, so a link
// to one file is not a link to another. If it were, one link would be a key to
// the bucket.
func TestTwoPresignedLinksDifferByPath(t *testing.T) {
	adapter, _ := bucket(t, func(http.ResponseWriter, *http.Request) {})
	ctx := context.Background()

	mine, err := adapter.PresignGet(ctx, tenantA+"/a.pdf", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := adapter.PresignGet(ctx, tenantB+"/a.pdf", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	signature := func(link string) string {
		u, _ := url.Parse(link)
		return u.Query().Get("X-Amz-Signature")
	}
	if signature(mine) == signature(theirs) {
		t.Fatal("two objects signed the same, and one link would open both")
	}
}

// TestAPresignedLinkNeedsALifetimeItCanHave: zero is not a lifetime, and seven
// days is the ceiling SigV4 query authorization itself sets -- asking for more
// produces a link the bucket refuses, which is worse than an error here.
func TestAPresignedLinkNeedsALifetimeItCanHave(t *testing.T) {
	adapter, _ := bucket(t, func(http.ResponseWriter, *http.Request) {})
	ctx := context.Background()

	for _, ttl := range []time.Duration{0, -time.Hour, 8 * 24 * time.Hour} {
		if _, err := adapter.PresignGet(ctx, tenantA+"/a.pdf", ttl); err == nil {
			t.Errorf("PresignGet with ttl %s was accepted", ttl)
		}
	}
}

// TestTheDiskAsksTheStoreForTheLink: a presigning driver answers from the store
// and a directory answers from the application, and the caller writes one call.
func TestTheDiskAsksTheStoreForTheLink(t *testing.T) {
	adapter, seen := bucket(t, func(http.ResponseWriter, *http.Request) {})
	d := filesystem.NewDisk("s3", adapter)

	signer := filesystem.NewURLSigner(nil, "/files")
	link, err := signer.TemporaryURL(context.Background(), grant(tenantA), d, "a.pdf", time.Hour)
	if err != nil {
		t.Fatalf("TemporaryURL: %v", err)
	}
	if !strings.Contains(link, "X-Amz-Signature=") {
		t.Fatalf("link = %q, want the store's own link", link)
	}
	if strings.HasPrefix(link, "/files") {
		t.Fatalf("link = %q, want it to point at the bucket", link)
	}
	// Nothing was called to make it. A URL that costs a round trip is a URL
	// nobody puts in a loop over a listing.
	if len(*seen) != 0 {
		t.Fatalf("%d requests to sign a link", len(*seen))
	}
}

// TestAPresignedLinkStillNeedsAGrant: the path is built by filesystem.Key, so
// there is no way to ask for one without a Policy having run.
func TestAPresignedLinkStillNeedsAGrant(t *testing.T) {
	adapter, _ := bucket(t, func(http.ResponseWriter, *http.Request) {})
	d := filesystem.NewDisk("s3", adapter)
	signer := filesystem.NewURLSigner(nil, "/files")

	if _, err := signer.TemporaryURL(context.Background(), auth.Grant{}, d, "a.pdf", time.Hour); !errors.Is(err, filesystem.ErrNoTenant) {
		t.Fatalf("err = %v, want ErrNoTenant", err)
	}
}
