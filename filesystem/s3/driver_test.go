package s3_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/arandu-io/hesape/filesystem"
	"github.com/arandu-io/hesape/filesystem/s3"
)

// TestADiskNamedInConfigurationIsBuiltByTheDriver: once the driver is
// registered, the bucket, endpoint, region, path style and credentials on the
// disk's Config are the bucket the files land in -- no application writes the
// adapter by hand.
func TestADiskNamedInConfigurationIsBuiltByTheDriver(t *testing.T) {
	var seen []*http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Clone(context.Background()))
	}))
	t.Cleanup(srv.Close)

	disks := filesystem.NewDisks("files").Extend(s3.DriverName, s3.Driver())
	err := disks.Configure(map[string]filesystem.Config{"files": {
		Driver: s3.DriverName, Endpoint: srv.URL, Bucket: "kuaa-dev", Region: "auto",
		PathStyle: true, AccessKey: "key", SecretKey: "secret",
	}})
	if err != nil {
		t.Fatalf("Configure: %v", err)
	}

	disk, err := disks.Disk("files")
	if err != nil {
		t.Fatalf("Disk: %v", err)
	}
	if err := disk.Put(context.Background(), grant(tenantA), "invoice.pdf", strings.NewReader("pdf"), "application/pdf"); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if len(seen) != 1 {
		t.Fatalf("the bucket saw %d requests, want 1", len(seen))
	}
	if path := seen[0].URL.Path; !strings.HasPrefix(path, "/kuaa-dev/"+tenantA+"/") {
		t.Fatalf("the file landed on %s, want the configured bucket in the path, then the tenant", path)
	}
	if auth := seen[0].Header.Get("Authorization"); !strings.Contains(auth, "Credential=key/") || !strings.Contains(auth, "/auto/s3/") {
		t.Fatalf("Authorization = %q, want the configured key and region signed", auth)
	}
}

// TestTheDriverNamesTheDiskThatIsMissingASetting: a boot stopped by an unset
// variable says which disk wanted it.
func TestTheDriverNamesTheDiskThatIsMissingASetting(t *testing.T) {
	disks := filesystem.NewDisks("files").Extend(s3.DriverName, s3.Driver())
	err := disks.Configure(map[string]filesystem.Config{"files": {Driver: s3.DriverName, Endpoint: "https://acct.r2.cloudflarestorage.com", Bucket: "b"}})
	if err == nil || !strings.Contains(err.Error(), `"files"`) || !strings.Contains(err.Error(), "no credentials") {
		t.Fatalf("Configure with no credentials answered %v", err)
	}
}
